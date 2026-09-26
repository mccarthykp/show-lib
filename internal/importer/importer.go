package importer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

type Service struct {
	TMDB  *tmdb.Client
	Store *store.Store
}

func (s *Service) Run(ctx context.Context, mediaType string, id int64, token string) error {
	var data store.ImportData
	switch mediaType {
	case "movie":
		movie, err := s.TMDB.GetMovie(ctx, id)
		if err != nil {
			return fmt.Errorf("load movie: %w", err)
		}
		credits, err := s.TMDB.GetMovieCredits(ctx, id)
		if err != nil {
			return fmt.Errorf("load movie credits: %w", err)
		}
		data = movieData(movie, credits)
	case "tv":
		series, err := s.TMDB.GetSeries(ctx, id)
		if err != nil {
			return fmt.Errorf("load series: %w", err)
		}
		credits, err := s.TMDB.GetSeriesAggregateCredits(ctx, id)
		if err != nil {
			return fmt.Errorf("load series credits: %w", err)
		}
		data = seriesData(series, credits)
	default:
		return fmt.Errorf("unsupported media type %q", mediaType)
	}
	if err := s.Store.CompleteImport(ctx, mediaType, id, token, data); err != nil {
		return fmt.Errorf("save import: %w", err)
	}
	return nil
}

func (s *Service) SyncEpisodes(ctx context.Context, id int64) error {
	titleID, started, err := s.Store.StartEpisodeSync(ctx, id)
	if err != nil || !started {
		return err
	}
	if err = s.syncEpisodes(ctx, id, titleID); err != nil {
		_ = s.Store.FailEpisodeSync(context.Background(), titleID, err.Error())
	}
	return err
}

func (s *Service) syncEpisodes(ctx context.Context, id, titleID int64) error {
	title, err := s.Store.GetTitle(ctx, "tv", id)
	if err != nil {
		return fmt.Errorf("load saved progress: %w", err)
	}
	series, err := s.TMDB.GetSeries(ctx, id)
	if err != nil {
		return fmt.Errorf("load series seasons: %w", err)
	}
	var episodes []store.EpisodeInput
	for _, season := range series.Seasons {
		if season.SeasonNumber == 0 || season.EpisodeCount == 0 {
			continue
		}
		details, err := s.TMDB.GetSeason(ctx, id, season.SeasonNumber)
		if err != nil {
			return fmt.Errorf("load season %d: %w", season.SeasonNumber, err)
		}
		seasonName := details.Name
		if seasonName == "" {
			seasonName = season.Name
		}
		for _, episode := range details.Episodes {
			credits, err := s.TMDB.GetEpisodeCredits(ctx, id, season.SeasonNumber, episode.EpisodeNumber)
			if err != nil {
				return fmt.Errorf("load S%dE%d credits: %w", season.SeasonNumber, episode.EpisodeNumber, err)
			}
			item := episodeData(episode, seasonName, credits)
			item.SeasonNumber = season.SeasonNumber
			episodes = append(episodes, item)
		}
	}
	if title.WatchedSeason.Valid && title.WatchedSeason.Int64 == 0 {
		special, err := s.SpecialEpisode(ctx, id, int(title.WatchedEpisode.Int64))
		if err != nil {
			return fmt.Errorf("load selected special: %w", err)
		}
		if !title.WatchedEpisodeTMDBID.Valid || special.TMDBID != title.WatchedEpisodeTMDBID.Int64 {
			return store.ErrInvalidEpisode
		}
		episodes = append(episodes, special)
	}
	return s.Store.CompleteEpisodeSync(ctx, id, titleID, episodes)
}

func (s *Service) SpecialEpisode(ctx context.Context, id int64, number int) (store.EpisodeInput, error) {
	if number <= 0 {
		return store.EpisodeInput{}, store.ErrInvalidEpisode
	}
	details, err := s.TMDB.GetSeason(ctx, id, 0)
	if err != nil {
		return store.EpisodeInput{}, err
	}
	for _, episode := range details.Episodes {
		if episode.EpisodeNumber != number {
			continue
		}
		credits, err := s.TMDB.GetEpisodeCredits(ctx, id, 0, number)
		if err != nil {
			return store.EpisodeInput{}, err
		}
		item := episodeData(episode, details.Name, credits)
		item.SeasonNumber = 0
		return item, nil
	}
	return store.EpisodeInput{}, store.ErrInvalidEpisode
}

func episodeData(episode tmdb.Episode, seasonName string, credits tmdb.EpisodeCredits) store.EpisodeInput {
	item := store.EpisodeInput{TMDBID: episode.ID, SeasonNumber: episode.SeasonNumber,
		SeasonName: seasonName, EpisodeNumber: episode.EpisodeNumber, Name: episode.Name, AirDate: episode.AirDate}
	for _, cast := range append(credits.Cast, credits.GuestStars...) {
		item.Credits = append(item.Credits, store.EpisodePersonCreditInput{PersonTMDBID: cast.ID,
			PersonName: cast.Name, ProfilePath: cast.ProfilePath, Category: "cast", CharacterName: cast.Character})
	}
	for _, crew := range credits.Crew {
		item.Credits = append(item.Credits, store.EpisodePersonCreditInput{PersonTMDBID: crew.ID,
			PersonName: crew.Name, ProfilePath: crew.ProfilePath, Category: "crew", Job: crew.Job})
	}
	return item
}

func movieData(movie tmdb.Movie, credits tmdb.MovieCredits) store.ImportData {
	data := store.ImportData{Name: movie.Title, ReleaseYear: year(movie.ReleaseDate), ReleaseDate: movie.ReleaseDate, PosterPath: movie.PosterPath, Overview: movie.Overview}
	for _, c := range credits.Cast {
		order := c.Order
		data.Credits = append(data.Credits, store.CreditInput{
			Key: creditKey("cast", c.ID, c.CreditID, c.Character), PersonTMDBID: c.ID,
			PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "cast", CharacterName: c.Character, BillingOrder: &order,
		})
	}
	for _, c := range credits.Crew {
		data.Credits = append(data.Credits, store.CreditInput{
			Key: creditKey("crew", c.ID, c.CreditID, c.Department+":"+c.Job), PersonTMDBID: c.ID,
			PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "crew", Department: c.Department, Job: c.Job,
		})
	}
	return data
}

func seriesData(series tmdb.Series, credits tmdb.AggregateCredits) store.ImportData {
	data := store.ImportData{Name: series.Name, ReleaseYear: year(series.FirstAirDate), PosterPath: series.PosterPath, Overview: series.Overview}
	for _, c := range credits.Cast {
		order := c.Order
		if len(c.Roles) == 0 {
			data.Credits = append(data.Credits, store.CreditInput{
				Key: creditKey("cast", c.ID, "", ""), PersonTMDBID: c.ID,
				PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "cast", BillingOrder: &order,
			})
		}
		for _, role := range c.Roles {
			data.Credits = append(data.Credits, store.CreditInput{
				Key: creditKey("cast", c.ID, role.CreditID, role.Character), PersonTMDBID: c.ID,
				PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "cast", CharacterName: role.Character, BillingOrder: &order,
			})
		}
	}
	for _, c := range credits.Crew {
		if len(c.Jobs) == 0 {
			data.Credits = append(data.Credits, store.CreditInput{
				Key: creditKey("crew", c.ID, "", c.Department), PersonTMDBID: c.ID,
				PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "crew", Department: c.Department,
			})
		}
		for _, job := range c.Jobs {
			data.Credits = append(data.Credits, store.CreditInput{
				Key: creditKey("crew", c.ID, job.CreditID, c.Department+":"+job.Job), PersonTMDBID: c.ID,
				PersonName: c.Name, ProfilePath: c.ProfilePath, Category: "crew", Department: c.Department, Job: job.Job,
			})
		}
	}
	return data
}

func creditKey(category string, personID int64, creditID, role string) string {
	return fmt.Sprintf("%s:%d:%s:%s", category, personID, creditID, strings.TrimSpace(role))
}

func year(date string) *int {
	if len(date) < 4 {
		return nil
	}
	parsed, err := strconv.Atoi(date[:4])
	if err != nil || parsed < 1000 || parsed > 9999 {
		return nil
	}
	return &parsed
}
