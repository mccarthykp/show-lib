package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

type Title struct {
	MediaType            string
	TMDBID               int64
	Name                 string
	ReleaseYear          sql.NullInt64
	PosterPath           sql.NullString
	Overview             sql.NullString
	ImportStatus         string
	ImportError          sql.NullString
	Watched              bool
	WatchedSeason        sql.NullInt64
	WatchedEpisode       sql.NullInt64
	WatchedEpisodeTMDBID sql.NullInt64
	EpisodeSyncStatus    string
	EpisodeSyncError     sql.NullString
}

type Person struct {
	TMDBID int64
	Name   string
	Roles  string
}

func Open(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, true)
}

// OpenForLibraryMigration skips startup recovery writes, so a refused
// snapshot cannot alter an existing library.
func OpenForLibraryMigration(ctx context.Context, path string) (*Store, error) {
	return open(ctx, path, false)
}

func open(ctx context.Context, path string, recoverInterrupted bool) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1) // Keep connection-local SQLite PRAGMAs consistent.
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure database file: %w", err)
	}
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure SQLite: %w", err)
		}
	}
	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	if !recoverInterrupted {
		return &Store{db: db}, nil
	}
	if _, err := db.ExecContext(ctx, `UPDATE titles SET import_status = 'failed',
		import_error = 'Import interrupted; retry to continue', updated_at = CURRENT_TIMESTAMP
		WHERE import_status = 'importing'`); err != nil {
		db.Close()
		return nil, fmt.Errorf("recover interrupted imports: %w", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE titles SET episode_sync_status = 'failed',
		episode_sync_error = 'Episode import interrupted; retry to continue'
		WHERE episode_sync_status = 'syncing'`); err != nil {
		db.Close()
		return nil, fmt.Errorf("recover interrupted episode imports: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) ListTitles(ctx context.Context) ([]Title, error) {
	return s.listTitles(ctx, -1, 0)
}

func (s *Store) ListTitlesPage(ctx context.Context, limit, offset int) ([]Title, error) {
	if limit <= 0 || offset < 0 {
		return nil, errors.New("invalid title page")
	}
	return s.listTitles(ctx, limit, offset)
}

func (s *Store) listTitles(ctx context.Context, limit, offset int) ([]Title, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT media_type, tmdb_id, name, release_year, poster_path, import_status, import_error,
		watched, watched_season, watched_episode, watched_episode_tmdb_id, episode_sync_status FROM titles
		WHERE watched = 1 AND import_status = 'complete'
		ORDER BY name COLLATE NOCASE, media_type, tmdb_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []Title
	for rows.Next() {
		var title Title
		if err := rows.Scan(&title.MediaType, &title.TMDBID, &title.Name, &title.ReleaseYear, &title.PosterPath,
			&title.ImportStatus, &title.ImportError, &title.Watched, &title.WatchedSeason,
			&title.WatchedEpisode, &title.WatchedEpisodeTMDBID, &title.EpisodeSyncStatus); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

func (s *Store) GetTitle(ctx context.Context, mediaType string, tmdbID int64) (Title, error) {
	var title Title
	err := s.db.QueryRowContext(ctx, `SELECT media_type, tmdb_id, name, release_year, poster_path, overview, import_status, import_error,
		watched, watched_season, watched_episode, watched_episode_tmdb_id, episode_sync_status, episode_sync_error
		FROM titles WHERE media_type = ? AND tmdb_id = ?`,
		mediaType, tmdbID).Scan(&title.MediaType, &title.TMDBID, &title.Name, &title.ReleaseYear,
		&title.PosterPath, &title.Overview, &title.ImportStatus, &title.ImportError, &title.Watched, &title.WatchedSeason,
		&title.WatchedEpisode, &title.WatchedEpisodeTMDBID, &title.EpisodeSyncStatus, &title.EpisodeSyncError)
	return title, err
}

func (s *Store) ListPendingTitles(ctx context.Context) ([]Title, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT media_type, tmdb_id, name, release_year, poster_path, import_status, import_error,
		watched, watched_season, watched_episode, watched_episode_tmdb_id FROM titles
		WHERE import_status != 'complete' ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []Title
	for rows.Next() {
		var title Title
		if err := rows.Scan(&title.MediaType, &title.TMDBID, &title.Name, &title.ReleaseYear, &title.PosterPath,
			&title.ImportStatus, &title.ImportError, &title.Watched, &title.WatchedSeason,
			&title.WatchedEpisode, &title.WatchedEpisodeTMDBID); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT p.tmdb_id, p.name FROM people p
		JOIN credits c ON c.person_tmdb_id = p.tmdb_id
		JOIN titles t ON t.id = c.title_id
		WHERE t.watched = 1 AND t.import_status = 'complete'
		ORDER BY p.name COLLATE NOCASE, p.tmdb_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var people []Person
	for rows.Next() {
		var person Person
		if err := rows.Scan(&person.TMDBID, &person.Name); err != nil {
			return nil, err
		}
		people = append(people, person)
	}
	return people, rows.Err()
}

func (s *Store) GetPerson(ctx context.Context, tmdbID int64) (Person, error) {
	var person Person
	err := s.db.QueryRowContext(ctx, "SELECT tmdb_id, name FROM people WHERE tmdb_id = ?", tmdbID).
		Scan(&person.TMDBID, &person.Name)
	return person, err
}
