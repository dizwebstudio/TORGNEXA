// Package database owns the bounded PostgreSQL connection-pool lifecycle.
package database

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/torgnexa/torgnexa/internal/platform/config"
)

var (
	// ErrInvalidConfig indicates an unusable pool configuration.
	ErrInvalidConfig = errors.New("database: invalid configuration")
	// ErrUnavailable indicates that PostgreSQL was not reachable before the
	// startup deadline. Driver errors are deliberately not exposed because they
	// may contain secret-bearing connection details.
	ErrUnavailable = errors.New("database: unavailable")
)

// Open creates a bounded pool and proves connectivity before returning it.
// Callers own the returned pool and must close it during graceful shutdown.
func Open(ctx context.Context, cfg config.Database) (*sql.DB, error) {
	if ctx == nil || !valid(cfg) {
		return nil, ErrInvalidConfig
	}

	db, err := sql.Open("pgx", cfg.URL)
	if err != nil {
		return nil, ErrUnavailable
	}
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	db.SetConnMaxIdleTime(cfg.ConnMaxIdleTime)

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err = db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, ErrUnavailable
	}
	return db, nil
}

func valid(cfg config.Database) bool {
	return cfg.URL != "" &&
		cfg.MaxOpenConns >= 1 &&
		cfg.MaxIdleConns >= 0 && cfg.MaxIdleConns <= cfg.MaxOpenConns &&
		cfg.ConnMaxLifetime >= time.Minute &&
		cfg.ConnMaxIdleTime >= time.Second &&
		cfg.ConnectTimeout >= 100*time.Millisecond
}
