package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/removarr/removarr/internal/config"
	"github.com/removarr/removarr/internal/httpjson"
)

// healthTimeout bounds one health check. The image's HEALTHCHECK gives it longer.
const healthTimeout = 4 * time.Second

// command decides what the binary does from its arguments: "serve" without any, and
// "healthcheck" for exactly that word. Anything else is an error, so a mistyped command never
// starts a second server (spec section 11).
func command(args []string) (string, error) {
	switch {
	case len(args) == 0:
		return "serve", nil
	case len(args) == 1 && args[0] == "healthcheck":
		return "healthcheck", nil
	default:
		return "", errors.New("usage: removarr [healthcheck]")
	}
}

// loadHealthEnv returns the port removarr listens on, read from the environment as the server
// reads it.
func loadHealthEnv(getenv func(string) string) (int, error) {
	env, err := config.LoadEnv(getenv)
	if err != nil {
		return 0, err
	}
	return env.Port, nil
}

// healthcheck asks /api/health of the removarr listening on 127.0.0.1 at port, and returns
// nil only for a 200 whose status is "ok" (spec section 11). The base image has no shell or
// HTTP client, so the image's HEALTHCHECK runs this.
func healthcheck(ctx context.Context, port int, client *http.Client) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/health", port), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("removarr does not answer on port %d", port)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check answered status %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil || body.Status != "ok" {
		return errors.New("health check answered without status ok")
	}
	return nil
}

// runHealthcheck runs the healthcheck command and returns the process exit code.
func runHealthcheck() int {
	port, err := loadHealthEnv(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "removarr healthcheck:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
	defer cancel()
	client := &http.Client{CheckRedirect: httpjson.SameHostRedirects}
	if err := healthcheck(ctx, port, client); err != nil {
		fmt.Fprintln(os.Stderr, "removarr healthcheck:", err)
		return 1
	}
	return 0
}
