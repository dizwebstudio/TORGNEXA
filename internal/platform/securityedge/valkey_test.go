package securityedge

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValkeyLimiterStartupFailsClosedWhenBackendIsUnavailable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := NewValkeyLimiter(ctx, ValkeyConfig{
		Address: "127.0.0.1:1", Namespace: "torgnexa:test", ConnectTimeout: 100 * time.Millisecond,
		RequestTimeout: 100 * time.Millisecond, MaxConnections: 1, MaxKeys: 10,
	})
	if !errors.Is(err, ErrLimiterUnavailable) {
		t.Fatalf("NewValkeyLimiter() error = %v", err)
	}
}

func TestValkeyLimiterRejectsUsernameWithoutPassword(t *testing.T) {
	_, err := NewValkeyLimiter(context.Background(), ValkeyConfig{
		Address: "127.0.0.1:6379", Username: "api", Namespace: "torgnexa:test",
		ConnectTimeout: time.Second, RequestTimeout: time.Second, MaxConnections: 1, MaxKeys: 10,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewValkeyLimiter() error = %v", err)
	}
}

func TestValkeyLimiterBoundsSecretConfiguration(t *testing.T) {
	_, err := NewValkeyLimiter(context.Background(), ValkeyConfig{
		Address: "127.0.0.1:6379", Password: string(make([]byte, 4097)), Namespace: "torgnexa:test",
		ConnectTimeout: time.Second, RequestTimeout: time.Second, MaxConnections: 1, MaxKeys: 10,
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewValkeyLimiter() error = %v", err)
	}
}
