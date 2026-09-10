package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
)

var errSettingsAudit = errors.New("settings: atomic audit failed")

// auditedSettingsMutation returns a result only after mutation and audit commit together.
func auditedSettingsMutation[T any](ctx context.Context, scope tenancy.Scope, capturer auditCapturer, mutate func(context.Context) (T, error), capture func(context.Context, T) error) (T, error) {
	var result T
	boundary, ok := capturer.(interface {
		WithinTransaction(context.Context, tenancy.Scope, func(context.Context) error) error
	})
	if !ok {
		return result, errSettingsAudit
	}
	var mutationErr error
	err := boundary.WithinTransaction(ctx, scope, func(txCtx context.Context) error {
		var err error
		result, err = mutate(txCtx)
		mutationErr = err
		if err != nil {
			return err
		}
		if err := capture(txCtx, result); err != nil {
			return fmt.Errorf("%w: %w", errSettingsAudit, err)
		}
		return nil
	})
	if err != nil {
		var zero T
		if mutationErr == nil && !errors.Is(err, errSettingsAudit) {
			err = fmt.Errorf("%w: %w", errSettingsAudit, err)
		}
		return zero, err
	}
	return result, nil
}
