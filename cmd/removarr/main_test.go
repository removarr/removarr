package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func TestServeWaitsForInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "complete body")
	})}
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, 5*time.Second) }()

	type result struct {
		status int
		body   string
		err    error
	}
	got := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err != nil {
			got <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		got <- result{status: resp.StatusCode, body: string(b), err: err}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never started")
	}
	cancel()

	select {
	case err := <-served:
		t.Fatalf("serve returned (%v) while a request was still in flight", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(release)
	select {
	case r := <-got:
		if r.err != nil || r.status != http.StatusOK || r.body != "complete body" {
			t.Errorf("client got status %d body %q err %v; want 200 and the complete body", r.status, r.body, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the client never got a response")
	}
	select {
	case err := <-served:
		if err != nil {
			t.Errorf("serve = %v, want nil after a clean drain", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the request finished")
	}
}

func TestServeReportsAnUnfinishedDrain(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
	})}
	t.Cleanup(func() { close(release) })
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	served := make(chan error, 1)
	go func() { served <- serve(ctx, srv, ln, 50*time.Millisecond) }()
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String())
		if err == nil {
			resp.Body.Close()
		}
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never started")
	}
	cancel()

	select {
	case err := <-served:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("serve = %v, want an error wrapping context.DeadlineExceeded", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after the grace period")
	}
}

func TestServeReturnsWhenServingFails(t *testing.T) {
	ln := listen(t)
	ln.Close()
	srv := &http.Server{Handler: http.NotFoundHandler()}

	served := make(chan error, 1)
	go func() { served <- serve(context.Background(), srv, ln, time.Second) }()
	select {
	case err := <-served:
		if err == nil {
			t.Error("serve = nil, want the listener error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return although the listener failed")
	}
}
