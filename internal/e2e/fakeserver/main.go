// Command fakeserver serves removarr's real API and the embedded web UI on in-memory fake
// apps, for the end-to-end test and for working on the UI without a media server. It
// listens on 127.0.0.1 only and never connects to an app. Deletions change the fake
// library, which starts afresh on every start.
//
//	go run ./internal/e2e/fakeserver -port 7373 -dir /tmp/removarr-fake
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	port := flag.Int("port", 7272, "port to listen on, on 127.0.0.1")
	dir := flag.String("dir", "", "directory for the database (required)")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if *dir == "" || *port < 1 || *port > 65535 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*dir, *port, log); err != nil {
		log.Error("fakeserver stopped", "error", err)
		os.Exit(1)
	}
}

func run(dir string, port int, log *slog.Logger) error {
	w, err := wire(dir, port, log)
	if err != nil {
		return err
	}
	defer w.Close()

	ln, err := net.Listen("tcp", w.http.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		w.http.Shutdown(shutdown)
	}()
	log.Info("fakeserver listening", "addr", "http://"+w.http.Addr, "dir", dir)
	if err := w.http.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	// Serve returns as soon as Shutdown starts: let the requests finish before the
	// database closes.
	<-drained
	return nil
}
