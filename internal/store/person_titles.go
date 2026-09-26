package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

type EpisodeInput struct {
	TMDBID        int64
	SeasonNumber  int
	SeasonName    string
	EpisodeNumber int
	Name          string
	AirDate       string
	Credits       []EpisodePersonCreditInput
}

type EpisodePersonCreditInput struct {
	PersonTMDBID  int64
	PersonName    string
	ProfilePath   string
	Category      string
	CharacterName string
	Job           string
}

type PersonEpisode struct {
	Number int
	Name   string
}

type PersonSeason struct {
	Number   int
	Name     string
	Episodes []PersonEpisode
}

type PersonTitle struct {
	MediaType          string
	TMDBID             int64
	Name               string
	PosterPath         sql.NullString
	Roles              string
	Period             string
	SyncStatus         string
	WatchedStatus      string
	ReleaseDateMissing bool
	Seasons            []PersonSeason
}

func (s *Store) SetMovieReleaseDate(ctx context.Context, tmdbID int64, date string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE titles SET release_date = ? WHERE media_type = 'movie'
		AND tmdb_id = ? AND release_date IS NULL`, date, tmdbID)
	return err
}

func (s *Store) StartEpisodeSync(ctx context.Context, tmdbID int64) (int64, bool, error) {
	var titleID int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM titles
		WHERE media_type = 'tv' AND tmdb_id = ? AND watched = 1 AND import_status = 'complete'
		AND episode_sync_status IN ('pending', 'failed')`, tmdbID).Scan(&titleID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE titles SET episode_sync_status = 'syncing', episode_sync_error = NULL
		WHERE id = ? AND episode_sync_status IN ('pending', 'failed')`, titleID)
	if err != nil {
		return 0, false, err
	}
	count, err := result.RowsAffected()
	return titleID, count == 1, err
}

func (s *Store) FailEpisodeSync(ctx context.Context, titleID int64, message string) error {
	if len(message) > 300 {
		message = message[:300]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE titles SET episode_sync_status = 'failed', episode_sync_error = ?
		WHERE id = ? AND episode_sync_status = 'syncing'`, message, titleID)
	return err
}

func (s *Store) CompleteEpisodeSync(ctx context.Context, tmdbID, expectedTitleID int64, episodes []EpisodeInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var titleID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM titles WHERE media_type = 'tv' AND tmdb_id = ?
		AND watched = 1 AND import_status = 'complete' AND episode_sync_status = 'syncing' AND id = ?`, tmdbID, expectedTitleID).Scan(&titleID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrImportCancelled
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM episodes WHERE series_title_id = ?`, titleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM credits WHERE title_id = ? AND credit_key LIKE 'episode:%'`, titleID); err != nil {
		return err
	}
	for _, episode := range episodes {
		if episode.TMDBID <= 0 || episode.EpisodeNumber <= 0 {
			return fmt.Errorf("invalid episode for series %d", tmdbID)
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO episodes(series_title_id, tmdb_id, season_number, episode_number, name, season_name, air_date)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, titleID, episode.TMDBID, episode.SeasonNumber, episode.EpisodeNumber,
			episode.Name, episode.SeasonName, episode.AirDate)
		if err != nil {
			return err
		}
		episodeID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		if err := saveEpisodeCreditRows(ctx, tx, titleID, episodeID, episode.Credits); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE titles SET episode_sync_status = 'complete', episode_sync_error = NULL
		WHERE id = ?`, titleID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM people WHERE NOT EXISTS
		(SELECT 1 FROM credits WHERE credits.person_tmdb_id = people.tmdb_id)`); err != nil {
		return err
	}
	return tx.Commit()
}

func saveEpisodeCreditRows(ctx context.Context, tx *sql.Tx, titleID, episodeID int64, credits []EpisodePersonCreditInput) error {
	for _, credit := range credits {
		if credit.PersonTMDBID <= 0 || credit.PersonName == "" || credit.Category != "cast" && credit.Category != "crew" {
			return fmt.Errorf("invalid episode credit for title %d", titleID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO people(tmdb_id, name, profile_path, profile_checked_at)
			VALUES (?, ?, NULLIF(?, ''), CASE WHEN ? != '' THEN CURRENT_TIMESTAMP END)
			ON CONFLICT(tmdb_id) DO UPDATE SET name = excluded.name,
			profile_path = COALESCE(excluded.profile_path, people.profile_path),
			profile_checked_at = CASE WHEN excluded.profile_path IS NOT NULL THEN CURRENT_TIMESTAMP ELSE people.profile_checked_at END,
			updated_at = CURRENT_TIMESTAMP`, credit.PersonTMDBID, credit.PersonName, credit.ProfilePath, credit.ProfilePath); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO episode_person_credits
			(episode_id, person_tmdb_id, category, character_name, job) VALUES (?, ?, ?, ?, ?)`,
			episodeID, credit.PersonTMDBID, credit.Category, credit.CharacterName, credit.Job); err != nil {
			return err
		}
		var existing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM credits WHERE title_id = ? AND person_tmdb_id = ?
			AND category = ? AND character_name = ? AND job = ?`, titleID, credit.PersonTMDBID,
			credit.Category, credit.CharacterName, credit.Job).Scan(&existing); err != nil {
			return err
		}
		if existing == 0 {
			key := fmt.Sprintf("episode:%s:%d:%s:%s", credit.Category, credit.PersonTMDBID, credit.CharacterName, credit.Job)
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credits(title_id, person_tmdb_id, credit_key, category, character_name, department, job)
				VALUES (?, ?, ?, ?, ?, '', ?)`, titleID, credit.PersonTMDBID, key, credit.Category, credit.CharacterName, credit.Job); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) PersonTitles(ctx context.Context, personID int64) ([]PersonTitle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.media_type, t.tmdb_id, t.name, t.release_date, t.release_year, t.poster_path,
		t.episode_sync_status, c.category, COALESCE(c.character_name, ''), COALESCE(c.job, ''), COALESCE(c.department, '')
		FROM credits c JOIN titles t ON t.id = c.title_id
		WHERE c.person_tmdb_id = ? AND t.watched = 1 AND t.import_status = 'complete'
		ORDER BY t.name COLLATE NOCASE, t.tmdb_id, t.media_type, c.id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var titles []PersonTitle
	seenRoles := map[string]map[string]bool{}
	for rows.Next() {
		var media, name, status, category, character, job, department string
		var id int64
		var releaseDate sql.NullString
		var releaseYear sql.NullInt64
		var posterPath sql.NullString
		if err := rows.Scan(&media, &id, &name, &releaseDate, &releaseYear, &posterPath, &status,
			&category, &character, &job, &department); err != nil {
			return nil, err
		}
		key := fmt.Sprintf("%s:%d", media, id)
		if len(titles) == 0 || titles[len(titles)-1].MediaType != media || titles[len(titles)-1].TMDBID != id {
			title := PersonTitle{MediaType: media, TMDBID: id, Name: name, PosterPath: posterPath, SyncStatus: status}
			if media == "movie" {
				title.WatchedStatus = "watched"
				title.ReleaseDateMissing = !releaseDate.Valid
				title.Period = releaseDate.String
				if title.Period == "" && releaseYear.Valid {
					title.Period = fmt.Sprint(releaseYear.Int64)
				}
			}
			titles = append(titles, title)
			seenRoles[key] = map[string]bool{}
		}
		role := strings.TrimSpace(character)
		if category == "crew" {
			role = strings.TrimSpace(job)
			if role == "" {
				role = strings.TrimSpace(department)
			}
		}
		if role != "" && !seenRoles[key][role] {
			if titles[len(titles)-1].Roles != "" {
				titles[len(titles)-1].Roles += ", "
			}
			titles[len(titles)-1].Roles += role
			seenRoles[key][role] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range titles {
		if titles[i].MediaType != "tv" {
			continue
		}
		if titles[i].SyncStatus != "complete" {
			titles[i].WatchedStatus = "loading"
			if titles[i].SyncStatus == "failed" {
				titles[i].WatchedStatus = "unavailable"
			}
			continue
		}
		if err := s.fillPersonEpisodes(ctx, personID, &titles[i]); err != nil {
			return nil, err
		}
	}
	return titles, nil
}

func (s *Store) fillPersonEpisodes(ctx context.Context, personID int64, title *PersonTitle) error {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT e.season_number, e.season_name, e.episode_number, e.name, e.air_date,
		t.watched_season, t.watched_episode, e.tmdb_id, t.watched_episode_tmdb_id
		FROM episode_person_credits ec JOIN episodes e ON e.id = ec.episode_id
		JOIN titles t ON t.id = e.series_title_id
		WHERE ec.person_tmdb_id = ? AND t.media_type = 'tv' AND t.tmdb_id = ?
		ORDER BY e.season_number, e.episode_number`, personID, title.TMDBID)
	if err != nil {
		return err
	}
	defer rows.Close()
	firstYear, lastYear, watchedCount, totalCount := "", "", 0, 0
	for rows.Next() {
		var seasonNo, episodeNo, watchedSeason, watchedEpisode int
		var episodeTMDBID, watchedEpisodeTMDBID int64
		var seasonName, episodeName, airDate sql.NullString
		if err := rows.Scan(&seasonNo, &seasonName, &episodeNo, &episodeName, &airDate,
			&watchedSeason, &watchedEpisode, &episodeTMDBID, &watchedEpisodeTMDBID); err != nil {
			return err
		}
		totalCount++
		watched := seasonNo > 0 && watchedSeason > 0 && (seasonNo < watchedSeason || seasonNo == watchedSeason && episodeNo <= watchedEpisode) ||
			seasonNo == 0 && watchedSeason == 0 && episodeTMDBID == watchedEpisodeTMDBID
		if watched {
			title.WatchedStatus = "watched"
			watchedCount++
			if len(title.Seasons) == 0 || title.Seasons[len(title.Seasons)-1].Number != seasonNo {
				name := seasonName.String
				if name == "" {
					name = fmt.Sprintf("Season %d", seasonNo)
				}
				title.Seasons = append(title.Seasons, PersonSeason{Number: seasonNo, Name: name})
			}
			season := &title.Seasons[len(title.Seasons)-1]
			season.Episodes = append(season.Episodes, PersonEpisode{Number: episodeNo, Name: episodeName.String})
			if len(airDate.String) >= 4 {
				y := airDate.String[:4]
				if firstYear == "" || y < firstYear {
					firstYear = y
				}
				if lastYear == "" || y > lastYear {
					lastYear = y
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if totalCount == 0 {
		title.WatchedStatus = "unknown"
		return nil
	}
	if watchedCount == 0 {
		title.WatchedStatus = "later"
		return nil
	}
	if firstYear != "" {
		title.Period = firstYear
		if lastYear != firstYear {
			title.Period += "-" + lastYear
		}
		title.Period += ", "
	}
	title.Period += fmt.Sprintf("%d eps", watchedCount)
	return nil
}
