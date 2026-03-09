package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// DB wraps the standard sql.DB with application-specific methods.
type DB struct {
	*sql.DB
}

// Open creates or opens the SQLite database at the given path.
// It creates parent directories if they do not exist, enables WAL mode,
// and turns on foreign key enforcement.
func Open(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating database directory %s: %w", dir, err)
	}

	dsn := fmt.Sprintf("file:%s?_journal_mode=WAL&_foreign_keys=on&_busy_timeout=5000", dbPath)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbPath, err)
	}

	// Verify the connection works
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	// Set connection pool limits appropriate for SQLite
	sqlDB.SetMaxOpenConns(1) // SQLite only supports one writer
	sqlDB.SetMaxIdleConns(1)

	return &DB{DB: sqlDB}, nil
}

// Migrate runs all SQL migration files from the given directory in order.
// It tracks which migrations have been applied in a _migrations table.
func (db *DB) Migrate(ctx context.Context, migrationsDir string) error {
	// Create the migrations tracking table
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS _migrations (
			id        INTEGER PRIMARY KEY AUTOINCREMENT,
			filename  TEXT NOT NULL UNIQUE,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations directory %s: %w", migrationsDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if filepath.Ext(name) != ".sql" {
			continue
		}

		// Check if already applied
		var count int
		err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM _migrations WHERE filename = ?", name).Scan(&count)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", name, err)
		}
		if count > 0 {
			continue
		}

		// Read and execute migration
		content, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", name, err)
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("beginning transaction for migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, string(content)); err != nil {
			tx.Rollback()
			return fmt.Errorf("executing migration %s: %w", name, err)
		}

		if _, err := tx.ExecContext(ctx, "INSERT INTO _migrations (filename) VALUES (?)", name); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", name, err)
		}
	}

	return nil
}
