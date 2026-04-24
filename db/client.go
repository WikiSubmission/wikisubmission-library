package db

import (
	"context"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the pgxpool.Pool to provide database connectivity and state management.
type DB struct {
	// Pool is the underlying PostgreSQL connection pool.
	Pool *pgxpool.Pool
	// isSyncing is an atomic flag used to prevent concurrent S3-to-DB sync operations.
	isSyncing atomic.Bool
}

// migrationFiles embeds the SQL scripts from the /migrations directory into the binary.
// This ensures that the migrations are always shipped with the application executable.
//go:embed migrations/*.sql
var migrationFiles embed.FS

// DbSetup is a high-level helper that initializes the connection pool and
// runs all pending schema migrations. It returns an error if the connection
// fails or if a migration script contains invalid SQL.
func DbSetup() (*DB, error) {
	db, err := ConnectDB()
	if err != nil {
		return nil, err
	}

	// Use a background context for migrations during startup
	err = db.RunMigrations(context.Background())
	if err != nil {
		slog.Error("Database migrations failed", slog.Any("error", err))
		db.Close()
		return nil, err
	}

	slog.Info("Database setup and migrations completed successfully")
	return db, nil
}

// RunMigrations discovers all .sql files in the embedded 'migrations' directory,
// sorts them alphabetically, and executes them against the database.
// It is idempotent assuming the SQL scripts use 'IF NOT EXISTS' clauses.
func (db *DB) RunMigrations(ctx context.Context) error {
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	// Sort files alphabetically to ensure numeric prefixes (e.g., 01_, 02_) are respected.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			path := "migrations/" + entry.Name()
			content, err := migrationFiles.ReadFile(path)
			if err != nil {
				return fmt.Errorf("failed to read migration file %s: %w", entry.Name(), err)
			}

			slog.Info("Executing database migration", "file", entry.Name())
			_, err = db.Pool.Exec(ctx, string(content))
			if err != nil {
				return fmt.Errorf("migration failed in %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// ConnectDB builds a connection string from environment variables and 
// establishes a validated connection pool with production-optimized limits.
func ConnectDB() (*DB, error) {
	required := []string{
		"DATABASE_USER", "DATABASE_PASSWORD", "DATABASE_DOMAIN",
		"DATABASE_PORT", "DATABASE_NAME", "DATABASE_SSL_MODE",
	}

	for _, env := range required {
		if os.Getenv(env) == "" {
			slog.Error("Missing required database environment variable", "variable", env)
			return nil, fmt.Errorf("missing required environment variable: %s", env)
		}
	}

	// key=value DSN (not URL) so passwords with @, ?, /, &, *, ! don't need
	// percent-encoding. pgx treats quoted values as literal.
	connStr := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		os.Getenv("DATABASE_DOMAIN"),
		os.Getenv("DATABASE_PORT"),
		os.Getenv("DATABASE_USER"),
		quoteDSN(os.Getenv("DATABASE_PASSWORD")),
		os.Getenv("DATABASE_NAME"),
		os.Getenv("DATABASE_SSL_MODE"),
	)

	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse connection string: %w", err)
	}

	// Production connection pool tuning
	config.MaxConns = 20                   // Limits max concurrent DB connections
	config.MinConns = 2                    // Maintains warm connections for performance
	config.MaxConnIdleTime = time.Minute * 5

	// Set a 10-second timeout for the initial connection attempt and ping
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		slog.Error("Unable to create database pool", slog.Any("error", err))
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Verify the connection is active
	if err := pool.Ping(ctx); err != nil {
		slog.Error("Database ping failed", slog.Any("error", err))
		return nil, fmt.Errorf("database unreachable: %w", err)
	}

	slog.Info("Successfully connected to database", 
		"host", os.Getenv("DATABASE_DOMAIN"), 
		"db", os.Getenv("DATABASE_NAME"),
	)

	return &DB{Pool: pool}, nil
}

// quoteDSN wraps a libpq key=value value in single quotes and escapes any
// embedded backslashes or single quotes, so special characters (spaces,
// @, ?, &, *, !) in passwords pass through verbatim.
func quoteDSN(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `'`, `\'`)
	return "'" + v + "'"
}

// Close gracefully shuts down the database connection pool,
// allowing existing queries to complete before closing.
func (db *DB) Close() {
	if db.Pool != nil {
		slog.Info("Closing database connection pool")
		db.Pool.Close()
	}
}