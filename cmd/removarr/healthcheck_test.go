package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// healthServer serves h on 127.0.0.1 and returns its port.
func healthServer(t *testing.T, h http.HandlerFunc) int {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func okHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/health" || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func TestHealthcheckPassesWhenTheServerReportsOK(t *testing.T) {
	port := healthServer(t, okHealth)
	if err := healthcheck(context.Background(), port, &http.Client{}); err != nil {
		t.Fatalf("healthcheck = %v, want nil", err)
	}
}

func portEnv(port int) func(string) string {
	return func(k string) string {
		if k == "REMOVARR_PORT" {
			return strconv.Itoa(port)
		}
		return ""
	}
}

func TestRunHealthcheckReadsTheConfiguredPort(t *testing.T) {
	port := healthServer(t, okHealth)
	var stderr bytes.Buffer
	if code := runHealthcheck(portEnv(port), &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
}

func TestRunHealthcheckFailsOnThePortNothingListensOn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var stderr bytes.Buffer
	if code := runHealthcheck(portEnv(port), &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if stderr.Len() == 0 {
		t.Fatal("nothing written to stderr")
	}
}

func TestRunHealthcheckFailsOnAnInvalidPort(t *testing.T) {
	var stderr bytes.Buffer
	if code := runHealthcheck(func(k string) string {
		if k == "REMOVARR_PORT" {
			return "not-a-port"
		}
		return ""
	}, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestHealthcheckFailsWhenNothingListens(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil with nothing listening")
	}
}

func TestHealthcheckFailsOnAnErrorStatus(t *testing.T) {
	port := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"status":"ok"}`, http.StatusServiceUnavailable)
	})
	if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil for a 503")
	}
}

func TestHealthcheckFailsWhenTheAnswerIsNotOK(t *testing.T) {
	for _, body := range []string{`{"status":"starting"}`, `not json`, ``} {
		port := healthServer(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
		if err := healthcheck(context.Background(), port, &http.Client{}); err == nil {
			t.Errorf("healthcheck = nil for body %q", body)
		}
	}
}

func TestHealthcheckGivesUpOnAServerThatHangs(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	port := healthServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := healthcheck(ctx, port, &http.Client{}); err == nil {
		t.Fatal("healthcheck = nil for a server that never answers")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("healthcheck did not give up at its deadline")
	}
}

func TestCommandRunsTheServerWithoutArguments(t *testing.T) {
	got, err := command(nil)
	if err != nil || got != "serve" {
		t.Fatalf("command(nil) = %q, %v; want serve", got, err)
	}
}

func TestCommandRecognisesHealthcheck(t *testing.T) {
	got, err := command([]string{"healthcheck"})
	if err != nil || got != "healthcheck" {
		t.Fatalf("command = %q, %v; want healthcheck", got, err)
	}
}

func TestCommandRefusesAnythingElse(t *testing.T) {
	for _, args := range [][]string{{"serve"}, {"healthcheck", "extra"}, {"--help"}, {"helthcheck"}} {
		if got, err := command(args); err == nil {
			t.Errorf("command(%q) = %q, nil; want an error", args, got)
		}
	}
}
