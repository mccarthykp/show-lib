package store

import (
	"context"
	"database/sql"
	"errors"
)

var ErrEpisodeDataUnavailable = errors.New("episode credits are not ready")
var ErrInvalidEpisode = errors.New("episode is not in this series")

type ProgressSeason struct {
	Number int
	Name   string
}

type ProgressEpisode struct {
	Number int
	Name   string
}

func (s *Store) ProgressSeasons(ctx context.Context, tmdbID int64) ([]ProgressSeason, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.season_number, COALESCE(MAX(e.season_name), '') FROM episodes e
		JOIN titles t ON t.id = e.series_title_id
		WHERE t.media_type = 'tv' AND t.tmdb_id = ? AND t.watched = 1
		AND t.import_status = 'complete' AND t.episode_sync_status = 'complete'
		AND e.season_number > 0 AND e.episode_number > 0
		GROUP BY e.season_number ORDER BY e.season_number`, tmdbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var seasons []ProgressSeason
	for rows.Next() {
		var season ProgressSeason
		if err := rows.Scan(&season.Number, &season.Name); err != nil {
			return nil, err
		}
		seasons = append(seasons, season)
	}
	return seasons, rows.Err()
}

func (s *Store) ProgressEpisodes(ctx context.Context, tmdbID int64, season int) ([]ProgressEpisode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT e.episode_number, COALESCE(e.name, '') FROM episodes e
		JOIN titles t ON t.id = e.series_title_id
		WHERE t.media_type = 'tv' AND t.tmdb_id = ? AND t.watched = 1
		AND t.import_status = 'complete' AND t.episode_sync_status = 'complete'
		AND e.season_number = ? AND e.episode_number > 0
		ORDER BY e.episode_number`, tmdbID, season)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var episodes []ProgressEpisode
	for rows.Next() {
		var episode ProgressEpisode
		if err := rows.Scan(&episode.Number, &episode.Name); err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

func (s *Store) UpdateProgress(ctx context.Context, tmdbID int64, season, episode int) error {
	if tmdbID <= 0 || season < 0 || episode <= 0 {
		return ErrInvalidEpisode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var titleID int64
	var syncStatus string
	err = tx.QueryRowContext(ctx, `SELECT id, episode_sync_status FROM titles WHERE media_type = 'tv'
		AND tmdb_id = ? AND watched = 1 AND import_status = 'complete'`, tmdbID).Scan(&titleID, &syncStatus)
	if err != nil {
		return err
	}
	if syncStatus != "complete" {
		return ErrEpisodeDataUnavailable
	}
	var episodeID int64
	err = tx.QueryRowContext(ctx, `SELECT tmdb_id FROM episodes WHERE series_title_id = ?
		AND season_number = ? AND episode_number = ?`, titleID, season, episode).Scan(&episodeID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInvalidEpisode
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE titles SET watched_season = ?, watched_episode = ?,
		watched_episode_tmdb_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		season, episode, episodeID, titleID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddSpecialProgress(ctx context.Context, tmdbID int64, special EpisodeInput) error {
	if tmdbID <= 0 || special.SeasonNumber != 0 || special.EpisodeNumber <= 0 || special.TMDBID <= 0 {
		return ErrInvalidEpisode
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var titleID int64
	var syncStatus string
	err = tx.QueryRowContext(ctx, `SELECT id, episode_sync_status FROM titles WHERE media_type = 'tv'
		AND tmdb_id = ? AND watched = 1 AND import_status = 'complete'`, tmdbID).Scan(&titleID, &syncStatus)
	if err != nil {
		return err
	}
	if syncStatus != "complete" {
		return ErrEpisodeDataUnavailable
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO episodes(series_title_id, tmdb_id, season_number,
		episode_number, name, season_name, air_date) VALUES (?, ?, 0, ?, ?, ?, ?)
		ON CONFLICT(series_title_id, season_number, episode_number) DO UPDATE SET
		tmdb_id = excluded.tmdb_id, name = excluded.name, season_name = excluded.season_name,
		air_date = excluded.air_date`, titleID, special.TMDBID, special.EpisodeNumber,
		special.Name, special.SeasonName, special.AirDate); err != nil {
		return err
	}
	var episodeID int64
	if err := tx.QueryRowContext(ctx, `SELECT id FROM episodes WHERE series_title_id = ?
		AND season_number = 0 AND episode_number = ?`, titleID, special.EpisodeNumber).Scan(&episodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM episode_person_credits WHERE episode_id = ?`, episodeID); err != nil {
		return err
	}
	if err := saveEpisodeCreditRows(ctx, tx, titleID, episodeID, special.Credits); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE titles SET watched_season = 0, watched_episode = ?,
		watched_episode_tmdb_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		special.EpisodeNumber, special.TMDBID, titleID); err != nil {
		return err
	}
	return tx.Commit()
}
