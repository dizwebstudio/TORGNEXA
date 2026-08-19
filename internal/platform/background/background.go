// Package background provides the lifecycle boundary for background processes
// before their task-specific loops are introduced.
package background

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

// Run marks a background process ready and waits for graceful cancellation.
func Run(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if ctx == nil {
		return fmt.Errorf("context is required")
	}
	if logger == nil {
		return fmt.Errorf("logger is required")
	}
	logger.Info("service ready", "event", "service.ready", "shutdown_timeout", cfg.ShutdownTimeout.String())
	<-ctx.Done()
	return nil
}
