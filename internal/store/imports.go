package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

var ErrImportCancelled = errors.New("import was removed or superseded")

type StartImportRequest struct {
	MediaType     string
	TMDBID        int64
	Name          string
	Season        *int
	Episode       *int
	EpisodeTMDBID *int64
}

type StartImportResult string

const (
	ImportStarted StartImportResult = "started"
	ImportRunning StartImportResult = "running"
	ImportExists  StartImportResult = "exists"
)

type CreditInput struct {
	Key           string
	PersonTMDBID  int64
	PersonName    string
	ProfilePath   string
	Category      string
	CharacterName string
	Department    string
	Job           string
	BillingOrder  *int
}

type ImportData struct {
	Name        string
	ReleaseYear *int
	ReleaseDate string
	PosterPath  string
	Overview    string
	Credits     []CreditInput
}

type CreditView struct {
	PersonTMDBID   int64
	PersonName     string
	Category       string
	CharacterName  string
	Department     string
	Job            string
	BillingOrder   sql.NullInt64
	Status         string
	EpisodeSummary string
	Episodes       []CreditEpisode
}

func (s *Store) StartImport(ctx context.Context, req StartImportRequest) (StartImportResult, string, error) {
	if req.TMDBID <= 0 || strings.TrimSpace(req.Name) == "" || req.MediaType != "movie" && req.MediaType != "tv" {
		return "", "", errors.New("invalid import title")
	}
	if req.MediaType == "tv" && (req.Season == nil || req.Episode == nil || req.EpisodeTMDBID == nil) {
		return "", "", errors.New("series import requires watched episode")
	}
	if req.MediaType == "movie" && (req.Season != nil || req.Episode != nil || req.EpisodeTMDBID != nil) {
		return "", "", errors.New("movie import must not include episode progress")
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", err
	}
	token := hex.EncodeToString(bytes)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var status string
	err = tx.QueryRowContext(ctx, "SELECT import_status FROM titles WHERE media_type = ? AND tmdb_id = ?", req.MediaType, req.TMDBID).Scan(&status)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	if err == nil {
		switch status {
		case "complete":
			return ImportExists, "", nil
		case "importing":
			return ImportRunning, "", nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE titles SET name = ?, import_status = 'importing', import_error = NULL,
			import_token = ?, watched = 0, watched_season = ?, watched_episode = ?, watched_episode_tmdb_id = ?,
			updated_at = CURRENT_TIMESTAMP WHERE media_type = ? AND tmdb_id = ?`,
			req.Name, token, nullableInt(req.Season), nullableInt(req.Episode), nullableInt64(req.EpisodeTMDBID), req.MediaType, req.TMDBID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO titles(media_type, tmdb_id, name, import_status, import_token, watched,
			watched_season, watched_episode, watched_episode_tmdb_id)
			VALUES (?, ?, ?, 'importing', ?, 0, ?, ?, ?)`, req.MediaType, req.TMDBID, req.Name, token,
			nullableInt(req.Season), nullableInt(req.Episode), nullableInt64(req.EpisodeTMDBID))
	}
	if err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return ImportStarted, token, nil
}

func (s *Store) CompleteImport(ctx context.Context, mediaType string, tmdbID int64, token string, data ImportData) error {
	if strings.TrimSpace(data.Name) == "" {
		return errors.New("imported title has no name")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var titleID int64
	var status string
	var savedToken sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT id, import_status, import_token FROM titles WHERE media_type = ? AND tmdb_id = ?", mediaType, tmdbID).
		Scan(&titleID, &status, &savedToken)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrImportCancelled
	}
	if err != nil {
		return err
	}
	if status != "importing" || token == "" || savedToken.String != token {
		return ErrImportCancelled
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM episodes WHERE series_title_id = ?", titleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM credits WHERE title_id = ?", titleID); err != nil {
		return err
	}
	for _, credit := range data.Credits {
		if credit.Key == "" || credit.PersonTMDBID <= 0 || credit.PersonName == "" || credit.Category != "cast" && credit.Category != "crew" {
			return fmt.Errorf("invalid imported credit for title %s:%d", mediaType, tmdbID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO people(tmdb_id, name, profile_path, profile_checked_at)
			VALUES (?, ?, NULLIF(?, ''), CASE WHEN ? != '' THEN CURRENT_TIMESTAMP END)
			ON CONFLICT(tmdb_id) DO UPDATE SET name = excluded.name,
			profile_path = COALESCE(excluded.profile_path, people.profile_path),
			profile_checked_at = CASE WHEN excluded.profile_path IS NOT NULL THEN CURRENT_TIMESTAMP ELSE people.profile_checked_at END,
			updated_at = CURRENT_TIMESTAMP`,
			credit.PersonTMDBID, credit.PersonName, credit.ProfilePath, credit.ProfilePath); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO credits(title_id, person_tmdb_id, credit_key, category,
			character_name, department, job, billing_order) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(title_id, credit_key) DO UPDATE SET person_tmdb_id = excluded.person_tmdb_id,
			category = excluded.category, character_name = excluded.character_name,
			department = excluded.department, job = excluded.job, billing_order = excluded.billing_order`,
			titleID, credit.PersonTMDBID, credit.Key, credit.Category, credit.CharacterName,
			credit.Department, credit.Job, nullableInt(credit.BillingOrder)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE titles SET name = ?, release_year = ?, release_date = ?, poster_path = ?, overview = ?, import_status = 'complete',
		import_error = NULL, import_token = NULL, watched = 1, source_updated_at = CURRENT_TIMESTAMP,
		updated_at = CURRENT_TIMESTAMP WHERE id = ?`, data.Name, nullableInt(data.ReleaseYear), data.ReleaseDate, data.PosterPath, data.Overview, titleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE NOT EXISTS
		(SELECT 1 FROM credits WHERE credits.person_tmdb_id = people.tmdb_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FailImport(ctx context.Context, mediaType string, tmdbID int64, token, message string) error {
	if len(message) > 300 {
		message = message[:300]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE titles SET import_status = 'failed', import_error = ?,
		import_token = NULL, watched = 0, updated_at = CURRENT_TIMESTAMP
		WHERE media_type = ? AND tmdb_id = ? AND import_status = 'importing' AND import_token = ?`,
		message, mediaType, tmdbID, token)
	return err
}

func (s *Store) DeleteTitle(ctx context.Context, mediaType string, tmdbID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM titles WHERE media_type = ? AND tmdb_id = ?", mediaType, tmdbID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE NOT EXISTS
		(SELECT 1 FROM credits WHERE credits.person_tmdb_id = people.tmdb_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TitleCredits(ctx context.Context, mediaType string, tmdbID int64) ([]CreditView, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.tmdb_id, p.name, c.category, COALESCE(c.character_name, ''),
		COALESCE(c.department, ''), COALESCE(c.job, ''), c.billing_order FROM credits c
		JOIN people p ON p.tmdb_id = c.person_tmdb_id
		JOIN titles t ON t.id = c.title_id
		WHERE t.media_type = ? AND t.tmdb_id = ? AND t.import_status = 'complete'
		ORDER BY CASE c.category WHEN 'cast' THEN 0 ELSE 1 END,
		c.billing_order IS NULL, c.billing_order, p.name COLLATE NOCASE, c.id`, mediaType, tmdbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var credits []CreditView
	for rows.Next() {
		var c CreditView
		if err := rows.Scan(&c.PersonTMDBID, &c.PersonName, &c.Category, &c.CharacterName,
			&c.Department, &c.Job, &c.BillingOrder); err != nil {
			return nil, err
		}
		credits = append(credits, c)
	}
	return credits, rows.Err()
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
