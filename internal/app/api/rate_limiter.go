package api

import (
	"context"
	"io"

	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

func newAPIRateLimiter(ctx context.Context, cfg config.Config) (securityedge.Limiter, io.Closer, error) {
	switch cfg.Security.RateLimitBackend {
	case config.RateLimitBackendLocal:
		if cfg.Environment != config.EnvironmentDevelopment && cfg.Environment != config.EnvironmentTest {
			return nil, nil, ErrSecurityCompositionInvalid
		}
		return securityedge.NewLocalLimiter(cfg.Security.RateLimitMaxKeys), nil, nil
	case config.RateLimitBackendValkey:
		limiter, err := securityedge.NewValkeyLimiter(ctx, securityedge.ValkeyConfig{
			Address:        cfg.Valkey.Address,
			Username:       cfg.Valkey.Username,
			Password:       cfg.Valkey.Password,
			Namespace:      cfg.Security.RateLimitNamespace,
			ConnectTimeout: cfg.Valkey.ConnectTimeout,
			RequestTimeout: cfg.Valkey.RequestTimeout,
			MaxConnections: cfg.Valkey.MaxConnections,
			MaxKeys:        cfg.Security.RateLimitMaxKeys,
		})
		if err != nil {
			return nil, nil, err
		}
		return limiter, limiter, nil
	default:
		return nil, nil, ErrSecurityCompositionInvalid
	}
}
