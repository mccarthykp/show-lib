package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"show-lib/internal/store"
)

func (s *Server) watchlist(w http.ResponseWriter, r *http.Request) {
	titles, err := s.store.ListWatchlist(r.Context())
	if err != nil {
		http.Error(w, "could not load watchlist", http.StatusInternalServerError)
		return
	}
	media := titleMediaFilter(r.URL.Query().Get("media"))
	if media != "" {
		filtered := titles[:0]
		for _, title := range titles {
			if title.MediaType == media {
				filtered = append(filtered, title)
			}
		}
		titles = filtered
	}
	s.render(w, pageData{Heading: "Watchlist", Kind: "watchlist", Watchlist: titles, TitleMedia: media})
}

func (s *Server) addToWatchlist(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
	if err := r.ParseMultipartForm(256 << 10); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Error(w, "invalid watchlist title", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	releaseDate := strings.TrimSpace(r.FormValue("release_date"))
	posterPath := strings.TrimSpace(r.FormValue("poster_path"))
	overview := strings.TrimSpace(r.FormValue("overview"))
	if name == "" || len(name) > 500 {
		http.Error(w, "invalid watchlist title", http.StatusBadRequest)
		return
	}
	if overviewRunes := []rune(overview); len(overviewRunes) > 20000 {
		overview = string(overviewRunes[:20000])
	}
	if len(posterPath) > 500 || posterPath != "" && posterURL("w185", posterPath) == "" {
		posterPath = ""
	}
	var year sql.NullInt64
	var date sql.NullString
	if releaseDate != "" {
		validDate := len(releaseDate) == 4
		if !validDate {
			_, err := time.Parse("2006-01-02", releaseDate)
			validDate = err == nil
		}
		if validDate {
			yearValue, err := strconv.ParseInt(firstYear(releaseDate), 10, 64)
			if err == nil && yearValue >= 1000 && yearValue <= 9999 {
				year = sql.NullInt64{Int64: yearValue, Valid: true}
				date = sql.NullString{String: releaseDate, Valid: true}
			}
		}
	}
	title := store.WatchlistTitle{MediaType: media, TMDBID: id, Name: name, ReleaseYear: year,
		ReleaseDate: date, PosterPath: sql.NullString{String: posterPath, Valid: posterPath != ""}, Overview: overview}
	if err := s.store.AddToWatchlist(r.Context(), title); err != nil {
		http.Error(w, "could not add title to watchlist", http.StatusInternalServerError)
		return
	}
	if inlineTitleAction(r) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	redirect := "/watchlist"
	if personID, ok := positiveID(r.FormValue("person_id")); ok {
		redirect = "/people/" + strconv.FormatInt(personID, 10)
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func (s *Server) removeFromWatchlist(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	media, id, ok := routeTitle(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := s.store.RemoveFromWatchlist(r.Context(), media, id); err != nil {
		http.Error(w, "could not remove title from watchlist", http.StatusInternalServerError)
		return
	}
	redirect := "/watchlist"
	if personID, ok := positiveID(r.FormValue("person_id")); ok {
		redirect = "/people/" + strconv.FormatInt(personID, 10)
	} else if filter := titleMediaFilter(r.URL.Query().Get("media")); filter != "" {
		redirect += "?media=" + filter
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}
