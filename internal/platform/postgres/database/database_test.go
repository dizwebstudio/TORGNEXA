package database

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/torgnexa/torgnexa/internal/platform/config"
)

func validConfig() config.Database {
	return config.Database{
		URL:             "postgres://user:secret@example.invalid:5432/db?sslmode=require",
		MaxOpenConns:    4,
		MaxIdleConns:    2,
		ConnMaxLifetime: 30 * time.Minute,
		ConnMaxIdleTime: 5 * time.Minute,
		ConnectTimeout:  100 * time.Millisecond,
	}
}

func TestOpenRejectsInvalidConfiguration(t *testing.T) {
	if _, err := Open(context.Background(), config.Database{}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Open() error = %v, want ErrInvalidConfig", err)
	}
	if _, err := Open(nil, validConfig()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Open(nil) error = %v, want ErrInvalidConfig", err)
	}
}

func TestOpenUnavailableDoesNotLeakDSN(t *testing.T) {
	cfg := validConfig()
	const secret = "secret-that-must-not-leak"
	cfg.URL = "postgres://user:" + secret + "@127.0.0.1:1/db?sslmode=disable"
	_, err := Open(context.Background(), cfg)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Open() error = %v, want ErrUnavailable", err)
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), cfg.URL) {
		t.Fatalf("Open() leaked connection material: %v", err)
	}
}
