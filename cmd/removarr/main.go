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

	cfg := config.NewService(env, st)
	apps := &app.Apps{Config: cfg, Timeout: 60 * time.Second}
	scans := &scanner.Service{
		Build: apps.Providers,
		Rules: cfg.Rules,
		Store: st,
		Now:   time.Now,
		Log:   log,
	}
	server := &api.Server{Config: cfg, Store: st, Scanner: scans, Apps: apps, Log: log}

	httpServer := &http.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", env.Port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	log.Info("removarr listening", "port", env.Port, "configDir", configDir, "dryRun", env.DryRun, "allowedHosts", env.AllowedHosts)
	return serve(ctx, httpServer, ln, 10*time.Second)
}

// serve serves srv on ln until ctx is done, then shuts the server down, giving in-flight
// requests up to grace to finish. It returns only after both Shutdown and Serve have
// returned, so the caller can close the database right after it returns without cutting
// off a request that is still using it. It returns nil after a clean drain, an error
// wrapping the Shutdown error if the grace period ran out, or the serving error if serving
// stopped on its own. It never returns http.ErrServerClosed.
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
