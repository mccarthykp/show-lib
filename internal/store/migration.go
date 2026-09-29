package store

import (
	"bufio"
	"compress/gzip"
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"strings"
)

// Migration 1 creates an empty database at startup. Migration 2 is an opt-in
// snapshot of this project's current library and is never applied normally.
//
//go:embed migrations/0001_initial.sql migrations/0002_library_snapshot.sql.gz
var migrationFiles embed.FS

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin initial migration: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	var applied int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&applied); err != nil {
		return fmt.Errorf("check initial migration: %w", err)
	}
	if applied > 0 {
		// Historical ledger rows are left untouched when the database already
		// has the complete schema represented by the squashed migration 1.
		for _, query := range []string{
			"SELECT poster_path, overview, watched_episode_tmdb_id FROM titles LIMIT 0",
			"SELECT profile_path, profile_checked_at FROM people LIMIT 0",
			"SELECT id FROM credits LIMIT 0",
			"SELECT id FROM episodes LIMIT 0",
			"SELECT episode_id FROM episode_person_credits LIMIT 0",
			"SELECT media_type, tmdb_id FROM watchlist LIMIT 0",
		} {
			rows, err := tx.QueryContext(ctx, query)
			if err != nil {
				return fmt.Errorf("existing database needs its historical migrations before this release: %w", err)
			}
			rows.Close()
		}
		return tx.Commit()
	}
	var existingTables int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name NOT IN ('schema_migrations', 'sqlite_sequence')`).Scan(&existingTables); err != nil {
		return fmt.Errorf("check initial database state: %w", err)
	}
	if existingTables != 0 {
		return errors.New("database contains tables without the initial migration; back it up before upgrading")
	}
	body, err := migrationFiles.ReadFile("migrations/0001_initial.sql")
	if err != nil {
		return fmt.Errorf("read initial migration: %w", err)
	}
	for _, statement := range strings.Split(string(body), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply initial migration: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (1)"); err != nil {
		return fmt.Errorf("record initial migration: %w", err)
	}
	return tx.Commit()
}

// ApplyLibrarySnapshot imports the bundled 0002 data migration into an empty
// library. Every row and the version marker are committed together.
func (s *Store) ApplyLibrarySnapshot(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin library migration: %w", err)
	}
	defer tx.Rollback()
	var applied int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&applied); err != nil {
		return 0, fmt.Errorf("check library migration: %w", err)
	}
	if applied > 0 {
		return 0, errors.New("library migration 2 is already recorded")
	}
	for _, table := range []string{"titles", "people", "credits", "episodes", "episode_person_credits", "watchlist"} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
			return 0, fmt.Errorf("check %s before library migration: %w", table, err)
		}
		if count != 0 {
			return 0, fmt.Errorf("library migration 2 requires an empty database; %s has %d rows", table, count)
		}
	}
	file, err := migrationFiles.Open("migrations/0002_library_snapshot.sql.gz")
	if err != nil {
		return 0, fmt.Errorf("open library migration: %w", err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return 0, fmt.Errorf("decompress library migration: %w", err)
	}
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		statement := scanner.Text()
		if statement == "" || strings.HasPrefix(statement, "--") {
			continue
		}
		if !strings.HasPrefix(statement, "INSERT INTO ") || !strings.HasSuffix(statement, ";") {
			return 0, fmt.Errorf("invalid library migration statement at line %d", line)
		}
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return 0, fmt.Errorf("apply library migration line %d: %w", line, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("read library migration: %w", err)
	}
	for _, expected := range []struct {
		table string
		count int
	}{
		{"titles", 263},
		{"people", 49657},
		{"credits", 70532},
		{"episodes", 6343},
		{"episode_person_credits", 150969},
		{"watchlist", 30},
	} {
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+expected.table).Scan(&count); err != nil {
			return 0, fmt.Errorf("count migrated %s: %w", expected.table, err)
		}
		if count != expected.count {
			return 0, fmt.Errorf("library migration incomplete: got %d %s, expected %d", count, expected.table, expected.count)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (2)"); err != nil {
		return 0, fmt.Errorf("record library migration: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit library migration: %w", err)
	}
	return 263, nil
}
