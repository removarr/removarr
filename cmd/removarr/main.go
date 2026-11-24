// Command removarr serves the removarr API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/removarr/removarr/internal/api"
	"github.com/removarr/removarr/internal/app"
	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/executor"
	"github.com/removarr/removarr/internal/gate"
	"github.com/removarr/removarr/internal/planner"
	"github.com/removarr/removarr/internal/scanner"
	"github.com/removarr/removarr/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("removarr stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	env, err := config.LoadEnv(os.Getenv)
	if err != nil {
		return err
	}
	// The database path is made absolute so it does not depend on the working directory.
	configDir, err := filepath.Abs(env.ConfigDir)
	if err != nil {
		return fmt.Errorf("resolve config directory: %w", err)
	}
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	st, err := store.Open(filepath.Join(configDir, "removarr.db"))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg := config.NewService(env, st)
	apps := &app.Apps{Config: cfg, Timeout: 60 * time.Second}
	g := &gate.Gate{}
	scans := &scanner.Service{
		Gate:  g,
		Build: apps.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
		Log:   log,
	}
	plans := &planner.Service{
		Gate:  g,
		Build: apps.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
	}
	exec := &executor.Service{Build: apps.Writers, Store: st, Gate: g, DryRun: env.DryRun, Now: time.Now, Stop: ctx.Done(), Log: log}
	server := &api.Server{Config: cfg, Store: st, Scanner: scans, Apps: apps, Planner: plans, Executor: exec, Log: log}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", env.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("removarr listening", "port", env.Port, "configDir", configDir, "dryRun", env.DryRun, "allowedHosts", env.AllowedHosts)
	err = serve(ctx, httpServer, ln, 10*time.Second)
	// An execution goes on after its request (spec 6.11): let it finish its item and record
	// it before the database closes. stop also covers serving that failed on its own.
	stop()
	if !exec.Wait(executionGrace) {
		log.Error("an execution was still running when removarr stopped: the item it was running may be missing from the history")
	}
	return err
}

// executionGrace is how long removarr waits, when stopping, for a running execution to
// finish its item (spec 6.11, stopping removarr).
const executionGrace = 2 * time.Minute

// serve serves srv on ln until ctx is done, then shuts the server down, giving in-flight
// requests up to grace to finish. It returns only after both Shutdown and Serve have
// returned. That does not make closing the database safe: once grace runs out a request may
// still be running, and an execution goes on after its request anyway, so the caller waits
// for the executor first. It returns nil after a clean drain, an error wrapping the Shutdown
// error if the grace period ran out, or the serving error if serving stopped on its own. It
// never returns http.ErrServerClosed.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error {
	stopped := make(chan struct{})
	defer close(stopped)
	shutdownDone := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
		case <-stopped:
			shutdownDone <- nil
			return
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()
		shutdownDone <- srv.Shutdown(shutdownCtx)
	}()

	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		// Serve returns as soon as Shutdown starts; wait for the drain itself.
		if err := <-shutdownDone; err != nil {
			return fmt.Errorf("shut down: %w", err)
		}
		return nil
	}
	return err
}
