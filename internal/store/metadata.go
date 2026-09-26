package store

import (
	"context"
)

// MissingTitleMetadata returns saved titles still needing poster or overview metadata.
func (s *Store) MissingTitleMetadata(ctx context.Context) ([]Title, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT media_type, tmdb_id FROM titles
		WHERE watched = 1 AND import_status = 'complete' AND (poster_path IS NULL OR overview IS NULL)
		ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []Title
	for rows.Next() {
		var title Title
		if err := rows.Scan(&title.MediaType, &title.TMDBID); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// SetTitleMetadata fills legacy fields without changing saved credits or progress.
func (s *Store) SetTitleMetadata(ctx context.Context, mediaType string, tmdbID int64, path, overview string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE titles SET poster_path = COALESCE(poster_path, ?), overview = COALESCE(overview, ?)
		WHERE media_type = ? AND tmdb_id = ? AND watched = 1 AND import_status = 'complete'
		AND (poster_path IS NULL OR overview IS NULL)`, path, overview, mediaType, tmdbID)
	return err
}
