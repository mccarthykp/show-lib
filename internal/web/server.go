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
	store    *store.Store
	tmdb     *tmdb.Client
	importer *importer.Service
	page     *template.Template
}

//go:embed live_search.js
var liveSearchJS []byte

//go:embed favicon.svg
var faviconSVG []byte

type pageData struct {
	Heading          string
	Kind             string
	Titles           []store.Title
	Pending          []store.Title
	HasMoreTitles    bool
	NextTitleOffset  int
	People           []store.Person
	HasMorePeople    bool
	NextPeopleOffset int
	Title            store.Title
	Person           store.Person
	Cast             []store.CreditView
	Crew             []store.CreditView
	Connections      []connectionGroup
	ConnectionCount  int
	PersonTitles     []store.PersonTitle
	ProgressSeasons  []store.ProgressSeason
	ProgressEpisodes []store.ProgressEpisode
	SelectedEpisode  int
	Query            string
	SearchPage       int
	PreviousPage     int
	HasNext          bool
	NextPage         int
	Results          []searchHit
	Error            string
	PreviewMedia     string
	PreviewID        int64
	PreviewYear      string
	PreviewPoster    string
	Seasons          []tmdb.Season
	SelectedSeason   int
	Episodes         []tmdb.Episode
	AlreadyAdded     bool
}

func New(db *store.Store, tmdbClient *tmdb.Client) http.Handler {
	s := &Server{store: db, tmdb: tmdbClient, importer: &importer.Service{TMDB: tmdbClient, Store: db},
		page: template.Must(template.New("page").Funcs(template.FuncMap{"posterURL": posterURL}).Parse(pageHTML))}
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
	mux.HandleFunc("GET /titles", s.titles)
	mux.HandleFunc("GET /titles/more", s.moreTitles)
	mux.HandleFunc("GET /titles/{media}/{id}", s.title)
	mux.HandleFunc("GET /titles/tv/{id}/progress", s.progress)
	mux.HandleFunc("POST /titles/tv/{id}/progress", s.postProgress)
	mux.HandleFunc("POST /titles/tv/{id}/retry-episodes", s.retryTitleEpisodes)
	mux.HandleFunc("GET /titles/{media}/{id}/remove", s.removeConfirm)
	mux.HandleFunc("POST /titles/{media}/{id}/delete", s.remove)
	mux.HandleFunc("POST /titles/{media}/{id}/retry", s.retry)
	mux.HandleFunc("GET /people", s.people)
	mux.HandleFunc("GET /people/more", s.morePeople)
	mux.HandleFunc("GET /people/{id}/photo", s.personPhoto)
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
	titles, hasMore, err := s.titleBatch(r.Context(), query, 0)
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
		Query: query, HasMoreTitles: hasMore, NextTitleOffset: len(titles)})
}

const resultBatchSize = 12

func (s *Server) titleBatch(ctx context.Context, query string, offset int) ([]store.Title, bool, error) {
	var titles []store.Title
	var err error
	if query == "" {
		titles, err = s.store.ListTitlesPage(ctx, resultBatchSize+1, offset)
	} else {
		all, searchErr := s.store.SearchTitles(ctx, query)
		if searchErr != nil {
			return nil, false, searchErr
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

func (s *Server) moreTitles(w http.ResponseWriter, r *http.Request) {
	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		http.Error(w, "invalid title offset", http.StatusBadRequest)
		return
	}
	titles, hasMore, err := s.titleBatch(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")), offset)
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
	s.render(w, pageData{Heading: person.Name, Kind: "person", Person: person, PersonTitles: titles})
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
  <meta name="theme-color" content="#101211">
  <link rel="icon" type="image/svg+xml" sizes="any" href="/favicon.svg?v=3">
  <link rel="shortcut icon" href="/favicon.svg?v=3">
  <title>{{.Heading}} · Show-lib</title>
  <script defer src="/assets/live-search.js"></script>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #f2f0e9; background: #101211; }
    * { box-sizing: border-box; }
    body { margin: 0; padding: 1.25rem 0 3rem; min-height: 100vh; background: radial-gradient(circle at 50% -14rem, #263a37 0, #181e1c 37rem, #101211 68rem); line-height: 1.55; -webkit-user-select: none; user-select: none; }
    a { color: #6cdece; text-decoration: none; } a:hover { color: #a4f5e9; }
    a:focus-visible, button:focus-visible, input:focus-visible, select:focus-visible, summary:focus-visible { outline: 2px solid #6cdece; outline-offset: 3px; }
    .site-header { width: min(760px, calc(100% - 2rem)); min-height: 3.5rem; margin: 0 auto; padding: .35rem .4rem; border: 1px solid #343c38; border-radius: 999px; background: #131816; box-shadow: 0 16px 40px #0005; }
    .brand { display: inline-flex; align-items: center; justify-content: center; justify-self: start; width: 2.3rem; height: 2.3rem; margin-left: .35rem; border-radius: 50%; color: #a7e8dc; }
    .brand svg { display: block; width: 1.85rem; height: 1.85rem; }
    .brand:hover { color: #c5f5ed; }
    button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; border: 1px solid #69d7c7; border-radius: .55rem; background: #69d7c7; color: #102a26; font: inherit; font-weight: 750; cursor: pointer; padding: .65rem 1rem; }
    button:hover { background: #99ecdf; border-color: #99ecdf; color: #102a26; }
    .primary-nav { width: 100%; display: grid; grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr); align-items: center; gap: .55rem; }
    .nav-links { display: flex; align-items: center; justify-self: center; gap: .1rem; padding: .25rem; border-radius: 999px; background: #262e2a; }
    .nav-links a { display: block; min-width: 4.7rem; padding: .42rem .85rem; border-radius: 999px; color: #d1d8d2; text-align: center; font-size: .88rem; font-weight: 550; }
    .nav-links a:hover, .nav-links a[aria-current="page"] { background: #414b45; color: #f7fbf8; }
    .nav-add { display: block; justify-self: end; padding: .56rem 1.15rem; border-radius: 999px; background: #a7e8dc; color: #102a26; font-size: .88rem; font-weight: 720; text-align: center; }
    .nav-add:hover, .nav-add[aria-current="page"] { background: #c5f5ed; color: #102a26; }
    .nav-add[aria-current="page"] { box-shadow: inset 0 0 0 2px #69d7c7; }
    main { width: min(1080px, calc(100% - 2rem)); margin: 4.5rem auto 5rem; }
    .eyebrow { margin: 0 0 .4rem; color: #7adfce; font-size: .72rem; font-weight: 800; letter-spacing: .2em; text-transform: uppercase; }
    .title-eyebrow { margin-bottom: 0; }
    h1 { margin: 0 0 2rem; font-size: clamp(2.4rem, 6vw, 4.7rem); line-height: 1.03; font-weight: 690; letter-spacing: -.065em; }
    h2 { margin: 2.4rem 0 1rem; font-size: 1.15rem; letter-spacing: -.025em; }
    p { max-width: 72ch; }
    ul { padding-left: 1.2rem; } li { margin: .55rem 0; }
    .muted { color: #a6aaa3; } .error { color: #ffaba2; }
    .card, .panel { background: #222522; border: 1px solid #3c403b; border-radius: .85rem; padding: 1.2rem; margin: 1rem 0; }
    .item-list { list-style: none; padding: 0; display: grid; gap: .55rem; }
    .item-list li { margin: 0; padding: 0; background: #222522; border: 1px solid #383c37; border-radius: .65rem; }
    .item-list li:hover { border-color: #58988b; background: #293833; }
    .item-list a { color: #f4f0e7; font-weight: 620; }
    .item-list a:hover { color: #f4f0e7; }
    .item-list li { cursor: pointer; }
    .load-more-results { min-height: 1.5rem; margin: .5rem 0; color: #a6aaa3; text-align: center; }
    .item-link { display: flex; align-items: center; justify-content: space-between; gap: 1rem; width: 100%; padding: .8rem 1rem; border-radius: .65rem; cursor: pointer; }
    .item-title { display: inline-flex; align-items: center; gap: .85rem; min-width: 0; }
    .title-list-copy { display: flex; flex-direction: column; justify-content: center; align-self: stretch; gap: .2rem; min-width: 0; }
    .title-list-name { line-height: 1.25; }
    .title-list-meta { font-size: .85rem; line-height: 1.2; font-weight: 450; }
    .poster-thumb { display: block; flex: 0 0 auto; width: 3rem; height: 4.5rem; border: 1px solid #46544d; border-radius: .35rem; object-fit: cover; background: #1a211e; }
    .title-list-item .poster-thumb, .person-title > summary .poster-thumb { width: 4.5rem; height: 6.75rem; }
    .poster-placeholder { background: linear-gradient(150deg, #30413c, #19221e); }
    .title-poster { display: block; width: 7.5rem; height: 11.25rem; margin: 0 0 1.3rem; border: 1px solid #46544d; border-radius: .6rem; object-fit: cover; background: #1a211e; }
    .title-hero { display: flex; align-items: flex-start; gap: clamp(1.25rem, 4vw, 2.5rem); padding-top: 1.5rem; margin-bottom: 2rem; }
    .title-hero .title-poster { flex: 0 0 auto; width: 13rem; height: 19.5rem; margin: 0; }
    .title-hero-copy, .title-identification { display: flex; flex-direction: column; gap: .85rem; min-width: 0; }
    .title-hero-copy { flex: 1; }
    .title-hero h1 { margin: 0; padding: 0; font-size: clamp(1.6rem, 4vw, 3.15rem); line-height: 1; }
    .title-hero .title-meta { margin: 0; font-size: 1rem; }
    .title-overview h2 { margin: 0 0 .3rem; color: #7adfce; font-size: .85rem; letter-spacing: .12em; text-transform: uppercase; }
    .title-overview p { margin: 0; font-size: .92rem; line-height: 1.55; }
    .item-link .muted { flex: 0 0 auto; font-weight: 450; }
    .add-result-link .item-title { flex: 1 1 auto; }
    .result-check-slot { display: flex; align-items: center; justify-content: center; flex: 0 0 1.5rem; margin-left: auto; }
    .added-check { display: inline-flex; align-items: center; justify-content: center; width: 1.5rem; height: 1.5rem; border-radius: 50%; background: #69d7c7; color: #102a26; font-size: 1rem; font-weight: 800; line-height: 1; }
    .row { display: flex; gap: .75rem; flex-wrap: wrap; align-items: end; }
    .search-status { margin: -.45rem 0 1rem; }
    .search-status:empty { display: none; }
    label { display: grid; gap: .4rem; color: #d7d9d2; font-size: .83rem; font-weight: 680; }
    input, select { min-height: 2.75rem; border: 1px solid #515e57; border-radius: .5rem; background-color: #1e2723; color: #f2f0e9; font: inherit; padding: .55rem .7rem; }
    input { -webkit-user-select: text; user-select: text; }
    select { appearance: none; max-width: 100%; padding: .55rem 2.6rem .55rem .85rem; background-image: url('data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16" fill="none"%3E%3Cpath d="m4 6 4 4 4-4" stroke="%2369d7c7" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"/%3E%3C/svg%3E'); background-repeat: no-repeat; background-position: right .8rem center; background-size: 1rem; }
    input[type=text] { min-width: min(24rem, 80vw); }
    .danger { background: #a94540; border-color: #a94540; color: white; }
    .danger:hover { background: #c0554e; border-color: #c0554e; color: white; }
    details { margin: 1rem 0; border: 1px solid #3c403b; border-radius: .7rem; background: #222522; }
    details:hover { border-color: #58988b; background: #293833; }
    summary { padding: 1rem 1.2rem; cursor: pointer; font-weight: 700; }
    .credits { max-height: 36rem; overflow: auto; list-style: none; padding: 0 1rem 1rem; margin: 0; }
    .credits li { margin: .35rem 0; }
    .credits a { display: block; padding: .65rem .8rem; border: 1px solid #3c403b; border-radius: .5rem; background: #1c231f; color: #f4f0e7; cursor: pointer; }
    .credits a:hover { border-color: #69d7c7; background: #30413c; color: #b7f8ed; }
    .credit-state { color: #8edcca; font-size: .78rem; font-weight: 550; }
    .credit-state.later { color: #dfc68a; }
    .credit-state.unknown { color: #a6aaa3; }
    .credits .episode-detail { margin: .2rem .5rem .7rem; background: #1a211e; }
    .credits .episode-detail summary { padding: .4rem .7rem; color: #a6aaa3; font-size: .8rem; font-weight: 500; }
    .credits .episode-detail ul { max-height: 16rem; overflow: auto; padding: 0 .7rem .6rem 1.8rem; }
    .credits .episode-detail li { margin: .15rem 0; }
    .person-title > summary, .person-season > summary { display: flex; align-items: baseline; gap: .55rem 1rem; list-style: none; }
    .person-appearances-heading { margin: 0 0 1rem; }
    .person-appearances-heading + .person-title { margin-top: 0; }
    .person-title > summary { align-items: center; }
    .person-title > summary::-webkit-details-marker, .person-season > summary::-webkit-details-marker { display: none; }
    .person-title > summary::before, .person-season > summary::before { content: '▸'; color: #69d7c7; flex: 0 0 auto; }
    .person-title[open] > summary::before, .person-season[open] > summary::before { content: '▾'; }
    .person-title-name { color: #f4f0e7; }
    .person-title-role { color: #a6aaa3; font-weight: 450; }
    .person-title-period { margin-left: auto; flex: 0 0 auto; color: #a6aaa3; font-weight: 500; text-align: right; }
    .person-title-body { padding: 0 1.2rem 1.2rem 2.65rem; }
    .person-season { margin: .6rem 0; background: #1c231f; }
    .person-season > summary { padding: .75rem 1rem; color: #f4f0e7; }
    .person-season-count { margin-left: auto; color: #a6aaa3; font-size: .85rem; font-weight: 500; }
    .person-season ul { margin: 0; padding: 0 1rem .9rem 2.6rem; }
    .person-season li { margin: .2rem 0; color: #d8dbd4; }
    .person-title-actions { margin: 1rem 0 0; }
    .remove-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 1rem; }
    .connection-panel { margin: 0 0 1.7rem; border: 1px solid #407267; border-radius: .85rem; background: #1e302b; }
    .connection-disclosure { margin: 0; border: 0; background: transparent; }
    .connection-disclosure > summary { display: flex; align-items: center; gap: .7rem; padding: 1.2rem; list-style: none; color: #c7f2e8; }
    .connection-disclosure > summary::-webkit-details-marker { display: none; }
    .connection-disclosure > summary::before { content: '▸'; color: #69d7c7; }
    .connection-disclosure[open] > summary::before { content: '▾'; }
    .connection-content { padding: 0 1.2rem 1.2rem; }
    .no-connections { margin: 0 0 1.7rem; font-weight: 700; }
    .connection-line { max-width: none; margin: 0; font-weight: 630; }
    .connection-group + .connection-group { border-top: 1px solid #37554d; margin-top: 1rem; padding-top: 1rem; }
    .connection-info { margin: .6rem 0 0; border-color: #36554b; background: #1a2924; }
    .connection-info > summary { padding: .6rem .8rem; font-size: .86rem; font-weight: 600; color: #b9dfd4; }
    .connection-roles { margin: .55rem 0 0; padding-left: 1.25rem; color: #d6ddd7; font-size: .9rem; }
    .connection-roles li { margin: .4rem 0; }
    .connection-evidence { margin: .3rem 0 .65rem; background: #192722; }
    .connection-evidence > summary { padding: .4rem .7rem; font-weight: 500; font-size: .82rem; }
    .connection-evidence ul { max-height: 14rem; overflow: auto; margin: 0; padding: 0 .7rem .6rem 2rem; }
    @media (max-width: 640px) { body { padding-top: .6rem; } main { margin-top: 3rem; } .item-link { align-items: flex-start; flex-direction: column; gap: .15rem; } .add-result-link { align-items: center; flex-direction: row; gap: .5rem; } }
    @media (max-width: 640px) { .title-hero { display: grid; grid-template-columns: 9rem minmax(0, 1fr); gap: 1rem; align-items: start; } .title-hero .title-poster { grid-column: 1; grid-row: 1; width: 9rem; height: 13.5rem; } .title-hero-copy { display: contents; } .title-identification { grid-column: 2; grid-row: 1; } .title-overview { grid-column: 1 / -1; grid-row: 2; } }
    @media (max-width: 480px) { .site-header { width: calc(100% - 1rem); padding: .65rem; border-radius: 1.3rem; } .primary-nav { grid-template-columns: 1fr auto; row-gap: .5rem; } .brand { grid-column: 1; grid-row: 1; margin-left: .35rem; } .nav-add { grid-column: 2; grid-row: 1; padding: .55rem .95rem; } .nav-links { grid-column: 1 / -1; grid-row: 2; width: min(100%, 17rem); } .nav-links a { flex: 1; min-width: 0; padding: .48rem .6rem; } }
    @media (max-width: 640px) { .person-title > summary { flex-wrap: wrap; } .person-title-period { width: 100%; margin-left: auto; text-align: right; } }
  </style>
</head>
<body>
  <header class="site-header">
    <nav class="primary-nav" aria-label="Primary navigation">
      <a class="brand" href="/titles" aria-label="Show-lib home"><svg viewBox="0 0 36 36" aria-hidden="true" focusable="false"><defs><mask id="nav-reel-cutouts"><rect width="36" height="36" fill="white"/><g fill="black"><circle cx="18" cy="18" r="2"/><circle cx="18" cy="8.5" r="3"/><circle cx="27" cy="15" r="3"/><circle cx="23.5" cy="26" r="3"/><circle cx="12.5" cy="26" r="3"/><circle cx="9" cy="15" r="3"/></g></mask></defs><circle cx="18" cy="18" r="15" fill="currentColor" mask="url(#nav-reel-cutouts)"/></svg></a>
      <div class="nav-links">
        <a href="/titles" {{if or (eq .Kind "titles") (eq .Kind "title") (eq .Kind "progress") (eq .Kind "remove")}}aria-current="page"{{end}}>Titles</a>
        <a href="/people" {{if or (eq .Kind "people") (eq .Kind "person") (eq .Kind "remove-person")}}aria-current="page"{{end}}>People</a>
      </div>
      <a class="nav-add" href="/add" {{if or (eq .Kind "add") (eq .Kind "preview")}}aria-current="page"{{end}}>Add</a>
    </nav>
  </header>
  <main>
    <p class="eyebrow{{if or (eq .Kind "title") (eq .Kind "person")}} title-eyebrow{{end}}">Personal film &amp; television library</p>
    {{if eq .Kind "title"}}
      <div class="title-hero">
        {{with posterURL "w342" .Title.PosterPath.String}}<img class="title-poster" src="{{.}}" alt="" width="208" height="312">{{else}}<span class="title-poster poster-placeholder" aria-hidden="true"></span>{{end}}
        <div class="title-hero-copy"><div class="title-identification"><h1>{{.Heading}}</h1><p class="muted title-meta">{{.Title.MediaType}}{{if .Title.ReleaseYear.Valid}} · {{.Title.ReleaseYear.Int64}}{{end}}</p></div><section class="title-overview" aria-label="Overview"><h2>Overview</h2><p>{{if .Title.Overview.String}}{{.Title.Overview.String}}{{else}}Overview unavailable.{{end}}</p></section></div>
      </div>
    {{else if eq .Kind "person"}}
      <div class="title-hero">
        <img class="title-poster" src="/people/{{.Person.TMDBID}}/photo?size=w342" alt="" width="208" height="312">
        <div class="title-hero-copy"><div class="title-identification"><h1>{{.Heading}}</h1></div></div>
      </div>
    {{else}}
      {{if eq .Kind "preview"}}{{with posterURL "w342" .PreviewPoster}}<img class="title-poster" src="{{.}}" alt="" width="120" height="180">{{else}}<span class="title-poster poster-placeholder" aria-hidden="true"></span>{{end}}{{end}}
      <h1>{{.Heading}}</h1>
    {{end}}
    {{if .Error}}<p class="error">{{.Error}}</p>{{end}}
    {{if eq .Kind "titles"}}
      <form action="/titles" method="get" class="row panel" data-live-search="titles"><label>Search titles or people in your library<input name="q" type="text" value="{{.Query}}" placeholder="Title or person" autocomplete="off" required></label><button type="submit">Search</button>{{if .Query}}<a href="/titles">Clear</a>{{end}}</form>
      <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
      <section id="live-results" aria-label="Title search results">
      {{if .Titles}}<ul class="item-list">{{template "titleRows" .Titles}}</ul>{{if .HasMoreTitles}}<div class="load-more-results" data-load-more-results data-endpoint="/titles/more" data-offset="{{.NextTitleOffset}}" data-query="{{.Query}}" role="status" aria-live="polite"></div>{{end}}
      {{else if .Query}}<p class="muted">No matching titles in your watched library.</p>
      {{else}}<p class="muted">Your watched library is empty. <a href="/add">Add a title</a>.</p>{{end}}
      </section>
      {{if .Pending}}<h2>Imports</h2><ul class="item-list">{{range .Pending}}<li><a class="item-link" href="/titles/{{.MediaType}}/{{.TMDBID}}"><span class="item-title title-list-item">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .ReleaseYear.Valid}} · {{.ReleaseYear.Int64}}{{end}} · {{.ImportStatus}}</span></span></span></a></li>{{end}}</ul>{{end}}
    {{else if eq .Kind "people"}}
      <form action="/people" method="get" class="row panel" data-live-search="people">
        <label>Search people or roles<input name="q" type="text" value="{{.Query}}" placeholder="Person, character, or crew job" autocomplete="off" required></label>
        <button type="submit">Search</button>
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
        <button type="submit">Search</button>
      </form>
      <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
      <section id="live-results" aria-label="TMDB search results">
      {{if .Query}}
        {{if .Results}}<h2>Results</h2><ul class="item-list">{{range .Results}}<li><a class="item-link add-result-link" href="/add/{{.MediaType}}/{{.TMDBID}}"><span class="item-title title-list-item">{{with posterURL "w185" .PosterPath}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .Year}} · {{.Year}}{{end}}</span></span></span><span class="result-check-slot">{{if .InLibrary}}<span class="added-check" role="img" aria-label="Already in library">✓</span>{{end}}</span></a></li>{{end}}</ul>
        {{else if not .Error}}<p>No matching titles found.</p>{{end}}
        <p>{{if .PreviousPage}}<a href="/add?q={{.Query | urlquery}}&amp;page={{.PreviousPage}}">Previous page</a>{{end}} {{if .HasNext}}<a href="/add?q={{.Query | urlquery}}&amp;page={{.NextPage}}">Next page</a>{{end}}</p>
      {{end}}
      </section>
    {{else if eq .Kind "preview"}}
      <p class="muted">{{.PreviewMedia}}{{if .PreviewYear}} · {{.PreviewYear}}{{end}}</p>
      {{if .AlreadyAdded}}<p>This title is already in your library. <a href="/titles/{{.PreviewMedia}}/{{.PreviewID}}">Open it</a>.</p>
      {{else if eq .PreviewMedia "movie"}}<form action="/add/movie/{{.PreviewID}}" method="post"><button type="submit">Add to watched</button></form>
      {{else}}
        {{if .Seasons}}<form action="/add/tv/{{.PreviewID}}" method="get" class="row"><label>Season<select name="season">{{range .Seasons}}{{if gt .EpisodeCount 0}}<option value="{{.SeasonNumber}}" {{if eq .SeasonNumber $.SelectedSeason}}selected{{end}}>{{.Name}} ({{.EpisodeCount}} episodes)</option>{{end}}{{end}}</select></label><button type="submit">Show episodes</button></form>{{end}}
        {{if .Episodes}}<form action="/add/tv/{{.PreviewID}}" method="post" class="row"><input type="hidden" name="season" value="{{.SelectedSeason}}"><label>Last episode watched<select name="episode">{{range .Episodes}}<option value="{{.EpisodeNumber}}">Episode {{.EpisodeNumber}}{{if .Name}} — {{.Name}}{{end}}</option>{{end}}</select></label><button type="submit">Add to watched</button></form>
        {{else}}<p class="muted">No episodes are available for this season.</p>{{end}}
      {{end}}
    {{else if eq .Kind "title"}}
      {{if eq .Title.ImportStatus "complete"}}{{if .Connections}}<section class="connection-panel" aria-label="Connections"><details class="connection-disclosure"><summary>{{.ConnectionCount}} {{if eq .ConnectionCount 1}}connection{{else}}connections{{end}}</summary><div class="connection-content">{{range .Connections}}<div class="connection-group"><p class="connection-line">{{range $i, $person := .People}}{{if $i}}, {{end}}<a href="/people/{{$person.ID}}">{{$person.Name}}</a>{{end}} {{if gt (len .People) 1}}also appear{{else}}also appears{{end}} in {{range $i, $title := .Titles}}{{if $i}}, {{end}}<a href="/titles/{{$title.MediaType}}/{{$title.ID}}">{{$title.Name}}</a>{{end}}.</p>
          <details class="connection-info"><summary>View roles and watched episodes</summary><ul class="connection-roles">{{range .Details}}<li><a href="/people/{{.Person.ID}}">{{.Person.Name}}</a>: {{.HereRoles}} in {{$.Title.Name}}; {{.OtherRoles}} in <a href="/titles/{{.Other.MediaType}}/{{.Other.ID}}">{{.Other.Name}}</a>
            {{if .HereEpisodes}}<details class="connection-evidence"><summary>{{len .HereEpisodes}} watched credited episodes in {{$.Title.Name}}</summary><ul>{{range .HereEpisodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}
            {{if .OtherEpisodes}}<details class="connection-evidence"><summary>{{len .OtherEpisodes}} watched credited episodes in {{.Other.Name}}</summary><ul>{{range .OtherEpisodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}
          </li>{{end}}</ul></details></div>{{end}}</div></details></section>
        {{else if and (eq .Title.MediaType "tv") (ne .Title.EpisodeSyncStatus "complete")}}<p class="muted">Connections will appear after episode credits finish loading.</p>
        {{else}}<p class="no-connections">No connections</p>{{end}}{{end}}
      {{if .Title.WatchedSeason.Valid}}<p>{{if eq .Title.WatchedSeason.Int64 0}}Watched special episode {{.Title.WatchedEpisode.Int64}}{{else}}Watched through season {{.Title.WatchedSeason.Int64}}, episode {{.Title.WatchedEpisode.Int64}}{{end}} · <a href="/titles/tv/{{.Title.TMDBID}}/progress">Update progress</a></p>{{end}}
      {{if eq .Title.ImportStatus "importing"}}<p>Importing cast and crew… This page updates automatically.</p>
      {{else if eq .Title.ImportStatus "failed"}}<p class="error">Import failed{{if .Title.ImportError.Valid}}: {{.Title.ImportError.String}}{{end}}</p><form action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/retry" method="post"><button type="submit">Retry import</button></form>
      {{else}}
        {{if eq .Title.MediaType "tv"}}{{if eq .Title.EpisodeSyncStatus "failed"}}<p class="error">Episode credits could not be loaded{{if .Title.EpisodeSyncError.Valid}}: {{.Title.EpisodeSyncError.String}}{{end}}</p><form action="/titles/tv/{{.Title.TMDBID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>{{else if ne .Title.EpisodeSyncStatus "complete"}}<p class="muted">Loading episode credits… This page updates automatically.</p>{{end}}{{end}}
        <form action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}" method="get" class="row panel" data-live-search="credits"><label>Search cast and crew<input name="q" type="text" value="{{.Query}}" placeholder="Person, character, or job" autocomplete="off" required></label><button type="submit">Search</button>{{if .Query}}<a href="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}">Clear</a>{{end}}</form>
        <p id="search-status" class="muted search-status" role="status" aria-live="polite"></p>
        <section id="live-results" aria-label="Cast and crew search results">
        {{if and .Query (not (or .Cast .Crew))}}<p class="muted">No matching cast or crew for this title.</p>{{end}}
        <details><summary>Cast ({{len .Cast}})</summary><ul class="credits">{{range .Cast}}<li><a href="/people/{{.PersonTMDBID}}">{{.PersonName}}{{if .CharacterName}} — {{.CharacterName}}{{end}} <span class="credit-state {{.Status}}">{{if eq .Status "watched"}}· Watched credit{{else if eq .Status "later"}}· Later episodes{{else if eq .Status "unknown"}}· Episode details unavailable{{else if eq .Status "loading"}}· Loading episodes{{else if eq .Status "unavailable"}}· Episode credits unavailable{{end}}</span></a>{{if .Episodes}}<details class="episode-detail"><summary>{{.EpisodeSummary}}</summary><ul>{{range .Episodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}{{if not .Watched}} <span class="credit-state later">· Later</span>{{end}}</li>{{end}}</ul></details>{{end}}</li>{{end}}</ul></details>
        <details><summary>Crew ({{len .Crew}})</summary><ul class="credits">{{range .Crew}}<li><a href="/people/{{.PersonTMDBID}}">{{.PersonName}}{{if .Job}} — {{.Job}}{{else if .Department}} — {{.Department}}{{end}} <span class="credit-state {{.Status}}">{{if eq .Status "watched"}}· Watched credit{{else if eq .Status "later"}}· Later episodes{{else if eq .Status "unknown"}}· Episode details unavailable{{else if eq .Status "loading"}}· Loading episodes{{else if eq .Status "unavailable"}}· Episode credits unavailable{{end}}</span></a>{{if .Episodes}}<details class="episode-detail"><summary>{{.EpisodeSummary}}</summary><ul>{{range .Episodes}}<li>S{{.Season}} E{{.Number}}{{if .Name}} · {{.Name}}{{end}}{{if not .Watched}} <span class="credit-state later">· Later</span>{{end}}</li>{{end}}</ul></details>{{end}}</li>{{end}}</ul></details>
        </section>
      {{end}}
      <p><a href="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/remove">Remove title</a></p>
    {{else if eq .Kind "progress"}}
      <p>{{if eq .Title.WatchedSeason.Int64 0}}Currently watched special episode {{.Title.WatchedEpisode.Int64}}.{{else}}Currently watched through season {{.Title.WatchedSeason.Int64}}, episode {{.Title.WatchedEpisode.Int64}}.{{end}}</p>
      {{if eq .Title.EpisodeSyncStatus "failed"}}<p class="error">Episode credits could not be loaded. Your saved progress is unchanged.</p><form action="/titles/tv/{{.Title.TMDBID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>
      {{else if ne .Title.EpisodeSyncStatus "complete"}}<p class="muted">Loading episode credits before progress can be updated… This page updates automatically.</p>
      {{else if .ProgressSeasons}}<form action="/titles/tv/{{.Title.TMDBID}}/progress" method="get" class="row panel"><label>Season<select name="season">{{range .ProgressSeasons}}<option value="{{.Number}}" {{if eq .Number $.SelectedSeason}}selected{{end}}>{{if .Name}}{{.Name}}{{else}}Season {{.Number}}{{end}}</option>{{end}}</select></label><button type="submit">Show episodes</button></form>
        {{if .ProgressEpisodes}}<form action="/titles/tv/{{.Title.TMDBID}}/progress" method="post" class="row panel"><input type="hidden" name="season" value="{{.SelectedSeason}}"><label>Last episode watched<select name="episode">{{range .ProgressEpisodes}}<option value="{{.Number}}" {{if eq .Number $.SelectedEpisode}}selected{{end}}>Episode {{.Number}}{{if .Name}} · {{.Name}}{{end}}</option>{{end}}</select></label><button type="submit">Save progress</button></form>{{end}}
      {{else}}<p class="muted">No regular episodes are available to select.</p>{{end}}
      <p><a href="/titles/tv/{{.Title.TMDBID}}">Back to {{.Title.Name}}</a></p>
    {{else if eq .Kind "person"}}
      {{if .PersonTitles}}<h2 class="person-appearances-heading">Appears in</h2>{{range .PersonTitles}}<details class="person-title"><summary>{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="person-title-name">{{.Name}}</span>{{if .Roles}}<span class="person-title-role">{{.Roles}}</span>{{end}}{{if eq .WatchedStatus "later"}}<span class="credit-state later">Later episodes</span>{{else if eq .WatchedStatus "unknown"}}<span class="credit-state unknown">Episode details unavailable</span>{{else if eq .WatchedStatus "loading"}}<span class="credit-state unknown">Loading credits</span>{{else if eq .WatchedStatus "unavailable"}}<span class="credit-state unknown">Credits unavailable</span>{{end}}{{if .Period}}<span class="person-title-period">{{.Period}}</span>{{end}}</summary><div class="person-title-body">
        {{if eq .MediaType "movie"}}<p class="muted">Movie{{if .Period}} · Released {{.Period}}{{end}}</p>
        {{else if eq .SyncStatus "complete"}}{{if .Seasons}}<p class="muted">Credited episodes in your watched range</p>{{range .Seasons}}<details class="person-season"><summary><span>{{.Name}}</span><span class="person-season-count">{{len .Episodes}} eps</span></summary><ul>{{range .Episodes}}<li>Episode {{.Number}}{{if .Name}} · {{.Name}}{{end}}</li>{{end}}</ul></details>{{end}}{{else if eq .WatchedStatus "later"}}<p class="muted">No credited episodes in your watched range.</p>{{else}}<p class="muted">Episode details unavailable for this person.</p>{{end}}
        {{else if eq .SyncStatus "failed"}}<p class="error">Episode credits could not be loaded.</p><form action="/people/{{$.Person.TMDBID}}/retry-episodes" method="post"><button type="submit">Retry episode credits</button></form>
        {{else}}<p class="muted">Loading episode credits… This page updates automatically.</p>{{end}}
        <form class="person-title-actions" action="/people/{{$.Person.TMDBID}}/titles/{{.MediaType}}/{{.TMDBID}}/remove" method="get"><button class="danger" type="submit">Remove this title</button></form>
      </div></details>{{end}}{{else}}<p class="muted">No watched titles found for this person.</p>{{end}}
    {{else if eq .Kind "remove-person"}}
      <p>Remove this title and its credits from your watched library?</p>
      <form class="remove-actions" action="/people/{{.Person.TMDBID}}/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/delete" method="post"><button class="danger" type="submit">Remove title</button><a href="/people/{{.Person.TMDBID}}">Cancel</a></form>
    {{else if eq .Kind "remove"}}
      <p>Remove this title and its credits from your watched library?</p>
      <form class="remove-actions" action="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}/delete" method="post"><button class="danger" type="submit">Remove title</button><a href="/titles/{{.Title.MediaType}}/{{.Title.TMDBID}}">Cancel</a></form>
    {{end}}
  </main>
</body>
</html>
{{define "titleRows"}}{{range .}}<li><a class="item-link" href="/titles/{{.MediaType}}/{{.TMDBID}}"><span class="item-title title-list-item">{{with posterURL "w185" .PosterPath.String}}<img class="poster-thumb" src="{{.}}" alt="" width="72" height="108" loading="lazy">{{else}}<span class="poster-thumb poster-placeholder" aria-hidden="true"></span>{{end}}<span class="title-list-copy"><span class="title-list-name">{{.Name}}</span><span class="muted title-list-meta">{{.MediaType}}{{if .ReleaseYear.Valid}} · {{.ReleaseYear.Int64}}{{end}}</span></span></span></a></li>{{end}}{{end}}
{{define "peopleRows"}}{{range .}}<li><a class="item-link" href="/people/{{.TMDBID}}"><span class="item-title title-list-item"><img class="poster-thumb" src="/people/{{.TMDBID}}/photo" alt="" width="72" height="108" loading="lazy"><span>{{.Name}}</span></span>{{if .Roles}}<span class="muted">{{.Roles}}</span>{{end}}</a></li>{{end}}{{end}}`
