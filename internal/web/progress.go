package web

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"show-lib/internal/store"
)

func (s *Server) progress(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	title, err := s.store.GetTitle(r.Context(), "tv", id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load title", http.StatusInternalServerError)
		return
	}
	if title.ImportStatus != "complete" || !title.Watched {
		http.NotFound(w, r)
		return
	}
	data := pageData{Heading: "Update " + title.Name, Kind: "progress", Title: title}
	if r.URL.Query().Get("error") == "invalid" {
		data.Error = "Select an available season and episode."
	}
	if r.URL.Query().Get("error") == "loading" {
		data.Error = "Episode credits are still loading. Try again when they finish."
	}
	if r.URL.Query().Get("error") == "source" {
		data.Error = "Special episode details are temporarily unavailable. Your saved progress is unchanged."
	}
	if title.EpisodeSyncStatus == "pending" {
		go s.syncPersonEpisodes(id)
		w.Header().Set("Refresh", "4")
	} else if title.EpisodeSyncStatus == "syncing" {
		w.Header().Set("Refresh", "4")
	}
	if title.EpisodeSyncStatus != "complete" {
		s.render(w, data)
		return
	}
	seasons, err := s.store.ProgressSeasons(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load seasons", http.StatusInternalServerError)
		return
	}
	data.ProgressSeasons = append([]store.ProgressSeason{{Number: 0, Name: "Specials"}}, seasons...)
	selected := int(title.WatchedSeason.Int64)
	if raw := r.URL.Query().Get("season"); raw != "" {
		requested, err := strconv.Atoi(raw)
		if err != nil || !progressHasSeason(data.ProgressSeasons, requested) {
			data.Error = "Select an available season."
		} else {
			selected = requested
		}
	}
	data.SelectedSeason = selected
	episodes, err := s.store.ProgressEpisodes(r.Context(), id, selected)
	if err != nil {
		http.Error(w, "could not load episodes", http.StatusInternalServerError)
		return
	}
	if selected == 0 {
		details, sourceErr := s.tmdb.GetSeason(r.Context(), id, 0)
		if sourceErr == nil {
			episodes = nil
			for _, episode := range details.Episodes {
				if episode.EpisodeNumber > 0 {
					episodes = append(episodes, store.ProgressEpisode{Number: episode.EpisodeNumber, Name: episode.Name})
				}
			}
		} else if len(episodes) == 0 {
			data.Error = "Special episode choices are temporarily unavailable. Your saved progress is unchanged."
		}
	}
	data.ProgressEpisodes = episodes
	if selected == int(title.WatchedSeason.Int64) {
		data.SelectedEpisode = int(title.WatchedEpisode.Int64)
	}
	s.render(w, data)
}

func progressHasSeason(seasons []store.ProgressSeason, requested int) bool {
	for _, season := range seasons {
		if season.Number == requested {
			return true
		}
	}
	return false
}

func (s *Server) postProgress(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	season, seasonErr := strconv.Atoi(r.FormValue("season"))
	episode, episodeErr := strconv.Atoi(r.FormValue("episode"))
	if seasonErr != nil || episodeErr != nil {
		http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d/progress?error=invalid", id), http.StatusSeeOther)
		return
	}
	err := s.store.UpdateProgress(r.Context(), id, season, episode)
	if season == 0 && errors.Is(err, store.ErrInvalidEpisode) {
		special, sourceErr := s.importer.SpecialEpisode(r.Context(), id, episode)
		if errors.Is(sourceErr, store.ErrInvalidEpisode) {
			err = store.ErrInvalidEpisode
		} else if sourceErr != nil {
			http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d/progress?season=0&error=source", id), http.StatusSeeOther)
			return
		} else {
			err = s.store.AddSpecialProgress(r.Context(), id, special)
		}
	}
	switch {
	case err == nil:
		http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d", id), http.StatusSeeOther)
	case errors.Is(err, sql.ErrNoRows):
		http.NotFound(w, r)
	case errors.Is(err, store.ErrInvalidEpisode):
		http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d/progress?error=invalid", id), http.StatusSeeOther)
	case errors.Is(err, store.ErrEpisodeDataUnavailable):
		http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d/progress?error=loading", id), http.StatusSeeOther)
	default:
		http.Error(w, "could not update progress", http.StatusInternalServerError)
	}
}

func (s *Server) retryTitleEpisodes(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	title, err := s.store.GetTitle(r.Context(), "tv", id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load title", http.StatusInternalServerError)
		return
	}
	if title.ImportStatus == "complete" && title.EpisodeSyncStatus == "failed" {
		go s.syncPersonEpisodes(id)
	}
	http.Redirect(w, r, fmt.Sprintf("/titles/tv/%d", id), http.StatusSeeOther)
}
