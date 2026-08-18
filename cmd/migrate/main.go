// Command migrate applies pending PostgreSQL schema migrations.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jeanmamelo/medication-api/migrations"
)

const (
	lockName      = "github.com/jeanmamelo/medication-api/schema-migrations"
	lockSQL       = "SELECT pg_advisory_lock(hashtext($1))"
	unlockSQL     = "SELECT pg_advisory_unlock(hashtext($1))"
	migrationsDDL = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version BIGINT PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`
)

var migrationName = regexp.MustCompile(`^(\d+)_.*\.up\.sql$`)

type migration struct {
	version int64
	name    string
	path    string
}

func main() {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		slog.Error("DATABASE_URL is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		slog.Error("create database pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	if err := migrate(ctx, pool); err != nil {
		slog.Error("run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations completed")
}

func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	items, err := loadMigrations()
	if err != nil {
		return err
	}

	connection, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire database connection: %w", err)
	}
	defer connection.Release()

	if _, err := connection.Exec(ctx, lockSQL, lockName); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		if _, err := connection.Exec(context.Background(), unlockSQL, lockName); err != nil {
			slog.Error("release migration lock", "error", err)
		}
	}()

	if _, err := connection.Exec(ctx, migrationsDDL); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}

	applied, err := appliedMigrations(ctx, connection)
	if err != nil {
		return err
	}
	for _, item := range items {
		if applied[item.version] {
			continue
		}
		contents, err := migrations.FS.ReadFile(item.path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", item.name, err)
		}
		transaction, err := connection.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", item.name, err)
		}
		if _, err := transaction.Exec(ctx, string(contents)); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", item.name, err)
		}
		if _, err := transaction.Exec(ctx, "INSERT INTO schema_migrations (version, name) VALUES ($1, $2)", item.version, item.name); err != nil {
			_ = transaction.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", item.name, err)
		}
		if err := transaction.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", item.name, err)
		}
		slog.Info("migration applied", "version", item.version, "name", item.name)
	}

	return nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func appliedMigrations(ctx context.Context, connection queryer) (map[int64]bool, error) {
	rows, err := connection.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]bool)
	for rows.Next() {
		var version int64
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}

func loadMigrations() ([]migration, error) {
	items := make([]migration, 0)
	err := fs.WalkDir(migrations.FS, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		match := migrationName.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version < 1 {
			return fmt.Errorf("invalid migration version in %s", entry.Name())
		}
		items = append(items, migration{version: version, name: entry.Name(), path: path})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover migrations: %w", err)
	}
	sort.Slice(items, func(left, right int) bool { return items[left].version < items[right].version })
	for index := 1; index < len(items); index++ {
		if items[index-1].version == items[index].version {
			return nil, fmt.Errorf("duplicate migration version %d", items[index].version)
		}
	}
	return items, nil
}
