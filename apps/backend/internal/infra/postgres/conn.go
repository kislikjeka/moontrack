package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps a pgxpool connection pool
type DB struct {
	*pgxpool.Pool
}

// Config holds database configuration
type Config struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// NewPool creates a new database connection pool
func NewPool(ctx context.Context, cfg Config) (*DB, error) {
	config, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}

	// Configure connection pool
	if cfg.MaxConns > 0 {
		config.MaxConns = cfg.MaxConns
	} else {
		config.MaxConns = 25 // Default max connections
	}

	if cfg.MinConns > 0 {
		config.MinConns = cfg.MinConns
	} else {
		config.MinConns = 5 // Default min connections
	}

	if cfg.MaxConnLifetime > 0 {
		config.MaxConnLifetime = cfg.MaxConnLifetime
	} else {
		config.MaxConnLifetime = time.Hour // Default 1 hour
	}

	if cfg.MaxConnIdleTime > 0 {
		config.MaxConnIdleTime = cfg.MaxConnIdleTime
	} else {
		config.MaxConnIdleTime = 30 * time.Minute // Default 30 minutes
	}

	// Create connection pool
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DB{Pool: pool}, nil
}

// Close closes the database connection pool
func (db *DB) Close() {
	if db.Pool != nil {
		db.Pool.Close()
	}
}

// Health checks the database connection health
func (db *DB) Health(ctx context.Context) error {
	return db.Ping(ctx)
}
