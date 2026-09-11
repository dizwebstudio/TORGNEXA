package api

import (
	"context"
	"errors"
	"testing"

	"github.com/torgnexa/torgnexa/internal/platform/config"
	"github.com/torgnexa/torgnexa/internal/platform/securityedge"
)

func TestAPIRateLimiterAllowsLocalBackendOnlyInDevelopmentOrTest(t *testing.T) {
	cfg := config.Config{
		Environment: config.EnvironmentDevelopment,
		Security: config.Security{
			RateLimitBackend: config.RateLimitBackendLocal,
			RateLimitMaxKeys: 17,
		},
	}
	limiter, closer, err := newAPIRateLimiter(context.Background(), cfg)
	if err != nil || closer != nil {
		t.Fatalf("development limiter=%T closer=%v error=%v", limiter, closer, err)
	}
	if _, ok := limiter.(*securityedge.LocalLimiter); !ok {
		t.Fatalf("development limiter=%T", limiter)
	}

	cfg.Environment = "staging"
	limiter, closer, err = newAPIRateLimiter(context.Background(), cfg)
	if limiter != nil || closer != nil || !errors.Is(err, ErrSecurityCompositionInvalid) {
		t.Fatalf("staging limiter=%T closer=%v error=%v", limiter, closer, err)
	}
}
