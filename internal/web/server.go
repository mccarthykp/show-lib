package web

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"show-lib/internal/importer"
	"show-lib/internal/search"
	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

type Server struct {
	store             *store.Store
	tmdb              *tmdb.Client
	importer          *importer.Service
	previews          *previewStore
	loadPersonCredits func(context.Context, int64) (tmdb.PersonCombinedCredits, error)
	page              *template.Template
}

//go:embed live_search.js
var liveSearchJS []byte

//go:embed theme.css
var themeCSS []byte

//go:embed favicon.svg
var faviconSVG []byte

type pageData struct {
	Heading              string
	Kind                 string
	Titles               []store.Title
	Watchlist            []store.WatchlistTitle
	Pending              []store.Title
	HasMoreTitles        bool
	NextTitleOffset      int
	People               []store.Person
	HasMorePeople        bool
	NextPeopleOffset     int
	Title                store.Title
	Person               store.Person
	Cast                 []store.CreditView
	Crew                 []store.CreditView
	TopCast              []topCastPerson
	Connections          []connectionGroup
	ConnectionCount      int
	PersonTitles         []store.PersonTitle
	Appearances          []personAppearance
	HasMoreAppearances   bool
	NextAppearanceOffset int
	AppearancesError     string
	ProgressSeasons      []store.ProgressSeason
	ProgressEpisodes     []store.ProgressEpisode
	SelectedEpisode      int
	Query                string
	TitleMedia           string
	SearchPage           int
	PreviousPage         int
	HasNext              bool
	NextPage             int
	Results              []searchHit
	Error                string
	PreviewMedia         string
	PreviewID            int64
	PreviewYear          string
	PreviewPoster        string
	Seasons              []tmdb.Season
	SelectedSeason       int
	Episodes             []tmdb.Episode
	AlreadyAdded         bool
	Watchlisted          bool
}

func New(db *store.Store, tmdbClient *tmdb.Client) http.Handler {
	return newHandler(db, tmdbClient, tmdbClient.GetPersonCombinedCredits)
}

func pageTemplateFuncs() template.FuncMap {
	return template.FuncMap{"posterURL": posterURL, "releaseYear": firstYear, "releaseDate": formatReleaseDate}
}

func newHandler(db *store.Store, tmdbClient *tmdb.Client, loadPersonCredits func(context.Context, int64) (tmdb.PersonCombinedCredits, error)) http.Handler {
	s := &Server{store: db, tmdb: tmdbClient, importer: &importer.Service{TMDB: tmdbClient, Store: db}, previews: newPreviewStore(), loadPersonCredits: loadPersonCredits,
		page: template.Must(template.New("page").Funcs(pageTemplateFuncs()).Parse(pageHTML))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/titles", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /favicon.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(faviconSVG)
	})
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/favicon.svg?v=3", http.StatusFound)
	})
	mux.HandleFunc("GET /assets/live-search.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(liveSearchJS)
	})
	mux.HandleFunc("GET /assets/theme.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		_, _ = w.Write(themeCSS)
	})
	mux.HandleFunc("GET /titles", s.titles)
	mux.HandleFunc("GET /titles/more", s.moreTitles)
	mux.HandleFunc("GET /titles/{media}/{id}", s.title)
	mux.HandleFunc("GET /titles/tv/{id}/progress", s.progress)
	mux.HandleFunc("POST /titles/tv/{id}/progress", s.postProgress)
	mux.HandleFunc("POST /titles/tv/{id}/retry-episodes", s.retryTitleEpisodes)
	mux.HandleFunc("GET /titles/{media}/{id}/remove", s.removeConfirm)
	mux.HandleFunc("POST /titles/{media}/{id}/delete", s.remove)
	mux.HandleFunc("POST /titles/{media}/{id}/retry", s.retry)
	mux.HandleFunc("GET /watchlist", s.watchlist)
	mux.HandleFunc("POST /watchlist/{media}/{id}", s.addToWatchlist)
	mux.HandleFunc("POST /watchlist/{media}/{id}/delete", s.removeFromWatchlist)
	mux.HandleFunc("GET /people", s.people)
	mux.HandleFunc("GET /people/more", s.morePeople)
	mux.HandleFunc("GET /people/{id}/photo", s.personPhoto)
	mux.HandleFunc("GET /people/{id}/appearances/more", s.morePersonAppearances)
	mux.HandleFunc("GET /people/{id}", s.person)
	mux.HandleFunc("GET /people/{id}/titles/{media}/{titleID}/remove", s.removePersonTitleConfirm)
	mux.HandleFunc("POST /people/{id}/titles/{media}/{titleID}/delete", s.removePersonTitle)
	mux.HandleFunc("POST /people/{id}/retry-episodes", s.retryPersonEpisodes)
	mux.HandleFunc("GET /add", s.add)
	mux.HandleFunc("GET /add/{media}/{id}", s.preview)
	mux.HandleFunc("POST /add/{media}/{id}", s.postAdd)
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) titles(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	media := titleMediaFilter(r.URL.Query().Get("media"))
	titles, hasMore, err := s.titleBatch(r.Context(), query, media, 0)
	if err != nil {
		http.Error(w, "could not load titles", http.StatusInternalServerError)
		return
	}
	pending, err := s.store.ListPendingTitles(r.Context())
	if err != nil {
		http.Error(w, "could not load imports", http.StatusInternalServerError)
		return
	}
	s.render(w, pageData{Heading: "Titles", Kind: "titles", Titles: titles, Pending: pending,
		Query: query, TitleMedia: media, HasMoreTitles: hasMore, NextTitleOffset: len(titles)})
}

const resultBatchSize = 12

func (s *Server) titleBatch(ctx context.Context, query, media string, offset int) ([]store.Title, bool, error) {
	var titles []store.Title
	var err error
	if query == "" && media == "" {
		titles, err = s.store.ListTitlesPage(ctx, resultBatchSize+1, offset)
	} else {
		var all []store.Title
		var searchErr error
		if query == "" {
			all, searchErr = s.store.ListTitles(ctx)
		} else {
			all, searchErr = s.store.SearchTitles(ctx, query)
		}
		if searchErr != nil {
			return nil, false, searchErr
		}
		if media != "" {
			filtered := all[:0]
			for _, title := range all {
				if title.MediaType == media {
					filtered = append(filtered, title)
				}
			}
			all = filtered
		}
		if offset < len(all) {
			titles = all[offset:min(offset+resultBatchSize+1, len(all))]
		}
	}
	if err != nil {
		return nil, false, err
	}
	hasMore := len(titles) > resultBatchSize
	if hasMore {
		titles = titles[:resultBatchSize]
	}
	return titles, hasMore, nil
}

func titleMediaFilter(value string) string {
	if value == "movie" || value == "tv" {
		return value
	}
	return ""
}

func (s *Server) moreTitles(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid title offset", http.StatusBadRequest)
		return
	}
	titles, hasMore, err := s.titleBatch(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")),
		titleMediaFilter(r.URL.Query().Get("media")), offset)
	if err != nil {
		http.Error(w, "could not load titles", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if hasMore {
		w.Header().Set("X-Has-More", "true")
	}
	_ = s.page.ExecuteTemplate(w, "titleRows", titles)
}

func (s *Server) title(w http.ResponseWriter, r *http.Request) {
	media := r.PathValue("media")
	if media != "movie" && media != "tv" {
		http.NotFound(w, r)
		return
	}
	id, ok := positiveID(r.PathValue("id"))
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
	data := pageData{Heading: title.Name, Kind: "title", Title: title, Query: strings.TrimSpace(r.URL.Query().Get("q"))}
	if title.ImportStatus == "importing" {
		w.Header().Set("Refresh", "2")
	}
	if title.ImportStatus == "complete" {
		if media == "tv" {
			if title.EpisodeSyncStatus == "pending" {
				go s.syncPersonEpisodes(id)
				w.Header().Set("Refresh", "4")
			} else if title.EpisodeSyncStatus == "syncing" {
				w.Header().Set("Refresh", "4")
			}
		}
		credits, err := s.store.TitleCredits(r.Context(), media, id)
		if err != nil {
			http.Error(w, "could not load credits", http.StatusInternalServerError)
			return
		}
		if err := s.store.ApplyCreditAppearances(r.Context(), title, credits); err != nil {
			http.Error(w, "could not load episode credits", http.StatusInternalServerError)
			return
		}
		data.TopCast = rankTopCast(credits, media)
		if media == "movie" || title.EpisodeSyncStatus == "complete" {
			data.Connections, err = s.connections(r.Context(), title, credits)
			if err != nil {
				http.Error(w, "could not load connections", http.StatusInternalServerError)
				return
			}
			for _, group := range data.Connections {
				data.ConnectionCount += len(group.Details)
			}
		}
		for _, credit := range credits {
			if data.Query != "" && !creditMatches(data.Query, credit) {
				continue
			}
			if credit.Category == "cast" {
				data.Cast = append(data.Cast, credit)
			} else {
				data.Crew = append(data.Crew, credit)
			}
		}
	}
	s.render(w, data)
}

func creditMatches(query string, credit store.CreditView) bool {
	for _, field := range []string{credit.PersonName, credit.CharacterName, credit.Job, credit.Department} {
		if search.Score(query, field) >= 0 {
			return true
		}
	}
	return false
}

func (s *Server) people(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	data := pageData{Heading: "People", Kind: "people", Query: query}
	if query == "" {
		s.render(w, data)
		return
	}
	people, hasMore, err := s.peopleBatch(r.Context(), query, 0)
	if err != nil {
		http.Error(w, "could not search people", http.StatusInternalServerError)
		return
	}
	data.People = people
	data.HasMorePeople = hasMore
	data.NextPeopleOffset = len(people)
	s.render(w, data)
}

func (s *Server) peopleBatch(ctx context.Context, query string, offset int) ([]store.Person, bool, error) {
	all, err := s.store.SearchPeople(ctx, query)
	if err != nil {
		return nil, false, err
	}
	if offset >= len(all) {
		return nil, false, nil
	}
	end := min(offset+resultBatchSize, len(all))
	return all[offset:end], end < len(all), nil
}

func (s *Server) morePeople(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid people offset", http.StatusBadRequest)
		return
	}
	people, hasMore, err := s.peopleBatch(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), offset)
	if err != nil {
		http.Error(w, "could not load people", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if hasMore {
		w.Header().Set("X-Has-More", "true")
	}
	_ = s.page.ExecuteTemplate(w, "peopleRows", people)
}

func (s *Server) personPhoto(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	path, checked, err := s.store.PersonPhoto(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load person photo", http.StatusInternalServerError)
		return
	}
	if !checked {
		person, err := s.tmdb.GetPerson(r.Context(), id)
		if err == nil {
			path = person.ProfilePath
			_ = s.store.SetPersonPhoto(r.Context(), id, path)
		}
	}
	size := "w185"
	if r.URL.Query().Get("size") == "w342" {
		size = "w342"
	}
	if url := posterURL(size, path); url != "" {
		w.Header().Set("Cache-Control", "private, max-age=86400")
		http.Redirect(w, r, url, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 72 108"><rect width="72" height="108" fill="#24342e"/><circle cx="36" cy="39" r="15" fill="#647a70"/><path d="M8 104c0-19 12-34 28-34s28 15 28 34" fill="#647a70"/></svg>`))
}

func (s *Server) person(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	person, err := s.store.GetPerson(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "could not load person", http.StatusInternalServerError)
		return
	}
	titles, err := s.store.PersonTitles(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load person credits", http.StatusInternalServerError)
		return
	}
	updatedMovieDate := false
	for _, title := range titles {
		if title.MediaType != "movie" || !title.ReleaseDateMissing {
			continue
		}
		movie, err := s.tmdb.GetMovie(r.Context(), title.TMDBID)
		if err != nil {
			continue
		}
		if err := s.store.SetMovieReleaseDate(r.Context(), title.TMDBID, movie.ReleaseDate); err == nil {
			updatedMovieDate = true
		}
	}
	if updatedMovieDate {
		titles, err = s.store.PersonTitles(r.Context(), id)
		if err != nil {
			http.Error(w, "could not load person credits", http.StatusInternalServerError)
			return
		}
	}
	refresh := false
	for _, title := range titles {
		if title.MediaType != "tv" {
			continue
		}
		if title.SyncStatus == "pending" {
			refresh = true
			go s.syncPersonEpisodes(title.TMDBID)
		} else if title.SyncStatus == "syncing" {
			refresh = true
		}
	}
	if refresh {
		w.Header().Set("Refresh", "4")
	}
	credits, creditsErr := s.personCredits(r.Context(), id)
	watched, unwatched := groupPersonAppearances(id, titles, credits)
	watchlist, err := s.store.ListWatchlist(r.Context())
	if err != nil {
		http.Error(w, "could not load watchlist", http.StatusInternalServerError)
		return
	}
	markWatchlisted(unwatched, watchlist)
	appearances, hasMoreAppearances := appearanceBatch(mergePersonAppearances(watched, unwatched), 0)
	data := pageData{Heading: person.Name, Kind: "person", Person: person,
		PersonTitles: titles, Appearances: appearances, HasMoreAppearances: hasMoreAppearances,
		NextAppearanceOffset: len(appearances)}
	if creditsErr != nil {
		data.AppearancesError = "Additional appearances are temporarily unavailable."
	}
	s.render(w, data)
}

func appearanceBatch(items []personAppearance, offset int) ([]personAppearance, bool) {
	if offset >= len(items) {
		return nil, false
	}
	end := min(offset+resultBatchSize, len(items))
	return items[offset:end], end < len(items)
}

func (s *Server) morePersonAppearances(w http.ResponseWriter, r *http.Request) {
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.GetPerson(r.Context(), id); errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	} else if err != nil {
		http.Error(w, "could not load person", http.StatusInternalServerError)
		return
	}
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid appearance offset", http.StatusBadRequest)
		return
	}
	titles, err := s.store.PersonTitles(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load person credits", http.StatusInternalServerError)
		return
	}
	credits, _ := s.personCredits(r.Context(), id)
	watched, unwatched := groupPersonAppearances(id, titles, credits)
	watchlist, err := s.store.ListWatchlist(r.Context())
	if err != nil {
		http.Error(w, "could not load watchlist", http.StatusInternalServerError)
		return
	}
	markWatchlisted(unwatched, watchlist)
	items := mergePersonAppearances(watched, unwatched)
	items, hasMore := appearanceBatch(items, offset)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if hasMore {
		w.Header().Set("X-Has-More", "true")
	}
	_ = s.page.ExecuteTemplate(w, "personAppearanceRows", items)
}

func (s *Server) retryPersonEpisodes(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "invalid request origin", http.StatusForbidden)
		return
	}
	id, ok := positiveID(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	titles, err := s.store.PersonTitles(r.Context(), id)
	if err != nil {
		http.Error(w, "could not load person credits", http.StatusInternalServerError)
		return
	}
	for _, title := range titles {
		if title.MediaType == "tv" && title.SyncStatus == "failed" {
			go s.syncPersonEpisodes(title.TMDBID)
		}
	}
	http.Redirect(w, r, "/people/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
}

func (s *Server) syncPersonEpisodes(id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	if err := s.importer.SyncEpisodes(ctx, id); err != nil && !errors.Is(err, store.ErrImportCancelled) {
		log.Printf("episode credits for tv:%d failed: %v", id, err)
	}
}

func (s *Server) syncPersonEpisodesFromDetails(id int64, series tmdb.Series, seasons map[int]tmdb.SeasonDetails) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	if err := s.importer.SyncEpisodesFromDetails(ctx, id, &series, seasons); err != nil && !errors.Is(err, store.ErrImportCancelled) {
		log.Printf("episode credits for tv:%d failed: %v", id, err)
	}
}

func (s *Server) render(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.page.Execute(w, data); err != nil {
		// A broken connection may cause a write error after the response has started.
		return
	}
}

func posterURL(size, path string) string {
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#\\\"'<> ") {
		return ""
	}
	return "https://image.tmdb.org/t/p/" + size + path
}

func positiveID(raw string) (int64, bool) {
	if raw == "" || strings.HasPrefix(raw, "+") || strings.HasPrefix(raw, "-") {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

const pageHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#000000">
  <link rel="icon" type="image/svg+xml" sizes="any" href="/favicon.svg?v=3">
  <link rel="shortcut icon" href="/favicon.svg?v=3">
  <title>{{.Heading}} · Show-lib</title>
  <script defer src="/assets/live-search.js"></script>
  <link rel="stylesheet" href="/assets/theme.css?v=1">
</head>
<body class="page-{{.Kind}}">
  <header class="site-header">
    <nav class="primary-nav" aria-label="Primary navigation">
      <a class="brand" href="/titles" aria-label="Show-lib home"><svg viewBox="0 0 36 36" aria-hidden="true" focusable="false"><defs><mask id="nav-reel-cutouts"><rect width="36" height="36" fill="white"/><g fill="black"><circle cx="18" cy="18" r="2"/><circle cx="18" cy="8.5" r="3"/><circle cx="27" cy="15" r="3"/><circle cx="23.5" cy="26" r="3"/><circle cx="12.5" cy="26" r="3"/><circle cx="9" cy="15" r="3"/></g></mask></defs><circle cx="18" cy="18" r="15" fill="currentColor" mask="url(#nav-reel-cutouts)"/></svg></a>
      <div class="nav-links">
        <a href="/titles" {{if or (eq .Kind "titles") (eq .Kind "title") (eq .Kind "progress") (eq .Kind "remove")}}aria-current="page"{{end}}>Titles</a>
        <a href="/people" {{if or (eq .Kind "people") (eq .Kind "person") (eq .Kind "remove-person")}}aria-current="page"{{end}}>People</a>
        <a href="/watchlist" {{if eq .Kind "watchlist"}}aria-current="page"{{end}}>Watchlist</a>
      </div>
      <a class="nav-add" href="/add" aria-label="Add a title" {{if or (eq .Kind "add") (eq .Kind "preview")}}aria-current="page"{{end}}><svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><path d="M12 5v14M5 12h14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg></a>
    </nav>
  </header>
  <main>
    <p class="eyebrow{{if or (eq .Kind "title") (eq .Kind "person")}} title-eyebrow{{end}}">{{if eq .Kind "titles"}}FILM &amp; TELEVISION LIBRARY{{else if eq .Kind "people"}}CAST &amp; CREW LIBRARY{{else if eq .Kind "watchlist"}}WATCHLIST{{else if eq .Kind "person"}}PROFILE{{else if eq .Kind "title"}}{{if eq .Title.MediaType "movie"}}FILM{{else}}TELEVISION{{end}}{{else}}Personal film &amp; television library{{end}}</p>
    {{if eq .Kind "title"}}
      <div class="title-hero">
        {{with posterURL "w342" .Title.PosterPath.String}}<img class="title-poster" src="{{.}}" alt="" width="208" height="312">{{else}}<span class="title-poster poster-placeholder" aria-hidden="true"></span>{{end}}
        <div class="title-hero-copy"><div class="title-identification"><h1>{{.Heading}}</h1><p class="muted title-meta">{{.Title.MediaType}}{{if .Title.ReleaseYear.Valid}} · {{.Title.ReleaseYear.Int64}}{{end}}</p><span class="title-state-tag watched-tag">Watched</span></div><section class="title-overview" aria-label="Overview"><h2>Overview</h2><p>{{if .Title.Overview.String}}{{.Title.Overview.String}}{{else}}Overview unavailable.{{end}}</p>{{if .Title.WatchedSeason.Valid}}<p class="title-progress">{{if eq .Title.WatchedSeason.Int64 0}}Watched special episode {{.Title.WatchedEpisode.Int64}}{{else}}Watched through season {{.Title.WatchedSeason.Int64}}, episode {{.Title.WatchedEpisode.Int64}}{{end}} · <a href="/titles/tv/{{.Title.TMDBID}}/progress">Update progress</a></p>{{end}}</section></div>
      </div>
    {{else if eq .Kind "person"}}
      <div class="title-hero">
        <img class="title-poster" src="/people/{{.Person.TMDBID}}/photo?size=w342" alt="" width="208" height="312">
        <div class="title-hero-copy"><div class="title-identification"><h1>{{.Heading}}</h1></div></div>
      </div>
    {{else}}
      {{if eq .Kind "preview"}}<div class="title-hero preview-hero">{{with posterURL "w342" .PreviewPoster}}<img class="title-poster" src="{{.}}" alt="" width="160" height="240">{{else}}<span class="title-poster poster-placeholder" aria-hidden="true"></span>{{end}}<div class="title-hero-copy"><div class="title-identification"><h1>{{.Heading}}</h1><p class="muted title-meta">{{.PreviewMedia}}{{if .PreviewYear}} · {{.PreviewYear}}{{end}}</p>{{if .AlreadyAdded}}<span class="title-state-tag watched-tag">Watched</span>{{else if .Watchlisted}}<span class="title-state-tag watchlisted-tag">Watchlisted</span>{{end}}</div></div></div>{{else}}<h1>{{.Heading}}</h1>{{end}}
    {{end}}
    {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
    {{if eq .Kind "titles"}}
      <nav class="panel title-filters" aria-label="Filter watched titles">
        <a href="/titles{{if .Query}}?q={{.Query | urlquery}}{{end}}" {{if not .TitleMedia}}aria-current="page"{{end}}>All</a>
        <a href="/titles?media=movie{{if .Query}}&amp;q={{.Query | urlquery}}{{end}}" {{if eq .TitleMedia "movie"}}aria-current="page"{{end}}>Movies</a>
        <a href="/titles?media=tv{{if .Query}}&amp;q={{.Query | urlquery}}{{end}}" {{if eq .TitleMedia "tv"}}aria-current="page"{{end}}>TV</a>
      </nav>
      <form action="/titles" method="get" class="row panel" data-live-search="titles">{{if .TitleMedia}}<input name="media" type="hidden" value="{{.TitleMedia}}">{{end}}<label>Search titles in your library<input name="q" type="text" value="{{.Query}}" placeholder="Title or person" autocomplete="off" required></label></form>
      <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
      <section id="live-results" aria-label="Title search results">
      {{if .Titles}}<ul class="item-list">{{template "titleRows" .Titles}}</ul>{{if .HasMoreTitles}}<div class="load-more-results" data-load-more-results data-endpoint="/titles/more{{if .TitleMedia}}?media={{.TitleMedia}}{{end}}" data-offset="{{.NextTitleOffset}}" data-query="{{.Query}}" role="status" aria-live="polite"></div>{{end}}
      {{else if or .Query .TitleMedia}}<p class="muted">No matching titles in your watched library.</p>
      {{else}}<p class="muted">Your watched library is empty. <a href="/add">Add a title</a>.</p>{{end}}
      </section>
      {{if .Pending}}<h2>Imports</h2><ul class="item-list">{{range .Pending}}<li><a class="item-link" href="/titles/{{.MediaType}}/{{.TMDBID}}"><span class="item-title title-list-item">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .ReleaseYear.Valid}} · {{.ReleaseYear.Int64}}{{end}} · {{.ImportStatus}}</span></span></span></a></li>{{end}}</ul>{{end}}
    {{else if eq .Kind "watchlist"}}
      <nav class="panel title-filters" aria-label="Filter watchlist titles" data-watchlist-filters>
        <a href="/watchlist" {{if not .TitleMedia}}aria-current="page"{{end}}>All</a>
        <a href="/watchlist?media=movie" {{if eq .TitleMedia "movie"}}aria-current="page"{{end}}>Movies</a>
        <a href="/watchlist?media=tv" {{if eq .TitleMedia "tv"}}aria-current="page"{{end}}>TV</a>
      </nav>
      <button class="watchlist-randomizer-button" type="button" data-watchlist-randomizer {{if not .Watchlist}}disabled{{end}}>Randomizer!</button>
      <p class="visually-hidden" data-watchlist-randomizer-status role="status" aria-live="polite"></p>
      <div data-watchlist-results>
        {{if .Watchlist}}<section aria-label="Watchlist titles" data-watchlist-titles>{{template "watchlistRows" .}}</section>
        {{else if .TitleMedia}}<p class="muted">No matching titles in your watchlist.</p>
        {{else}}<p class="muted">Your watchlist is empty. Add unwatched titles from a person's profile.</p>{{end}}
      </div>
    {{else if eq .Kind "people"}}
      <form action="/people" method="get" class="row panel" data-live-search="people">
        <label>Search people in your library<input name="q" type="text" value="{{.Query}}" placeholder="Person, role or crew job" autocomplete="off" required></label>
      </form>
      <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
      <section id="live-results" aria-label="People search results">
      {{if .Query}}
        {{if .People}}<h2>People</h2><ul class="item-list">{{template "peopleRows" .People}}</ul>{{if .HasMorePeople}}<div class="load-more-results" data-load-more-results data-endpoint="/people/more" data-offset="{{.NextPeopleOffset}}" data-query="{{.Query}}" role="status" aria-live="polite"></div>{{end}}
        {{else}}<p class="muted">No matching people in your watched library.</p>{{end}}
      {{end}}
      </section>
    {{else if eq .Kind "add"}}
      <form action="/add" method="get" class="row panel" data-live-search="add">
        <label>Search movies and TV series<input name="q" type="text" value="{{.Query}}" placeholder="Title" autocomplete="off" required></label>
      </form>
      <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
      <section id="live-results" aria-label="TMDB search results">
      {{if .Query}}
        {{if .Results}}<h2>Results</h2><div class="add-title-results">{{template "addRows" .Results}}</div>
        {{else if not .Error}}<p>No matching titles found.</p>{{end}}
        <p>{{if .PreviousPage}}<a href="/add?q={{.Query | urlquery}}&amp;page={{.PreviousPage}}">Previous page</a>{{end}} {{if .HasNext}}<a href="/add?q={{.Query | urlquery}}&amp;page={{.NextPage}}">Next page</a>{{end}}</p>
      {{end}}
      </section>
    {{else if eq .Kind "preview"}}
      {{if .AlreadyAdded}}<p>This title is already in your library. <a href="/titles/{{.PreviewMedia}}/{{.PreviewID}}">Open it</a>.</p>
      {{else if eq .PreviewMedia "movie"}}<form action="/add/movie/{{.PreviewID}}" method="post"><button type="submit">Add to watched</button></form>
      {{else}}
        {{if .Seasons}}<form action="/add/tv/{{.PreviewID}}" method="get" class="row"><label>Season<select name="season">{{range .Seasons}}{{if gt .EpisodeCount 0}}<option value="{{.SeasonNumber}}" {{if eq .SeasonNumber $.SelectedSeason}}selected{{end}}>{{.Name}} ({{.EpisodeCount}} episodes)</option>{{end}}{{end}}</select></label><button type="submit">Show episodes</button></form>{{end}}
        {{if .Episodes}}<form action="/add/tv/{{.PreviewID}}" method="post" class="row"><input type="hidden" name="season" value="{{.SelectedSeason}}"><label>Last episode watched<select name="episode">{{range .Episodes}}<option value="{{.EpisodeNumber}}">Episode {{.EpisodeNumber}}{{if .Name}} — {{.Name}}{{end}}</option>{{end}}</select></label><button type="submit">Add to watched</button></form>
        {{else}}<p class="muted">No episodes are available for this season.</p>{{end}}
      {{end}}
    {{else if eq .Kind "title"}}
      {{if eq .Title.ImportStatus "complete"}}{{if .Connections}}<section class="connection-panel" aria-label="Connections"><details class="connection-disclosure"><summary>{{.ConnectionCount}} {{if eq .ConnectionCount 1}}connection{{else}}connections{{end}}</summary><div class="connection-content">{{range .Connections}}{{$byPerson := .ByPerson}}<div class="connection-group"><p class="connection-line">{{range $i, $person := .People}}{{if $i}}, {{end}}<a href="/people/{{$person.ID}}">{{$person.Name}}</a>{{end}} <span class="connection-relation">{{if gt (len .People) 1}}also appear{{else}}also appears{{end}} in</span> {{range $i, $title := .Titles}}{{if $i}}, {{end}}<a href="/titles/{{$title.MediaType}}/{{$title.ID}}">{{$title.Name}}</a>{{end}}.</p>
          <details class="connection-info"><summary>View roles and watched episodes</summary><ul class="connection-roles">{{if $byPerson}}{{with index .Details 0}}<li class="connection-current-credit">{{.HereRoles}} in {{$.Title.Name}}
            {{if .HereEpisodes}}<details class="connection-evidence"><summary>{{len .HereEpisodes}} credited {{if eq (len .HereEpisodes) 1}}episode{{else}}episodes{{end}} in {{$.Title.Name}}</summary><ul>{{range .HereEpisodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}
          </li>{{end}}{{end}}{{range .Details}}<li>{{if not $byPerson}}<a href="/people/{{.Person.ID}}">{{.Person.Name}}</a>: {{end}}{{.OtherRoles}} in <a href="/titles/{{.Other.MediaType}}/{{.Other.ID}}">{{.Other.Name}}</a>
            {{if .OtherEpisodes}}<details class="connection-evidence"><summary>{{len .OtherEpisodes}} credited {{if eq (len .OtherEpisodes) 1}}episode{{else}}episodes{{end}} in {{.Other.Name}}</summary><ul>{{range .OtherEpisodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}
          </li>{{end}}</ul></details></div>{{end}}</div></details></section>
        {{else if and (eq .Title.MediaType "tv") (ne .Title.EpisodeSyncStatus "complete")}}<p class="muted">Connections will appear after episode credits finish loading.</p>
        {{else}}<p class="no-connections">No connections</p>{{end}}{{end}}
      {{if .TopCast}}<section class="top-cast-panel" aria-labelledby="top-cast-heading" data-cast-carousel><h2 id="top-cast-heading">Top cast</h2><div class="top-cast-viewport"><div class="top-cast-list" data-cast-list tabindex="0">{{range .TopCast}}<a class="top-cast-card" href="/people/{{.ID}}"><img class="poster-thumb top-cast-photo" src="/people/{{.ID}}/photo" alt="" width="72" height="108"><span>{{.Name}}</span></a>{{end}}</div></div><div class="top-cast-progress" data-cast-progress aria-hidden="true" hidden><span class="top-cast-progress-dot is-active" data-cast-progress-dot></span><span class="top-cast-progress-dot" data-cast-progress-dot></span><span class="top-cast-progress-dot" data-cast-progress-dot></span></div></section>{{end}}
      {{if eq .Title.ImportStatus "importing"}}<p>Importing cast and crew… This page updates automatically.</p>
      {{else if eq .Title.ImportStatus "failed"}}<p class="error">Import failed{{if .Title.ImportError.Valid}}: {{.Title.ImportError.String}}{{end}}</p><form action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/retry" method="post"><button type="submit">Retry import</button></form>
      {{else}}
        {{if eq .Title.MediaType "tv"}}{{if eq .Title.EpisodeSyncStatus "failed"}}<p class="error">Episode credits could not be loaded{{if .Title.EpisodeSyncError.Valid}}: {{.Title.EpisodeSyncError.String}}{{end}}</p><form action="/titles/tv/{{.Title.TMDBID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>{{else if ne .Title.EpisodeSyncStatus "complete"}}<p class="muted">Loading episode credits… This page updates automatically.</p>{{end}}{{end}}
        <form action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}" method="get" class="row panel" data-live-search="credits"><label>Search cast and crew<input name="q" type="text" value="{{.Query}}" placeholder="Person, role or job" autocomplete="off" required></label></form>
        <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
        <section id="live-results" aria-label="Cast and crew search results">
        {{if and .Query (not (or .Cast .Crew))}}<p class="muted">No matching cast or crew for this title.</p>{{end}}
        <details><summary>Cast ({{len .Cast}})</summary><ul class="credits">{{range .Cast}}<li><a href="/people/{{.PersonTMDBID}}">{{.PersonName}}{{if .CharacterName}} — {{.CharacterName}}{{end}} {{if ne .Status "watched"}}<span class="credit-state {{.Status}}">{{if eq .Status "later"}}· Later episodes{{else if eq .Status "unknown"}}· Episode details unavailable{{else if eq .Status "loading"}}· Loading episodes{{else if eq .Status "unavailable"}}· Episode credits unavailable{{end}}</span>{{end}}</a>{{if .Episodes}}<details class="episode-detail"><summary>{{.EpisodeSummary}}</summary><ul>{{range .Episodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}{{if not .Watched}} <span class="credit-state later">· Later</span>{{end}}</li>{{end}}</ul></details>{{end}}</li>{{end}}</ul></details>
        <details><summary>Crew ({{len .Crew}})</summary><ul class="credits">{{range .Crew}}<li><a href="/people/{{.PersonTMDBID}}">{{.PersonName}}{{if .Job}} — {{.Job}}{{else if .Department}} — {{.Department}}{{end}} {{if ne .Status "watched"}}<span class="credit-state {{.Status}}">{{if eq .Status "later"}}· Later episodes{{else if eq .Status "unknown"}}· Episode details unavailable{{else if eq .Status "loading"}}· Loading episodes{{else if eq .Status "unavailable"}}· Episode credits unavailable{{end}}</span>{{end}}</a>{{if .Episodes}}<details class="episode-detail"><summary>{{.EpisodeSummary}}</summary><ul>{{range .Episodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}{{if not .Watched}} <span class="credit-state later">· Later</span>{{end}}</li>{{end}}</ul></details>{{end}}</li>{{end}}</ul></details>
        </section>
      {{end}}
      <form class="person-title-actions title-remove-actions" action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/remove" method="get"><button class="danger" type="submit">Remove from watched</button></form>
    {{else if eq .Kind "progress"}}
      <p>{{if eq .Title.WatchedSeason.Int64 0}}Currently watched special episode {{.Title.WatchedEpisode.Int64}}.{{else}}Currently watched through season {{.Title.WatchedSeason.Int64}}, episode {{.Title.WatchedEpisode.Int64}}.{{end}}</p>
      {{if eq .Title.EpisodeSyncStatus "failed"}}<p class="error">Episode credits could not be loaded. Your saved progress is unchanged.</p><form action="/titles/tv/{{.Title.TMDBID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>
      {{else if ne .Title.EpisodeSyncStatus "complete"}}<p class="muted">Loading episode credits before progress can be updated… This page updates automatically.</p>
      {{else if .ProgressSeasons}}<form action="/titles/tv/{{.Title.TMDBID}}/progress" method="get" class="row panel"><label>Season<select name="season">{{range .ProgressSeasons}}<option value="{{.Number}}" {{if eq .Number $.SelectedSeason}}selected{{end}}>{{if .Name}}{{.Name}}{{else}}Season {{.Number}}{{end}}</option>{{end}}</select></label><button type="submit">Show episodes</button></form>
        {{if .ProgressEpisodes}}<form action="/titles/tv/{{.Title.TMDBID}}/progress" method="post" class="row panel"><input type="hidden" name="season" value="{{.SelectedSeason}}"><label>Last episode watched<select name="episode">{{range .ProgressEpisodes}}<option value="{{.Number}}" {{if eq .Number $.SelectedEpisode}}selected{{end}}>Episode {{.Number}}{{if .Name}} · {{.Name}}{{end}}</option>{{end}}</select></label><button type="submit">Save progress</button></form>{{end}}
      {{else}}<p class="muted">No regular episodes are available to select.</p>{{end}}
      <p><a href="/titles/tv/{{.Title.TMDBID}}">Back to {{.Title.Name}}</a></p>
    {{else if eq .Kind "person"}}
      {{if .AppearancesError}}<p class="muted">{{.AppearancesError}}</p>{{end}}
      {{if .Appearances}}
        <section data-progressive-list aria-label="Appearances"><h2>Appears in:</h2><div data-progressive-items>{{template "personAppearanceRows" .Appearances}}</div>{{if .HasMoreAppearances}}<div class="load-more-results" data-progressive-load data-endpoint="/people/{{.Person.TMDBID}}/appearances/more" data-offset="{{.NextAppearanceOffset}}" data-label="titles" role="status" aria-live="polite"></div>{{end}}</section>
      {{else}}<p class="muted">No appearances found for this person.</p>{{end}}
    {{else if eq .Kind "remove-person"}}
      <p>Remove this title and its credits from your watched library?</p>
      <form class="remove-actions" action="/people/{{.Person.TMDBID}}/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/delete" method="post"><button class="danger" type="submit">Remove from watched</button><a href="/people/{{.Person.TMDBID}}">Cancel</a></form>
    {{else if eq .Kind "remove"}}
      <p>Remove this title and its credits from your watched library?</p>
      <form class="remove-actions" action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/delete" method="post"><button class="danger" type="submit">Remove from watched</button><a href="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}">Cancel</a></form>
    {{end}}
  </main>
</body>
</html>
{{define "titleRows"}}{{range .}}<li><a class="item-link" href="/titles/{{.MediaType}}/{{.TMDBID}}"><span class="item-title title-list-item">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .ReleaseYear.Valid}} · {{.ReleaseYear.Int64}}{{end}}</span></span></span></a></li>{{end}}{{end}}
{{define "peopleRows"}}{{range .}}<li><a class="item-link" href="/people/{{.TMDBID}}"><span class="item-title people-list-item"><img class="poster-thumb" src="/people/{{.TMDBID}}/photo" alt="" width="72" height="108" loading="lazy"><span class="people-list-copy"><span class="people-list-name">{{.Name}}</span>{{if .Roles}}<span class="muted people-list-match"><span class="people-list-match-label">{{.MatchLabel}}:</span> {{.Roles}}</span>{{end}}</span></span></a></li>{{end}}{{end}}
{{define "addRows"}}{{range .}}<details class="person-title add-title" data-title-entry><summary><span class="person-title-summary-content">{{with posterURL "w185" .PosterPath}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name person-title-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .Year}} · {{.Year}}{{end}}</span>{{if .InLibrary}}<span class="title-entry-state" data-title-state><span class="title-state-tag watched-tag">Watched</span></span>{{else if .InWatchlist}}<span class="title-entry-state" data-title-state><span class="title-state-tag watchlisted-tag">Watchlisted</span></span>{{else}}<span class="title-entry-state" data-title-state hidden></span>{{end}}</span></span></summary><div class="person-title-body">
  <h3 class="person-title-overview-heading">Overview</h3>
  <p class="person-title-overview">{{if .Overview}}{{.Overview}}{{else}}Overview unavailable.{{end}}</p>
  <p class="muted person-title-detail"><span class="person-title-detail-label">Release date:</span> {{with releaseDate .ReleaseDate}}{{.}}{{else}}Unavailable{{end}}</p>
  {{if not (or .InLibrary .InWatchlist)}}<form class="person-title-actions add-result-actions" action="/watchlist/{{.MediaType}}/{{.TMDBID}}" method="post" data-inline-title-action data-title-actions><input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="release_date" value="{{.ReleaseDate}}"><input type="hidden" name="poster_path" value="{{.PosterPath}}"><input type="hidden" name="overview" value="{{.Overview}}">{{if eq .MediaType "movie"}}<button type="submit" formaction="/add/movie/{{.TMDBID}}" data-title-action="watched">Add to watched</button>{{else}}<a class="button" href="/add/tv/{{.TMDBID}}">Add to watched</a>{{end}}<button class="watchlist-button" type="submit" data-title-action="watchlisted">Add to watchlist</button></form><span class="muted title-action-status" data-title-action-status role="status" aria-live="polite"></span>{{end}}
</div></details>{{end}}{{end}}
{{define "watchlistRows"}}{{range .Watchlist}}<details class="person-title watchlist-title"><summary><span class="person-title-summary-content">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name person-title-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .ReleaseYear.Valid}} · {{.ReleaseYear.Int64}}{{end}}</span></span></span></summary><div class="person-title-body">
  <h3 class="person-title-overview-heading">Overview</h3>
  <p class="person-title-overview">{{if .Overview}}{{.Overview}}{{else}}Overview unavailable.{{end}}</p>
  <p class="muted person-title-detail"><span class="person-title-detail-label">Release date:</span> {{if .ReleaseDate.Valid}}{{with releaseDate .ReleaseDate.String}}{{.}}{{else}}Unavailable{{end}}{{else if .ReleaseYear.Valid}}{{.ReleaseYear.Int64}}{{else}}Unavailable{{end}}</p>
  <div class="person-title-actions"><a class="button" href="/add/{{.MediaType}}/{{.TMDBID}}">Add to watched</a></div>
  <form class="person-title-actions" action="/watchlist/{{.MediaType}}/{{.TMDBID}}/delete{{if $.TitleMedia}}?media={{$.TitleMedia}}{{end}}" method="post"><button class="danger" type="submit">Remove from watchlist</button></form>
</div></details>{{end}}{{end}}
{{define "personAppearanceRows"}}{{range .}}<details class="person-title" data-title-entry><summary><span class="person-title-summary-content">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name person-title-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{with releaseYear .ReleaseSort}} · {{.}}{{end}}</span>{{if .InLibrary}}<span class="title-entry-state" data-title-state><span class="title-state-tag watched-tag">Watched</span></span>{{else if .InWatchlist}}<span class="title-entry-state" data-title-state><span class="title-state-tag watchlisted-tag">Watchlisted</span></span>{{else}}<span class="title-entry-state" data-title-state hidden></span>{{end}}</span></span></summary><div class="person-title-body">
  <h3 class="person-title-overview-heading">Overview</h3>
  <p class="person-title-overview">{{if .Overview}}{{.Overview}}{{else}}Overview unavailable.{{end}}</p>
  <p class="muted person-title-detail"><span class="person-title-detail-label">Role:</span> {{if .Roles}}{{.Roles}}{{else}}Unavailable{{end}}</p>
  {{with releaseDate .ReleaseSort}}<p class="muted person-title-detail"><span class="person-title-detail-label">Release date:</span> {{.}}</p>{{end}}
  {{if .InLibrary}}{{if eq .MediaType "tv"}}{{if eq .SyncStatus "complete"}}{{if .Seasons}}<p class="muted">Credited episodes in your watched range</p>{{range .Seasons}}<details class="person-season"><summary><span>{{.Name}}</span><span class="person-season-count">{{len .Episodes}} eps</span></summary><ul>{{range .Episodes}}<li>Episode {{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}{{else if eq .WatchedStatus "later"}}<p class="muted">No credited episodes in your watched range.</p>{{else}}<p class="muted">Episode details unavailable for this person.</p>{{end}}
  {{else if eq .SyncStatus "failed"}}<p class="error">Episode credits could not be loaded.</p><form action="/people/{{.PersonID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>
  {{else}}<p class="muted">Loading episode credits… This page updates automatically.</p>{{end}}{{end}}{{end}}
  {{if .InLibrary}}<div class="person-title-actions"><form action="/people/{{.PersonID}}/titles/{{.MediaType}}/{{.TMDBID}}/remove" method="get"><button class="danger" type="submit">Remove from watched</button></form></div>{{else if .InWatchlist}}<form class="person-title-actions" action="/watchlist/{{.MediaType}}/{{.TMDBID}}/delete" method="post"><input type="hidden" name="person_id" value="{{.PersonID}}"><button class="danger" type="submit">Remove from watchlist</button></form>{{else}}<div class="title-action-region" data-title-action-region><div class="title-action-stack"><div class="person-title-actions" data-title-actions>{{if eq .MediaType "movie"}}<form action="/add/movie/{{.TMDBID}}" method="post" data-inline-title-action><button type="submit" data-title-action="watched">Add to watched</button></form>{{else}}<a class="button" href="/add/tv/{{.TMDBID}}">Add to watched</a>{{end}}<form action="/watchlist/{{.MediaType}}/{{.TMDBID}}" method="post" data-inline-title-action><input type="hidden" name="person_id" value="{{.PersonID}}"><input type="hidden" name="name" value="{{.Name}}"><input type="hidden" name="release_date" value="{{.ReleaseSort}}"><input type="hidden" name="poster_path" value="{{.PosterPath.String}}"><input type="hidden" name="overview" value="{{.Overview}}"><button class="watchlist-button" type="submit" data-title-action="watchlisted">Add to watchlist</button></form></div><div class="person-title-actions" data-title-remove="watched" hidden><form action="/people/{{.PersonID}}/titles/{{.MediaType}}/{{.TMDBID}}/remove" method="get"><button class="danger" type="submit">Remove from watched</button></form></div><form class="person-title-actions" action="/watchlist/{{.MediaType}}/{{.TMDBID}}/delete" method="post" data-title-remove="watchlisted" hidden><input type="hidden" name="person_id" value="{{.PersonID}}"><button class="danger" type="submit">Remove from watchlist</button></form></div><span class="muted title-action-status" data-title-action-status role="status" aria-live="polite"></span></div>{{end}}
</div></details>{{end}}{{end}}`
