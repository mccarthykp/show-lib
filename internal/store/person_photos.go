package store

import (
	"context"
	"database/sql"
)

func (s *Store) PersonPhoto(ctx context.Context, id int64) (string, bool, error) {
	var path, checkedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT profile_path, profile_checked_at FROM people WHERE tmdb_id = ?`, id).
		Scan(&path, &checkedAt)
	return path.String, checkedAt.Valid, err
}

func (s *Store) SetPersonPhoto(ctx context.Context, id int64, path string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE people SET profile_path = NULLIF(?, ''),
		profile_checked_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP WHERE tmdb_id = ?`, path, id)
	return err
}
