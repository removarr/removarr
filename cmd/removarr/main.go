// Command removarr serves the removarr API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
		Addr:              fmt.Sprintf(":%d", env.Port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	log.Info("removarr listening", "port", env.Port, "configDir", configDir, "dryRun", env.DryRun)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
