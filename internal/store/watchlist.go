package store

import (
	"context"
	"database/sql"
)

type WatchlistTitle struct {
	MediaType   string
	TMDBID      int64
	Name        string
	ReleaseYear sql.NullInt64
	ReleaseDate sql.NullString
	PosterPath  sql.NullString
	Overview    string
}

func (s *Store) AddToWatchlist(ctx context.Context, title WatchlistTitle) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO watchlist
		(media_type, tmdb_id, name, release_year, release_date, poster_path, overview)
		SELECT ?, ?, ?, ?, ?, ?, ?
		WHERE NOT EXISTS (
			SELECT 1 FROM titles WHERE media_type = ? AND tmdb_id = ? AND watched = 1
		)
		ON CONFLICT(media_type, tmdb_id) DO UPDATE SET
			name = excluded.name,
			release_year = COALESCE(excluded.release_year, watchlist.release_year),
			release_date = COALESCE(excluded.release_date, watchlist.release_date),
			poster_path = COALESCE(excluded.poster_path, watchlist.poster_path),
			overview = CASE WHEN excluded.overview = '' THEN watchlist.overview ELSE excluded.overview END,
			updated_at = CURRENT_TIMESTAMP`,
		title.MediaType, title.TMDBID, title.Name, title.ReleaseYear, title.ReleaseDate, title.PosterPath, title.Overview,
		title.MediaType, title.TMDBID)
	return err
}

func (s *Store) ListWatchlist(ctx context.Context) ([]WatchlistTitle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT w.media_type, w.tmdb_id, w.name, w.release_year,
		w.release_date, w.poster_path, w.overview
		FROM watchlist w
		WHERE NOT EXISTS (
			SELECT 1 FROM titles t
			WHERE t.media_type = w.media_type AND t.tmdb_id = w.tmdb_id AND t.watched = 1
		)
		ORDER BY w.created_at DESC, w.name COLLATE NOCASE, w.media_type, w.tmdb_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []WatchlistTitle
	for rows.Next() {
		var title WatchlistTitle
		if err := rows.Scan(&title.MediaType, &title.TMDBID, &title.Name, &title.ReleaseYear,
			&title.ReleaseDate, &title.PosterPath, &title.Overview); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

func (s *Store) RemoveFromWatchlist(ctx context.Context, mediaType string, tmdbID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM watchlist WHERE media_type = ? AND tmdb_id = ?", mediaType, tmdbID)
	return err
}
