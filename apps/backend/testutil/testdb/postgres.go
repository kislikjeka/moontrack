package testdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestDB represents a test database instance
type TestDB struct {
	Container *postgres.PostgresContainer
	Pool      *pgxpool.Pool
	ConnStr   string
}

// NewTestDB creates a new test database with PostgreSQL container
func NewTestDB(ctx context.Context) (*TestDB, error) {
	// Get migrations directory path
	migrationsDir, err := getMigrationsDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get migrations dir: %w", err)
	}

	// Read migration files
	initScripts, err := readMigrationFiles(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations: %w", err)
	}

	// Start PostgreSQL container with TimescaleDB for production parity
	container, err := postgres.Run(ctx,
		"timescale/timescaledb:latest-pg15",
		postgres.WithDatabase("moontrack_test"),
		postgres.WithUsername("test"),
		postgres.WithPassword("test"),
		postgres.WithInitScripts(initScripts...),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(120*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to start postgres container: %w", err)
	}

	// Get connection string
	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to get connection string: %w", err)
	}

	// Create connection pool
	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Verify connection
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		container.Terminate(ctx)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &TestDB{
		Container: container,
		Pool:      pool,
		ConnStr:   connStr,
	}, nil
}

// Reset clears all data from the database (truncates tables)
func (db *TestDB) Reset(ctx context.Context) error {
	// Truncate tables in reverse dependency order
	tables := []string{
		"account_balances",
		"entries",
		"transactions",
		"accounts",
		"wallets",
		"users",
	}

	for _, table := range tables {
		_, err := db.Pool.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			return fmt.Errorf("failed to truncate %s: %w", table, err)
		}
	}

	return nil
}

// Close closes the connection pool and terminates the container
func (db *TestDB) Close(ctx context.Context) error {
	if db.Pool != nil {
		db.Pool.Close()
	}
	if db.Container != nil {
		return db.Container.Terminate(ctx)
	}
	return nil
}

// getMigrationsDir finds the migrations directory relative to this file
func getMigrationsDir() (string, error) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get current file path")
	}

	// Navigate from testutil/testdb/postgres.go to migrations/
	baseDir := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	migrationsDir := filepath.Join(baseDir, "migrations")

	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		return "", fmt.Errorf("migrations directory not found: %s", migrationsDir)
	}

	return migrationsDir, nil
}

// readMigrationFiles reads all .up.sql files and returns them as init scripts
func readMigrationFiles(migrationsDir string) ([]string, error) {
	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Filter and sort .up.sql files
	var upFiles []string
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".up.sql") {
			upFiles = append(upFiles, file.Name())
		}
	}
	sort.Strings(upFiles)

	// Build full paths
	var scripts []string
	for _, file := range upFiles {
		scripts = append(scripts, filepath.Join(migrationsDir, file))
	}

	return scripts, nil
}
