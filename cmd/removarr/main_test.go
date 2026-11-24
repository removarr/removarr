package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/removarr/removarr/internal/app"
	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/store"
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

func TestTheJobsShareOneGateAndTheExecutorGetsTheDryRunSetting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "removarr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	stop := make(chan struct{})
	for _, dry := range []bool{true, false} {
		env := config.Env{DryRun: dry}
		cfg := config.NewService(env, st)
		j := newJobs(env, cfg, &app.Apps{Config: cfg}, st, log, stop)
		if j.exec.DryRun != dry {
			t.Errorf("DryRun %v: the executor got %v", dry, j.exec.DryRun)
		}
		if j.scans.Gate == nil || j.scans.Gate != j.plans.Gate || j.plans.Gate != j.exec.Gate {
			t.Errorf("gates %p %p %p, want one shared gate", j.scans.Gate, j.plans.Gate, j.exec.Gate)
		}
		if j.exec.Stop != (<-chan struct{})(stop) || j.exec.Log != log || j.scans.Log != log {
			t.Error("the executor must get the stop signal and the log")
		}
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
