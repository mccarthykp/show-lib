package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"show-lib/internal/search"
	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

type searchHit struct {
	MediaType   string
	TMDBID      int64
	Name        string
	Year        string
	ReleaseDate string
	PosterPath  string
	Overview    string
	InLibrary   bool
	InWatchlist bool
}

func (s *Server) add(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := pageData{Heading: "Add a title", Kind: "add", Query: query, SearchPage: 1}
	if query == "" {
		s.render(w, data)
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 1 || page > 500 {
		page = 1
	}
	data.SearchPage = page
	if page > 1 {
		data.PreviousPage = page - 1
	}
	var hasNext bool
	results, err := s.tmdb.SearchMovies(r.Context(), query, page)
	if err != nil {
		data.Error = "Movie search is temporarily unavailable: " + err.Error()
	} else {
		if page == 1 && !hasRelevantTMDBResults(query, results.Results) {
			results.Results = append(results.Results, fuzzyTMDBFallback(r.Context(), query, s.tmdb.SearchMovies)...)
		}
		data.Results = append(data.Results, movieHits(query, results.Results)...)
		hasNext = hasNext || page < results.TotalPages
	}
	results, err = s.tmdb.SearchSeries(r.Context(), query, page)
	if err != nil {
		if data.Error != "" {
			data.Error += "; "
		}
		data.Error += "TV search is temporarily unavailable: " + err.Error()
	} else {
		if page == 1 && !hasRelevantTMDBResults(query, results.Results) {
			results.Results = append(results.Results, fuzzyTMDBFallback(r.Context(), query, s.tmdb.SearchSeries)...)
		}
		data.Results = append(data.Results, seriesHits(query, results.Results)...)
		hasNext = hasNext || page < results.TotalPages
	}
	sort.SliceStable(data.Results, func(i, j int) bool {
		left, right := search.Score(query, data.Results[i].Name), search.Score(query, data.Results[j].Name)
		if left < 0 {
			return false
		}
		return right < 0 || left < right
	})
	data.HasNext = hasNext
	if hasNext {
		data.NextPage = page + 1
	}
	if len(data.Results) > 0 {
		saved, err := s.store.ListTitles(r.Context())
		if err != nil {
			http.Error(w, "could not check library", http.StatusInternalServerError)
			return
		}
		markSavedSearchHits(data.Results, saved)
		watchlist, err := s.store.ListWatchlist(r.Context())
		if err != nil {
			http.Error(w, "could not check watchlist", http.StatusInternalServerError)
			return
		}
		markWatchlistedSearchHits(data.Results, watchlist)
	}
	s.render(w, data)
}

func markWatchlistedSearchHits(hits []searchHit, watchlist []store.WatchlistTitle) {
	watchlisted := make(map[string]bool, len(watchlist))
	for _, title := range watchlist {
		watchlisted[appearanceKey(title.MediaType, title.TMDBID)] = true
	}
	for i := range hits {
		hits[i].InWatchlist = watchlisted[appearanceKey(hits[i].MediaType, hits[i].TMDBID)]
	}
}

func markSavedSearchHits(hits []searchHit, saved []store.Title) {
	type titleKey struct {
		media string
		id    int64
	}
	inLibrary := make(map[titleKey]bool, len(saved))
	for _, title := range saved {
		inLibrary[titleKey{title.MediaType, title.TMDBID}] = true
	}
	for i := range hits {
		hits[i].InLibrary = inLibrary[titleKey{hits[i].MediaType, hits[i].TMDBID}]
	}
}

func hasRelevantTMDBResults(query string, items []tmdb.SearchItem) bool {
	for _, item := range items {
		name := item.Title
		if name == "" {
			name = item.Name
		}
		if search.Score(query, name) >= 0 {
			return true
		}
	}
	return false
}

func fuzzyTMDBFallback(ctx context.Context, query string, fetch func(context.Context, string, int) (tmdb.SearchResults, error)) []tmdb.SearchItem {
	seen := make(map[int64]bool)
	var matches []tmdb.SearchItem
	for _, variant := range fallbackQueries(query) {
		results, err := fetch(ctx, variant, 1)
		if err != nil {
			continue
		}
		for _, item := range results.Results {
			name := item.Title
			if name == "" {
				name = item.Name
			}
			if item.ID <= 0 || seen[item.ID] || search.Score(query, name) < 0 {
				continue
			}
			seen[item.ID] = true
			matches = append(matches, item)
		}
	}
	return matches
}

func fallbackQueries(query string) []string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.Is(unicode.Pd, r) || r == '\u2212' || r == '\u00ad'
	})
	if len(words) == 0 {
		return nil
	}
	var variants []string
	if len(words) > 1 && len([]rune(words[0])) >= 4 {
		variants = append(variants, words[0])
	}
	longest := words[0]
	for _, word := range words[1:] {
		if len([]rune(word)) >= len([]rune(longest)) {
			longest = word
		}
	}
	runes := []rune(longest)
	if len(words) > 1 && len(runes) >= 3 && len(runes) < 5 && (len(variants) == 0 || variants[len(variants)-1] != longest) {
		variants = append(variants, longest)
	}
	if len(runes) >= 5 {
		variants = append(variants, string(runes[:len(runes)-1]))
		if len(words) == 1 {
			variants = append(variants, string(runes[len(runes)-3:]))
		}
	}
	return variants
}

func (s *Server) preview(w http.ResponseWriter, r *http.Request) {
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	data := pageData{Kind: "preview", PreviewMedia: media, PreviewID: id}
	if media == "movie" {
		movie, err := s.movieDetails(r.Context(), id)
		if err != nil {
			http.Error(w, "TMDB title unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		data.Heading, data.PreviewYear, data.PreviewPoster = movie.Title, firstYear(movie.ReleaseDate), movie.PosterPath
	} else {
		series, err := s.seriesDetails(r.Context(), id)
		if err != nil {
			http.Error(w, "TMDB title unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		data.Heading, data.PreviewYear, data.PreviewPoster, data.Seasons = series.Name, firstYear(series.FirstAirDate), series.PosterPath, series.Seasons
		if len(series.Seasons) == 0 {
			data.Error = "TMDB has no selectable seasons for this series."
		} else {
			season := firstRegularSeason(series.Seasons)
			if raw := r.URL.Query().Get("season"); raw != "" {
				selected, err := strconv.Atoi(raw)
				if err != nil || !hasSeason(series.Seasons, selected) {
					data.Error = "That season is unavailable."
				} else {
					season = selected
				}
			}
			data.SelectedSeason = season
			seasonDetails, err := s.seasonDetails(r.Context(), id, season)
			if err != nil {
				data.Error = "Episodes are temporarily unavailable: " + err.Error()
			} else {
				data.Episodes = seasonDetails.Episodes
				sort.Slice(data.Episodes, func(i, j int) bool {
					return data.Episodes[i].EpisodeNumber < data.Episodes[j].EpisodeNumber
				})
			}
		}
	}
	if existing, err := s.store.GetTitle(r.Context(), media, id); err == nil && existing.ImportStatus == "complete" {
		data.AlreadyAdded = true
	}
	if !data.AlreadyAdded {
		watchlist, err := s.store.ListWatchlist(r.Context())
		if err != nil {
			http.Error(w, "could not check watchlist", http.StatusInternalServerError)
			return
		}
		for _, title := range watchlist {
			if title.MediaType == media && title.TMDBID == id {
				data.Watchlisted = true
				break
			}
		}
	}
	s.render(w, data)
}

func (s *Server) postAdd(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	req := store.StartImportRequest{MediaType: media, TMDBID: id}
	var cachedMovie *tmdb.Movie
	var cachedSeries *tmdb.Series
	var cachedSeasons map[int]tmdb.SeasonDetails
	if media == "movie" {
		movie, err := s.movieDetails(r.Context(), id)
		if err != nil {
			http.Error(w, "TMDB title unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		req.Name = movie.Title
		cachedMovie = &movie
	} else {
		series, err := s.seriesDetails(r.Context(), id)
		if err != nil {
			http.Error(w, "TMDB title unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		season, err1 := strconv.Atoi(r.FormValue("season"))
		episodeNumber, err2 := strconv.Atoi(r.FormValue("episode"))
		if err1 != nil || err2 != nil || !hasSeason(series.Seasons, season) {
			http.Error(w, "select a valid season and episode", http.StatusBadRequest)
			return
		}
		details, err := s.seasonDetails(r.Context(), id, season)
		if err != nil {
			http.Error(w, "TMDB episodes unavailable: "+err.Error(), http.StatusBadGateway)
			return
		}
		var episodeID int64
		for _, episode := range details.Episodes {
			if episode.EpisodeNumber == episodeNumber {
				episodeID = episode.ID
				break
			}
		}
		if episodeID <= 0 {
			http.Error(w, "select a valid season and episode", http.StatusBadRequest)
			return
		}
		req.Name, req.Season, req.Episode, req.EpisodeTMDBID = series.Name, &season, &episodeNumber, &episodeID
		cachedSeries = &series
		cachedSeasons = map[int]tmdb.SeasonDetails{season: details}
	}
	result, token, err := s.store.StartImport(r.Context(), req)
	if err != nil {
		http.Error(w, "could not start import", http.StatusInternalServerError)
		return
	}
	if result == store.ImportStarted {
		s.runImportWithDetails(media, id, token, cachedMovie, cachedSeries, cachedSeasons)
	}
	if err := s.store.RemoveFromWatchlist(r.Context(), media, id); err != nil {
		log.Printf("remove %s:%d from watchlist after adding: %v", media, id, err)
	}
	if inlineTitleAction(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/titles/%s/%d", media, id), http.StatusSeeOther)
}

func (s *Server) retry(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	title, err := s.store.GetTitle(r.Context(), media, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load import", http.StatusInternalServerError)
		return
	}
	if title.ImportStatus == "failed" {
		req := store.StartImportRequest{MediaType: media, TMDBID: id, Name: title.Name}
		if media == "tv" {
			season, episode, episodeID := int(title.WatchedSeason.Int64), int(title.WatchedEpisode.Int64), title.WatchedEpisodeTMDBID.Int64
			req.Season, req.Episode, req.EpisodeTMDBID = &season, &episode, &episodeID
		}
		result, token, err := s.store.StartImport(r.Context(), req)
		if err != nil {
			http.Error(w, "could not retry import", http.StatusInternalServerError)
			return
		}
		if result == store.ImportStarted {
			s.runImport(media, id, token)
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/titles/%s/%d", media, id), http.StatusSeeOther)
}

func (s *Server) removeConfirm(w http.ResponseWriter, r *http.Request) {
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	title, err := s.store.GetTitle(r.Context(), media, id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load title", http.StatusInternalServerError)
		return
	}
	s.render(w, pageData{Heading: "Remove " + title.Name + "?", Kind: "remove", Title: title})
}

func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteTitle(r.Context(), media, id); err != nil && !errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "could not remove title", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/titles", http.StatusSeeOther)
}

func (s *Server) runImport(media string, id int64, token string) {
	s.runImportWithDetails(media, id, token, nil, nil, nil)
}

func (s *Server) runImportWithDetails(media string, id int64, token string, movie *tmdb.Movie, series *tmdb.Series, seasons map[int]tmdb.SeasonDetails) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		var err error
		switch {
		case media == "movie" && movie != nil:
			err = s.importer.RunMovie(ctx, id, token, *movie)
		case media == "tv" && series != nil:
			err = s.importer.RunSeries(ctx, id, token, *series)
		default:
			err = s.importer.Run(ctx, media, id, token)
		}
		if err == nil {
			if media == "tv" {
				if series != nil {
					go s.syncPersonEpisodesFromDetails(id, *series, seasons)
				} else {
					go s.syncPersonEpisodes(id)
				}
			}
			return
		}
		if errors.Is(err, store.ErrImportCancelled) {
			return
		}
		log.Printf("import %s:%d failed: %v", media, id, err)
		failCtx, failCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer failCancel()
		if updateErr := s.store.FailImport(failCtx, media, id, token, err.Error()); updateErr != nil {
			log.Printf("record import failure %s:%d: %v", media, id, updateErr)
		}
	}()
}

func routeTitle(r *http.Request) (string, int64, bool) {
	media := r.PathValue("media")
	id, ok := positiveID(r.PathValue("id"))
	return media, id, ok && (media == "movie" || media == "tv")
}

func movieHits(query string, items []tmdb.SearchItem) []searchHit {
	var hits []searchHit
	for _, item := range items {
		if item.ID > 0 && search.Score(query, item.Title) >= 0 {
			hits = append(hits, searchHit{MediaType: "movie", TMDBID: item.ID, Name: item.Title,
				Year: firstYear(item.ReleaseDate), ReleaseDate: item.ReleaseDate, PosterPath: item.PosterPath, Overview: strings.TrimSpace(item.Overview)})
		}
	}
	return hits
}

func seriesHits(query string, items []tmdb.SearchItem) []searchHit {
	var hits []searchHit
	for _, item := range items {
		if item.ID > 0 && search.Score(query, item.Name) >= 0 {
			hits = append(hits, searchHit{MediaType: "tv", TMDBID: item.ID, Name: item.Name,
				Year: firstYear(item.FirstAirDate), ReleaseDate: item.FirstAirDate, PosterPath: item.PosterPath, Overview: strings.TrimSpace(item.Overview)})
		}
	}
	return hits
}

func firstYear(date string) string {
	if len(date) < 4 {
		return ""
	}
	return date[:4]
}

func firstRegularSeason(seasons []tmdb.Season) int {
	for _, season := range seasons {
		if season.SeasonNumber > 0 && season.EpisodeCount > 0 {
			return season.SeasonNumber
		}
	}
	return seasons[0].SeasonNumber
}

func hasSeason(seasons []tmdb.Season, number int) bool {
	for _, season := range seasons {
		if season.SeasonNumber == number && season.EpisodeCount > 0 {
			return true
		}
	}
	return false
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	return err == nil && strings.EqualFold(parsed.Host, r.Host)
}

func inlineTitleAction(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}
