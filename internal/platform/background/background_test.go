package background

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func TestRunStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
}

func TestRunRejectsMissingDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Run(nil, config.Config{}, logger); err == nil {
		t.Fatal("Run(nil, ...) error = nil")
	}
	if err := Run(context.Background(), config.Config{}, nil); err == nil {
		t.Fatal("Run(..., nil) error = nil")
	}
}
