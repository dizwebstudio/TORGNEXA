package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func TestExecuteDoesNotHideUnrelatedCancellation(t *testing.T) {
	err := execute(context.Background(), config.Config{}, testLogger(), func(context.Context, config.Config, *slog.Logger) error {
		return context.Canceled
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("execute() error = %v, want context.Canceled", err)
	}
}

func TestExecuteTreatsProcessCancellationAsGraceful(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := execute(ctx, config.Config{ShutdownTimeout: time.Second}, testLogger(), func(ctx context.Context, _ config.Config, _ *slog.Logger) error {
		return ctx.Err()
	})
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
}

func TestExecuteBoundsShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	release := make(chan struct{})
	err := execute(ctx, config.Config{ShutdownTimeout: 10 * time.Millisecond}, testLogger(), func(context.Context, config.Config, *slog.Logger) error {
		<-release
		return nil
	})
	close(release)
	if !errors.Is(err, errShutdownTimeout) {
		t.Fatalf("execute() error = %v, want shutdown timeout", err)
	}
}

func TestExecuteDoesNotHideJoinedCancellationFailure(t *testing.T) {
	realFailure := errors.New("flush failed")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := execute(ctx, config.Config{ShutdownTimeout: time.Second}, testLogger(), func(context.Context, config.Config, *slog.Logger) error {
		return errors.Join(context.Canceled, realFailure)
	})
	if !errors.Is(err, realFailure) {
		t.Fatalf("execute() error = %v, want joined failure", err)
	}
}

func TestExecuteRecoversRunnerPanicWithoutLoggingValue(t *testing.T) {
	const sensitivePanic = "secret-runner-panic"
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	err := execute(context.Background(), config.Config{}, logger, func(context.Context, config.Config, *slog.Logger) error {
		panic(sensitivePanic)
	})
	if !errors.Is(err, errRunnerPanic) {
		t.Fatalf("execute() error = %v, want runner panic", err)
	}
	if bytes.Contains(output.Bytes(), []byte(sensitivePanic)) {
		t.Fatalf("panic value leaked: %q", output.String())
	}
}

func TestExecuteDetectsRunnerGoexit(t *testing.T) {
	err := execute(context.Background(), config.Config{}, testLogger(), func(context.Context, config.Config, *slog.Logger) error {
		runtime.Goexit()
		return nil
	})
	if !errors.Is(err, errRunnerGoexit) {
		t.Fatalf("execute() error = %v, want runner Goexit", err)
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
