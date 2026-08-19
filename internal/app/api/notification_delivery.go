package api

import (
	"context"
	"strings"

	"github.com/torgnexa/torgnexa/internal/core/tenancy"
	"github.com/torgnexa/torgnexa/internal/platform/notifications"
	"github.com/torgnexa/torgnexa/internal/platform/postgres/notificationrepo"
	"github.com/torgnexa/torgnexa/internal/platform/secrets"
)

type notificationDestinationResolver struct {
	repo    *notificationrepo.Repository
	secrets secrets.SecretProvider
}

func (r notificationDestinationResolver) ResolveDestination(ctx context.Context, scope tenancy.Scope, recipient string, channel notifications.Channel) (string, error) {
	if r.repo == nil || r.secrets == nil || !scope.Valid() || recipient == "" {
		return "", notifications.ErrInvalid
	}
	destination, err := r.repo.Destination(ctx, scope, recipient, channel)
	if err != nil {
		return "", err
	}
	var value string
	err = r.secrets.Use(ctx, scope, destination.SecretReference, func(raw []byte) error {
		value = strings.TrimSpace(string(raw))
		if value == "" || len(value) > 512 {
			return notifications.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return value, nil
}
