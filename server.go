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

// ServerConfig configures connection limits and ordered shutdown. Zero timeout
// fields use defaults; negative fields are rejected. Hooks run after HTTP drain,
// in order, with the remaining shared shutdown budget and must honor that context.
// Hijacked connections/background workers remain the application's responsibility.
type ServerConfig struct {
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	ShutdownTimeout   time.Duration
	DrainDelay        time.Duration
	Health            *Health
	ShutdownHooks     []func(context.Context) error
}

func (cfg ServerConfig) normalized() (ServerConfig, error) {
	fields := []*time.Duration{&cfg.ReadHeaderTimeout, &cfg.ReadTimeout, &cfg.WriteTimeout, &cfg.IdleTimeout, &cfg.ShutdownTimeout}
	defaults := []time.Duration{5 * time.Second, 30 * time.Second, 30 * time.Second, 60 * time.Second, 10 * time.Second}
	for i, field := range fields {
		if *field < 0 {
			return cfg, errors.New("graft: server timeouts cannot be negative")
		}
		if *field == 0 {
			*field = defaults[i]
		}
	}
	if cfg.MaxHeaderBytes < 0 || cfg.DrainDelay < 0 {
		return cfg, errors.New("graft: invalid server limits")
	}
	if cfg.MaxHeaderBytes == 0 {
		cfg.MaxHeaderBytes = 1 << 20
	}
	for _, hook := range cfg.ShutdownHooks {
		if hook == nil {
			return cfg, errors.New("graft: nil shutdown hook")
		}
	}
	return cfg, nil
}

// Run serves with conservative timeouts and graceful shutdown on SIGINT or SIGTERM.
// For custom TLS/timeouts, use App as the Handler of your own http.Server.
func (a *App) Run(addr string) error {
	return a.RunWithConfig(addr, ServerConfig{})
}

// RunWithConfig is Run with explicit server and lifecycle settings.
func (a *App) RunWithConfig(addr string, cfg ServerConfig) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return a.RunContextWithConfig(ctx, addr, cfg)
}

// RunContext serves until cancellation, allowing up to 10 seconds for in-flight requests.
func (a *App) RunContext(ctx context.Context, addr string) error {
	return a.RunContextWithConfig(ctx, addr, ServerConfig{})
}

func (a *App) RunContextWithConfig(ctx context.Context, addr string, cfg ServerConfig) error {
	if _, err := cfg.normalized(); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return a.ServeWithConfig(ctx, listener, cfg)
}

// Serve takes ownership of a listener and gracefully stops when ctx is cancelled.
func (a *App) Serve(ctx context.Context, listener net.Listener) error {
	return a.ServeWithConfig(ctx, listener, ServerConfig{})
}

// ServeWithConfig owns listener, including on configuration errors. Cancellation
// withdraws readiness, waits DrainDelay for the load balancer, then drains HTTP.
// Request contexts survive normal drain and are cancelled when it finishes/expires.
func (a *App) ServeWithConfig(ctx context.Context, listener net.Listener, cfg ServerConfig) error {
	cfg, err := cfg.normalized()
	if err != nil {
		_ = listener.Close()
		return err
	}
	requests, cancelRequests := context.WithCancel(context.WithoutCancel(ctx))
	defer cancelRequests()
	server := &http.Server{Handler: a, ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: cfg.MaxHeaderBytes, BaseContext: func(net.Listener) context.Context { return requests }}
	if cfg.Health != nil {
		cfg.Health.SetReady(ctx.Err() == nil)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	var serveErr error
	stopped := false
	select {
	case serveErr = <-done:
		stopped = true
	case <-ctx.Done():
	}
	if cfg.Health != nil {
		cfg.Health.SetReady(false)
	}
	if !stopped && cfg.DrainDelay > 0 {
		timer := time.NewTimer(cfg.DrainDelay)
		<-timer.C
	}
	shutdown, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err = server.Shutdown(shutdown)
	if err != nil {
		cancelRequests()
		_ = server.Close()
	}
	if !stopped {
		serveErr = <-done
	}
	cancelRequests()
	for _, hook := range cfg.ShutdownHooks {
		err = errors.Join(err, hook(shutdown))
	}
	if errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = nil
	}
	return errors.Join(err, serveErr)
}
