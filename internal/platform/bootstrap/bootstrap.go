// Package bootstrap owns common process startup, logging, signals, and shutdown.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/domain"
	"github.com/torgnexa/torgnexa/internal/platform/logging"
)

// Runner implements the lifecycle of one TORGNEXA process.
type Runner func(context.Context, config.Config, *slog.Logger) error

var errShutdownTimeout = errors.New("process shutdown timed out")

var (
	errRunnerPanic  = errors.New("process runner panicked")
	errRunnerGoexit = errors.New("process runner exited without returning")
)

// Run loads configuration, installs termination-signal handling, and invokes runner.
// It logs every terminal outcome and returns an error so the thin command can select
// a non-zero exit status without duplicating process policy.
func Run(service config.Service, runner Runner) error {
	if runner == nil {
		err := fmt.Errorf("runner is required")
		logFallback(service, err)
		return err
	}

	cfg, err := config.Load(service)
	if err != nil {
		err = fmt.Errorf("load configuration: %w", err)
		logFallback(service, err)
		return err
	}
	logger, err := logging.New(os.Stderr, logging.Options{
		Level:     cfg.Log.Level,
		Format:    string(cfg.Log.Format),
		AddSource: cfg.Log.AddSource,
	})
	if err != nil {
		err = fmt.Errorf("initialize logging: %w", err)
		logFallback(service, err)
		return err
	}
	logger = logger.With("service", service, "environment", cfg.Environment)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		// Restore the operating system's default behavior so a second signal
		// can terminate a process whose bounded shutdown is itself stuck.
		stop()
	}()

	return execute(ctx, cfg, logger, runner)
}

func logFallback(service config.Service, err error) {
	logger, loggerErr := logging.New(os.Stderr, logging.Options{Level: "info", Format: "json"})
	if loggerErr != nil {
		return
	}
	logger.Error("service startup failed", "event", "service.startup_failed", "service", service, "reason", err.Error())
}

func execute(ctx context.Context, cfg config.Config, logger *slog.Logger, runner Runner) error {
	logger.Info("service starting", "event", "service.starting", "version", domain.Version())
	result := make(chan error, 1)
	go func() {
		returned := false
		defer func() {
			if recover() != nil {
				result <- errRunnerPanic
				return
			}
			if !returned {
				// runtime.Goexit runs deferred functions but never returns from runner.
				result <- errRunnerGoexit
			}
		}()
		err := runner(ctx, cfg, logger)
		returned = true
		result <- err
	}()

	select {
	case err := <-result:
		return finish(ctx, logger, err)
	case <-ctx.Done():
	}

	timeout := cfg.ShutdownTimeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-result:
		return finish(ctx, logger, err)
	case <-timer.C:
		err := fmt.Errorf("%w after %s", errShutdownTimeout, timeout)
		logger.Error("service failed", "event", "service.shutdown_timeout", "error_code", errorCode(err))
		return err
	}
}

func finish(ctx context.Context, logger *slog.Logger, err error) error {
	if err == nil || (err == context.Canceled && ctx.Err() != nil) {
		logger.Info("service stopped", "event", "service.stopped")
		return nil
	}
	logger.Error("service failed", "event", "service.failed", "error_code", errorCode(err))
	return err
}

func errorCode(err error) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) && coded.ErrorCode() != "" {
		return coded.ErrorCode()
	}
	switch {
	case errors.Is(err, errShutdownTimeout):
		return "shutdown_timeout"
	case errors.Is(err, errRunnerPanic):
		return "runner_panic"
	case errors.Is(err, errRunnerGoexit):
		return "runner_goexit"
	case err == context.Canceled:
		return "runner_canceled"
	default:
		return "runner_failed"
	}
}
