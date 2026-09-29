package web

import (
	"context"
	"sync"
	"time"

	"show-lib/internal/tmdb"
)

const previewCacheTTL = 30 * time.Minute

type previewKey struct {
	media string
	id    int64
}

type previewEntry struct {
	movie   *tmdb.Movie
	series  *tmdb.Series
	seasons map[int]tmdb.SeasonDetails
	expires time.Time
}

type previewStore struct {
	mu            sync.Mutex
	entries       map[previewKey]previewEntry
	personCredits map[int64]cachedPersonCredits
}

type cachedPersonCredits struct {
	credits tmdb.PersonCombinedCredits
	expires time.Time
}

func newPreviewStore() *previewStore {
	return &previewStore{entries: make(map[previewKey]previewEntry), personCredits: make(map[int64]cachedPersonCredits)}
}

func (c *previewStore) person(id int64) (tmdb.PersonCombinedCredits, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.personCredits[id]
	if !ok || time.Now().After(entry.expires) {
		return tmdb.PersonCombinedCredits{}, false
	}
	return entry.credits, true
}

func (c *previewStore) storePerson(id int64, credits tmdb.PersonCombinedCredits) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.personCredits[id] = cachedPersonCredits{credits: credits, expires: time.Now().Add(previewCacheTTL)}
}

func (c *previewStore) movie(id int64) (tmdb.Movie, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[previewKey{"movie", id}]
	if !ok || entry.movie == nil || time.Now().After(entry.expires) {
		return tmdb.Movie{}, false
	}
	return *entry.movie, true
}

func (c *previewStore) storeMovie(movie tmdb.Movie) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copy := movie
	c.entries[previewKey{"movie", movie.ID}] = previewEntry{movie: &copy, expires: time.Now().Add(previewCacheTTL)}
}

func (c *previewStore) series(id int64) (tmdb.Series, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[previewKey{"tv", id}]
	if !ok || entry.series == nil || time.Now().After(entry.expires) {
		return tmdb.Series{}, false
	}
	return *entry.series, true
}

func (c *previewStore) storeSeries(series tmdb.Series) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := previewKey{"tv", series.ID}
	entry := c.entries[key]
	copy := series
	entry.series = &copy
	entry.expires = time.Now().Add(previewCacheTTL)
	if entry.seasons == nil {
		entry.seasons = make(map[int]tmdb.SeasonDetails)
	}
	c.entries[key] = entry
}

func (c *previewStore) season(id int64, number int) (tmdb.SeasonDetails, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[previewKey{"tv", id}]
	if !ok || time.Now().After(entry.expires) {
		return tmdb.SeasonDetails{}, false
	}
	details, ok := entry.seasons[number]
	return details, ok
}

func (c *previewStore) storeSeason(id int64, number int, details tmdb.SeasonDetails) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := previewKey{"tv", id}
	entry := c.entries[key]
	if entry.seasons == nil {
		entry.seasons = make(map[int]tmdb.SeasonDetails)
	}
	entry.seasons[number] = details
	entry.expires = time.Now().Add(previewCacheTTL)
	c.entries[key] = entry
}

func (s *Server) movieDetails(ctx context.Context, id int64) (tmdb.Movie, error) {
	if movie, ok := s.previews.movie(id); ok {
		return movie, nil
	}
	movie, err := s.tmdb.GetMovie(ctx, id)
	if err == nil {
		s.previews.storeMovie(movie)
	}
	return movie, err
}

func (s *Server) seriesDetails(ctx context.Context, id int64) (tmdb.Series, error) {
	if series, ok := s.previews.series(id); ok {
		return series, nil
	}
	series, err := s.tmdb.GetSeries(ctx, id)
	if err == nil {
		s.previews.storeSeries(series)
	}
	return series, err
}

func (s *Server) seasonDetails(ctx context.Context, id int64, number int) (tmdb.SeasonDetails, error) {
	if details, ok := s.previews.season(id, number); ok {
		return details, nil
	}
	details, err := s.tmdb.GetSeason(ctx, id, number)
	if err == nil {
		s.previews.storeSeason(id, number, details)
	}
	return details, err
}

func (s *Server) personCredits(ctx context.Context, id int64) (tmdb.PersonCombinedCredits, error) {
	if credits, ok := s.previews.person(id); ok {
		return credits, nil
	}
	credits, err := s.loadPersonCredits(ctx, id)
	if err == nil {
		s.previews.storePerson(id, credits)
	}
	return credits, err
}
