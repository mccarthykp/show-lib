package store

import (
	"context"
	"fmt"
	"strings"
)

type CreditEpisode struct {
	ID      int64
	Season  int
	Number  int
	Name    string
	Watched bool
}

type appearanceKey struct {
	personID int64
	category string
	role     string
	wildcard bool
}

func (s *Store) ApplyCreditAppearances(ctx context.Context, title Title, credits []CreditView) error {
	if title.MediaType == "movie" {
		for i := range credits {
			credits[i].Status = "watched"
		}
		return nil
	}
	if title.EpisodeSyncStatus != "complete" {
		status := "loading"
		if title.EpisodeSyncStatus == "failed" {
			status = "unavailable"
		}
		for i := range credits {
			credits[i].Status = status
		}
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT ec.person_tmdb_id, ec.category, ec.character_name, ec.job,
		e.id, e.tmdb_id, e.season_number, e.episode_number, COALESCE(e.name, '')
		FROM episode_person_credits ec JOIN episodes e ON e.id = ec.episode_id
		JOIN titles t ON t.id = e.series_title_id
		WHERE t.media_type = 'tv' AND t.tmdb_id = ?
		ORDER BY e.season_number, e.episode_number`, title.TMDBID)
	if err != nil {
		return err
	}
	defer rows.Close()
	appearances := map[appearanceKey][]CreditEpisode{}
	for rows.Next() {
		var personID, episodeID, episodeTMDBID int64
		var category, character, job, name string
		var season, number int
		if err := rows.Scan(&personID, &category, &character, &job, &episodeID, &episodeTMDBID, &season, &number, &name); err != nil {
			return err
		}
		role := strings.TrimSpace(character)
		if category == "crew" {
			role = strings.TrimSpace(job)
		}
		watched := title.WatchedSeason.Valid && title.WatchedEpisode.Valid &&
			((season > 0 && title.WatchedSeason.Int64 > 0 &&
				(season < int(title.WatchedSeason.Int64) || season == int(title.WatchedSeason.Int64) && number <= int(title.WatchedEpisode.Int64))) ||
				(season == 0 && title.WatchedSeason.Int64 == 0 && title.WatchedEpisodeTMDBID.Valid && episodeTMDBID == title.WatchedEpisodeTMDBID.Int64))
		episode := CreditEpisode{ID: episodeID, Season: season, Number: number, Name: name, Watched: watched}
		key := appearanceKey{personID: personID, category: category, role: role}
		appearances[key] = append(appearances[key], episode)
		wild := appearanceKey{personID: personID, category: category, wildcard: true}
		appearances[wild] = append(appearances[wild], episode)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range credits {
		credit := &credits[i]
		role := strings.TrimSpace(credit.CharacterName)
		if credit.Category == "crew" {
			role = strings.TrimSpace(credit.Job)
		}
		key := appearanceKey{personID: credit.PersonTMDBID, category: credit.Category, role: role}
		if role == "" {
			key = appearanceKey{personID: credit.PersonTMDBID, category: credit.Category, wildcard: true}
		}
		seen := map[int64]bool{}
		for _, episode := range appearances[key] {
			if seen[episode.ID] {
				continue
			}
			seen[episode.ID] = true
			credit.Episodes = append(credit.Episodes, episode)
			if episode.Watched {
				credit.Status = "watched"
			}
		}
		if credit.Status == "" {
			if len(credit.Episodes) > 0 {
				credit.Status = "later"
			} else {
				credit.Status = "unknown"
			}
		}
		var parts []string
		for j, episode := range credit.Episodes {
			if j >= 3 {
				break
			}
			parts = append(parts, fmt.Sprintf("S%d E%d", episode.Season, episode.Number))
		}
		credit.EpisodeSummary = strings.Join(parts, ", ")
		if len(credit.Episodes) > 3 {
			credit.EpisodeSummary += fmt.Sprintf(" +%d more", len(credit.Episodes)-3)
		}
	}
	return nil
}
