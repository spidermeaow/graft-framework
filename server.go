package graft

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Run serves with conservative timeouts and graceful shutdown on SIGINT or SIGTERM.
// For custom TLS/timeouts, use App as the Handler of your own http.Server.
func (a *App) Run(addr string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.RunContext(ctx, addr)
}

// RunContext serves until cancellation, allowing up to 10 seconds for in-flight requests.
func (a *App) RunContext(ctx context.Context, addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return a.Serve(ctx, listener)
}

// Serve takes ownership of a listener and gracefully stops when ctx is cancelled.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: a, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := server.Shutdown(shutdown)
		if err != nil {
			_ = server.Close()
		}
		serveErr := <-done
		if err != nil {
			return err
		}
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
}
