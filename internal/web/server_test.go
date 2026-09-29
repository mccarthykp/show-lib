package web

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

func newTestHandler(db *store.Store, client *tmdb.Client) http.Handler {
	return newHandler(db, client, func(context.Context, int64) (tmdb.PersonCombinedCredits, error) {
		return tmdb.PersonCombinedCredits{}, nil
	})
}

func TestGroupPersonAppearancesIncludesFullFilmography(t *testing.T) {
	saved := []store.PersonTitle{{MediaType: "movie", TMDBID: 1, Name: "Watched Film", Overview: "Stored overview", ReleaseSort: "2001", WatchedStatus: "later", InLibrary: true}}
	credits := tmdb.PersonCombinedCredits{
		Cast: []tmdb.PersonCredit{
			{ID: 1, MediaType: "movie", Title: "Watched Film", ReleaseDate: "2001-03-04", Overview: "Remote overview", Character: "Lead"},
			{ID: 2, MediaType: "movie", Title: "Unwatched Film", ReleaseDate: "2024-02-01", PosterPath: "/film.jpg", Overview: "Film overview", Character: "Detective"},
		},
		Crew: []tmdb.PersonCredit{{ID: 3, MediaType: "tv", Name: "Unwatched Series", FirstAirDate: "2023-06-01", Job: "Director"}},
	}
	watched, unwatched := groupPersonAppearances(9, saved, credits)
	if len(watched) != 1 || watched[0].Name != "Watched Film" || watched[0].Overview != "Stored overview" || watched[0].ReleaseSort != "2001-03-04" || len(unwatched) != 2 {
		t.Fatalf("watched=%+v, unwatched=%+v", watched, unwatched)
	}
	if unwatched[0].Name != "Unwatched Film" || unwatched[0].Period != "2024" || unwatched[0].Overview != "Film overview" || unwatched[0].Roles != "Detective" || unwatched[0].InLibrary {
		t.Fatalf("unwatched movie = %+v", unwatched[0])
	}
	if unwatched[1].Name != "Unwatched Series" || unwatched[1].Roles != "Director" || unwatched[1].PersonID != 9 {
		t.Fatalf("unwatched series = %+v", unwatched[1])
	}
	markWatchlisted(unwatched, []store.WatchlistTitle{{MediaType: "movie", TMDBID: 2}})
	if !unwatched[0].InWatchlist || unwatched[1].InWatchlist {
		t.Fatalf("watchlist markers = %+v", unwatched)
	}
}

func TestFormatReleaseDate(t *testing.T) {
	if got := formatReleaseDate("2024-02-01"); got != "February 1, 2024" {
		t.Fatalf("formatted release date = %q", got)
	}
	if got := formatReleaseDate("2024"); got != "2024" {
		t.Fatalf("year-only release date = %q", got)
	}
}

func TestPersonAppearancesSortNewestFirst(t *testing.T) {
	items := []personAppearance{
		{PersonTitle: store.PersonTitle{Name: "Older", ReleaseSort: "2001-01-01"}},
		{PersonTitle: store.PersonTitle{Name: "Newest", ReleaseSort: "2025-06-01"}},
		{PersonTitle: store.PersonTitle{Name: "Middle", ReleaseSort: "2018"}},
	}
	sortAppearances(items)
	if items[0].Name != "Newest" || items[1].Name != "Middle" || items[2].Name != "Older" {
		t.Fatalf("release order = %+v", items)
	}
}

func TestMergePersonAppearancesSortsAcrossLibraryStates(t *testing.T) {
	watched := []personAppearance{{PersonTitle: store.PersonTitle{Name: "Older watched", ReleaseSort: "2001", InLibrary: true}}}
	unwatched := []personAppearance{{PersonTitle: store.PersonTitle{Name: "Newer unwatched", ReleaseSort: "2025-06-01"}}}
	items := mergePersonAppearances(watched, unwatched)
	if len(items) != 2 || items[0].Name != "Newer unwatched" || items[1].Name != "Older watched" {
		t.Fatalf("merged release order = %+v", items)
	}
}

func TestTitlesLoadInBatches(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i := 1; i <= 26; i++ {
		name := fmt.Sprintf("Film %02d", i)
		_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "movie", TMDBID: int64(i), Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CompleteImport(ctx, "movie", int64(i), token, store.ImportData{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	season, episode := 1, 1
	episodeID := int64(1001)
	for i := 1; i <= 2; i++ {
		id := int64(100 + i)
		name := fmt.Sprintf("Series %02d", i)
		_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "tv", TMDBID: id, Name: name,
			Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CompleteImport(ctx, "tv", id, token, store.ImportData{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	for _, query := range []string{"", "?q=Film"} {
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/titles"+query, nil))
		if page.Code != http.StatusOK || strings.Count(page.Body.String(), `class="item-link" href="/titles/movie/`) != 12 ||
			!strings.Contains(page.Body.String(), `data-offset="12"`) ||
			!strings.Contains(page.Body.String(), `<div class="load-more-results"`) {
			t.Fatalf("first title batch %q: status=%d, rows=%d", query, page.Code,
				strings.Count(page.Body.String(), `class="item-link" href="/titles/movie/`))
		}
		more := httptest.NewRecorder()
		path := "/titles/more?offset=12"
		if query != "" {
			path += "&q=Film"
		}
		handler.ServeHTTP(more, httptest.NewRequest(http.MethodGet, path, nil))
		if more.Code != http.StatusOK || strings.Count(more.Body.String(), `class="item-link" href="/titles/movie/`) != 12 ||
			more.Header().Get("X-Has-More") != "true" || !strings.Contains(more.Body.String(), `href="/titles/movie/24"`) {
			t.Fatalf("next title batch %q: status=%d, body=%q", query, more.Code, more.Body.String())
		}
		last := httptest.NewRecorder()
		path = "/titles/more?offset=24"
		if query != "" {
			path += "&q=Film"
		}
		handler.ServeHTTP(last, httptest.NewRequest(http.MethodGet, path, nil))
		if last.Code != http.StatusOK || strings.Count(last.Body.String(), `class="item-link" href="/titles/movie/`) != 2 ||
			last.Header().Get("X-Has-More") != "" || !strings.Contains(last.Body.String(), `href="/titles/movie/26"`) {
			t.Fatalf("last title batch %q: status=%d, body=%q", query, last.Code, last.Body.String())
		}
	}
	filtered := httptest.NewRecorder()
	handler.ServeHTTP(filtered, httptest.NewRequest(http.MethodGet, "/titles?media=tv", nil))
	filteredBody := filtered.Body.String()
	if filtered.Code != http.StatusOK || strings.Count(filteredBody, `class="item-link" href="/titles/tv/`) != 2 ||
		strings.Contains(filteredBody, `href="/titles/movie/`) ||
		!strings.Contains(filteredBody, `href="/titles?media=tv" aria-current="page">TV</a>`) {
		t.Fatalf("TV title filter: status=%d, body=%q", filtered.Code, filteredBody)
	}
	bad := httptest.NewRecorder()
	handler.ServeHTTP(bad, httptest.NewRequest(http.MethodGet, "/titles/more?offset=-1", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid title offset: status %d", bad.Code)
	}
}

func TestPeopleSearchLoadsInBatches(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "movie", TMDBID: 1, Name: "Sample Film"})
	if err != nil {
		t.Fatal(err)
	}
	credits := make([]store.CreditInput, 0, 26)
	for i := 1; i <= 26; i++ {
		credits = append(credits, store.CreditInput{Key: fmt.Sprintf("actor-%02d", i), PersonTMDBID: int64(i),
			PersonName: fmt.Sprintf("Actor %02d", i), Category: "cast", CharacterName: fmt.Sprintf("Captain %02d", i)})
	}
	if err := db.CompleteImport(ctx, "movie", 1, token, store.ImportData{Name: "Sample Film", Credits: credits}); err != nil {
		t.Fatal(err)
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	for _, query := range []string{"Actor", "Captain"} {
		page := httptest.NewRecorder()
		handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/people?q="+query, nil))
		if page.Code != http.StatusOK || strings.Count(page.Body.String(), `class="item-link" href="/people/`) != 12 ||
			!strings.Contains(page.Body.String(), `data-endpoint="/people/more" data-offset="12"`) {
			t.Fatalf("first people batch %q: status=%d, body=%q", query, page.Code, page.Body.String())
		}
		for _, tc := range []struct {
			offset, count int
			hasMore       string
		}{
			{12, 12, "true"},
			{24, 2, ""},
		} {
			more := httptest.NewRecorder()
			path := fmt.Sprintf("/people/more?offset=%d&q=%s", tc.offset, query)
			handler.ServeHTTP(more, httptest.NewRequest(http.MethodGet, path, nil))
			if more.Code != http.StatusOK || strings.Count(more.Body.String(), `class="item-link" href="/people/`) != tc.count ||
				more.Header().Get("X-Has-More") != tc.hasMore {
				t.Fatalf("people batch %q at %d: status=%d, body=%q", query, tc.offset, more.Code, more.Body.String())
			}
		}
	}
}

func TestWatchlistRouteStoresAndRendersExpandableTitles(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)

	request := httptest.NewRequest(http.MethodPost, "/watchlist/movie/42", strings.NewReader(
		"person_id=9&name=A+Film&release_date=2024-03-02&poster_path=%2Fposter.jpg&overview=An+overview."))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/people/9" {
		t.Fatalf("add to watchlist: status=%d location=%q body=%q", response.Code, response.Header().Get("Location"), response.Body.String())
	}
	var multipartBody bytes.Buffer
	multipartWriter := multipart.NewWriter(&multipartBody)
	for key, value := range map[string]string{"name": "A Film", "release_date": "2024-03-02", "poster_path": "/poster.jpg", "overview": "An overview."} {
		if err := multipartWriter.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := multipartWriter.Close(); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/watchlist/movie/42", &multipartBody)
	request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("inline add to watchlist: status=%d body=%q", response.Code, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, "/watchlist/movie/44", strings.NewReader(
		"name=Odd+Metadata&release_date=unknown&poster_path=https%3A%2F%2Finvalid.example%2Fposter.jpg&overview=Still+valid."))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("inline add with invalid optional metadata: status=%d body=%q", response.Code, response.Body.String())
	}
	if err := db.RemoveFromWatchlist(ctx, "movie", 44); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPost, "/watchlist/tv/43", strings.NewReader(
		"name=A+Show&release_date=2023-01-02&overview=A+series+overview."))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/watchlist" {
		t.Fatalf("add series to watchlist: status=%d location=%q", response.Code, response.Header().Get("Location"))
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/watchlist", nil))
	body := page.Body.String()
	if page.Code != http.StatusOK ||
		!strings.Contains(body, `href="/watchlist" aria-current="page">Watchlist</a>`) ||
		!strings.Contains(body, `aria-label="Filter watchlist titles" data-watchlist-filters`) ||
		!strings.Contains(body, `<button class="watchlist-randomizer-button" type="button" data-watchlist-randomizer >Randomizer!</button>`) ||
		strings.Contains(body, `watchlist-randomizer-panel`) ||
		!strings.Contains(body, `<div data-watchlist-results>`) ||
		!strings.Contains(body, `<section aria-label="Watchlist titles" data-watchlist-titles>`) ||
		!strings.Contains(body, `<details class="person-title watchlist-title">`) ||
		!strings.Contains(body, `src="https://image.tmdb.org/t/p/w185/poster.jpg"`) ||
		!strings.Contains(body, `<span class="title-list-name person-title-name">A Film</span><span class="muted title-list-meta">movie · 2024</span>`) ||
		!strings.Contains(body, `<p class="person-title-overview">An overview.</p>`) ||
		!strings.Contains(body, `<span class="person-title-detail-label">Release date:</span> March 2, 2024</p>`) ||
		!strings.Contains(body, `href="/add/movie/42">Add to watched</a>`) ||
		!strings.Contains(body, `action="/watchlist/movie/42/delete" method="post"><button class="danger" type="submit">Remove from watchlist</button>`) {
		t.Fatalf("watchlist page: status=%d body=%q", page.Code, body)
	}
	for _, tc := range []struct {
		media, current, wanted, unwanted string
	}{
		{"movie", "Movies", "A Film", "A Show"},
		{"tv", "TV", "A Show", "A Film"},
	} {
		filtered := httptest.NewRecorder()
		handler.ServeHTTP(filtered, httptest.NewRequest(http.MethodGet, "/watchlist?media="+tc.media, nil))
		filteredBody := filtered.Body.String()
		if filtered.Code != http.StatusOK || strings.Count(filteredBody, `<details class="person-title watchlist-title">`) != 1 ||
			!strings.Contains(filteredBody, `href="/watchlist?media=`+tc.media+`" aria-current="page">`+tc.current+`</a>`) ||
			!strings.Contains(filteredBody, tc.wanted) || strings.Contains(filteredBody, tc.unwanted) {
			t.Fatalf("watchlist %s filter: status=%d body=%q", tc.media, filtered.Code, filteredBody)
		}
	}
	remove := httptest.NewRequest(http.MethodPost, "/watchlist/movie/42/delete?media=movie", nil)
	removed := httptest.NewRecorder()
	handler.ServeHTTP(removed, remove)
	if removed.Code != http.StatusSeeOther || removed.Header().Get("Location") != "/watchlist?media=movie" {
		t.Fatalf("remove from watchlist: status=%d location=%q body=%q", removed.Code, removed.Header().Get("Location"), removed.Body.String())
	}
	page = httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/watchlist", nil))
	if page.Code != http.StatusOK || strings.Contains(page.Body.String(), "A Film") || !strings.Contains(page.Body.String(), "A Show") {
		t.Fatalf("watchlist after removal: status=%d body=%q", page.Code, page.Body.String())
	}
}

func TestHealthAndEmptyLibraryRoutes(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	for _, tc := range []struct {
		path       string
		status     int
		bodyMarker string
	}{
		{"/healthz", http.StatusOK, `"status":"ok"`},
		{"/favicon.svg", http.StatusOK, `<svg xmlns="http://www.w3.org/2000/svg"`},
		{"/favicon.ico", http.StatusFound, `/favicon.svg?v=3`},
		{"/assets/live-search.js", http.StatusOK, "AbortController"},
		{"/assets/theme.css", http.StatusOK, "prefers-reduced-motion"},
		{"/titles", http.StatusOK, "Your watched library is empty"},
		{"/watchlist", http.StatusOK, "Your watchlist is empty"},
		{"/people", http.StatusOK, `data-live-search="people"`},
		{"/add", http.StatusOK, `data-live-search="add"`},
		{"/titles/movie/0", http.StatusNotFound, "404"},
		{"/unknown", http.StatusNotFound, "404"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.bodyMarker) {
			t.Errorf("GET %s: status=%d body=%q", tc.path, response.Code, response.Body.String())
		}
		if tc.path == "/favicon.svg" {
			if response.Header().Get("Content-Type") != "image/svg+xml" {
				t.Errorf("favicon content type = %q", response.Header().Get("Content-Type"))
			}
			if !strings.Contains(response.Body.String(), `mask="url(#reel-cutouts)"`) ||
				strings.Contains(response.Body.String(), `fill="#131816"`) {
				t.Error("favicon should have a transparent background and reel cutouts")
			}
		}
		if (tc.path == "/titles" || tc.path == "/people" || tc.path == "/watchlist" || tc.path == "/add") &&
			!strings.Contains(response.Body.String(), `rel="icon" type="image/svg+xml" sizes="any" href="/favicon.svg?v=3"`) {
			t.Error("page does not link the film reel favicon")
		}
		if tc.path == "/assets/theme.css" && response.Header().Get("Content-Type") != "text/css; charset=utf-8" {
			t.Errorf("theme content type = %q", response.Header().Get("Content-Type"))
		}
		if tc.path == "/assets/theme.css" && (!strings.Contains(response.Body.String(), `.person-title > summary { display: grid; grid-template-columns: 1.5rem minmax(0, 1fr) 1.5rem;`) ||
			!strings.Contains(response.Body.String(), `.person-title > summary::before { grid-column: 1; align-self: center; }`) ||
			!strings.Contains(response.Body.String(), `.person-title > summary::after { content: ''; grid-column: 3; width: 1.5rem; height: 1.5rem; }`) ||
			!strings.Contains(response.Body.String(), `.person-title-summary-content { display: flex; grid-column: 2;`) ||
			!strings.Contains(response.Body.String(), `.person-title-body { display: grid; grid-template-columns: 1.5rem minmax(0, 1fr) 1.5rem;`) ||
			!strings.Contains(response.Body.String(), `.person-title-body > * { grid-column: 2; min-width: 0; }`) ||
			!strings.Contains(response.Body.String(), `.person-title > summary .title-list-name { overflow-wrap: anywhere; white-space: normal; }`) ||
			!strings.Contains(response.Body.String(), `.person-title-overview-heading { margin: 0 0 .45rem; color: var(--text);`) ||
			!strings.Contains(response.Body.String(), `.person-title-overview { margin: 0 0 .8rem; color: var(--muted);`) ||
			!strings.Contains(response.Body.String(), `.person-title-detail { margin: .25rem 0; color: var(--muted);`) ||
			!strings.Contains(response.Body.String(), `.person-title-detail-label { color: var(--text); font-family: "Geist Mono", "SFMono-Regular", Consolas, monospace; font-size: .68rem; font-weight: 500; letter-spacing: .1em; line-height: 1.4; text-transform: uppercase; }`) ||
			!strings.Contains(response.Body.String(), `details[data-animating="closing"] > summary::before { transform: rotate(0deg); }`) ||
			!strings.Contains(response.Body.String(), `transition: transform 220ms cubic-bezier(.2,.8,.2,1);`)) {
			t.Error("person title summaries do not retain horizontal, wrapping title layout")
		}
		if tc.path == "/assets/live-search.js" && (!strings.Contains(response.Body.String(), `const detailsAnimations = new WeakMap();`) ||
			!strings.Contains(response.Body.String(), `translateY(-.25rem)`) ||
			!strings.Contains(response.Body.String(), `const collapsedHeight = summary.getBoundingClientRect().height + details.offsetHeight - details.clientHeight;`) ||
			!strings.Contains(response.Body.String(), `const watchlistRandomizer = document.querySelector("[data-watchlist-randomizer]");`) ||
			!strings.Contains(response.Body.String(), `watchlistResults.innerHTML = incomingResults.innerHTML;`) ||
			!strings.Contains(response.Body.String(), `window.history.pushState(null, "", url.pathname + url.search);`) ||
			!strings.Contains(response.Body.String(), `form.matches("[data-inline-title-action]")`) ||
			!strings.Contains(response.Body.String(), `const actionURL = submitter.getAttribute("formaction") || form.action;`) ||
			!strings.Contains(response.Body.String(), `headers: {Accept: "application/json"}`) ||
			!strings.Contains(response.Body.String(), `actions.animate(`) ||
			!strings.Contains(response.Body.String(), "const replacementAction = entry?.querySelector(`[data-title-remove=\"${stateName}\"]`);") ||
			!strings.Contains(response.Body.String(), `actionRegion.animate(`) ||
			!strings.Contains(response.Body.String(), `await Promise.allSettled(transitions.map((transition) => transition.finished));`) ||
			!strings.Contains(response.Body.String(), `if (titleAdded) {`) ||
			!strings.Contains(response.Body.String(), `title.hidden = index !== nextIndex;`)) {
			t.Error("expandable containers do not include height and content animations")
		}
		if tc.path == "/assets/theme.css" && !strings.Contains(response.Body.String(), `.top-cast-card { width: 3.8rem; font-size: .68rem; line-height: 1.2; overflow-wrap: anywhere; }`) {
			t.Error("mobile top cast names do not use compact, wrapping text")
		}
		if tc.path == "/assets/theme.css" && (!strings.Contains(response.Body.String(), `.watchlist-button { border-color: #0070f3; background: #0070f3; color: #fff; }`) ||
			!strings.Contains(response.Body.String(), `.connection-line a { color: #fff; }`) ||
			!strings.Contains(response.Body.String(), `.connection-relation { color: var(--muted); }`) ||
			!strings.Contains(response.Body.String(), `.connection-roles { margin: .55rem .8rem .8rem; padding-left: 1.6rem;`) ||
			!strings.Contains(response.Body.String(), `.connection-evidence ul { max-height: 14rem; overflow: auto; margin: 0; padding: .2rem .8rem .7rem 2.25rem;`) ||
			!strings.Contains(response.Body.String(), `.watchlist-randomizer-button {`) ||
			!strings.Contains(response.Body.String(), `margin: 1rem 0;`) ||
			strings.Contains(response.Body.String(), `.watchlist-randomizer-panel`) ||
			!strings.Contains(response.Body.String(), `animation: watchlist-randomizer-border 2.4s linear infinite;`) ||
			!strings.Contains(response.Body.String(), `@property --randomizer-border-angle { syntax: "<angle>"; inherits: false; initial-value: 0deg; }`) ||
			!strings.Contains(response.Body.String(), `@keyframes watchlist-randomizer-border { to { --randomizer-border-angle: 1turn; } }`) ||
			!strings.Contains(response.Body.String(), `.title-state-tag { display: inline-flex; width: fit-content;`) ||
			!strings.Contains(response.Body.String(), `.add-result-link .item-title { flex: 1 1 auto; align-items: flex-start; }`) ||
			!strings.Contains(response.Body.String(), `.add-result-link .title-list-copy { align-self: flex-start; justify-content: flex-start; }`) ||
			!strings.Contains(response.Body.String(), `.add-result-actions { display: grid; width: min(100%, 25rem); grid-template-columns: repeat(2, minmax(0, 1fr)); }`) ||
			!strings.Contains(response.Body.String(), `.person-title-actions.add-result-actions { display: flex; width: 100%; flex-direction: column; align-items: stretch; }`) ||
			!strings.Contains(response.Body.String(), `.person-title-actions.add-result-actions > button, .person-title-actions.add-result-actions > .button { width: 100%; }`) ||
			!strings.Contains(response.Body.String(), `.title-entry-actions button, .title-entry-actions .button { min-height: 2.35rem;`) ||
			strings.Contains(response.Body.String(), `.add-result-link .title-state-tag`) ||
			!strings.Contains(response.Body.String(), `.watchlisted-tag { border-color: #1d4e89; background: #07172b; color: #8ab4f8; }`) ||
			!strings.Contains(response.Body.String(), `.watched-tag { border-color: #1d704f; background: #071c13; color: #46d98b; }`) ||
			!strings.Contains(response.Body.String(), `[hidden] { display: none !important; }`) ||
			!strings.Contains(response.Body.String(), `.title-action-stack > .person-title-actions { grid-area: 1 / 1; align-self: start; }`) ||
			!strings.Contains(response.Body.String(), `.people-list-item { flex: 1 1 auto; align-items: flex-start; }`) ||
			!strings.Contains(response.Body.String(), `.nav-links a { padding-inline: .4rem; font-size: .67rem; letter-spacing: 0; }`)) {
			t.Error("watchlist controls do not use the blue action and compact mobile navigation styles")
		}
		if tc.path == "/titles" && !strings.Contains(response.Body.String(), `href="/assets/theme.css?v=1"`) {
			t.Error("page does not link the shared theme")
		}
		if tc.path == "/titles" && !strings.Contains(response.Body.String(), `placeholder="Title or person"`) {
			t.Error("title search placeholder is incorrect")
		}
		if tc.path == "/titles" && !strings.Contains(response.Body.String(), `>FILM &amp; TELEVISION LIBRARY</p>`) {
			t.Error("titles page heading is incorrect")
		}
		if tc.path == "/people" && (!strings.Contains(response.Body.String(), "Search people in your library") ||
			!strings.Contains(response.Body.String(), `placeholder="Person, role or crew job"`)) {
			t.Error("people search copy is incorrect")
		}
		if tc.path == "/people" && !strings.Contains(response.Body.String(), `>CAST &amp; CREW LIBRARY</p>`) {
			t.Error("people page heading is incorrect")
		}
		if (tc.path == "/titles" || tc.path == "/people" || tc.path == "/add") &&
			!strings.Contains(response.Body.String(), `class="nav-add" href="/add" aria-label="Add a title"`) {
			t.Error("add menu control lacks its accessible label")
		}
	}
}

func TestProgressRouteUsesCachedEpisodes(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	season, episode := 1, 1
	episodeID := int64(101)
	_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "tv", TMDBID: 8, Name: "Series",
		Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteImport(ctx, "tv", 8, token, store.ImportData{Name: "Series", PosterPath: "/series.jpg", Credits: []store.CreditInput{
		{Key: "late", PersonTMDBID: 2, PersonName: "Late", ProfilePath: "/late.jpg", Category: "cast", CharacterName: "Late Role"},
	}}); err != nil {
		t.Fatal(err)
	}
	titleID, started, err := db.StartEpisodeSync(ctx, 8)
	if err != nil || !started {
		t.Fatalf("sync = %v %v", started, err)
	}
	if err := db.CompleteEpisodeSync(ctx, 8, titleID, []store.EpisodeInput{
		{TMDBID: 101, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1, Name: "Pilot"},
		{TMDBID: 102, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 2, Name: "Second", Credits: []store.EpisodePersonCreditInput{
			{PersonTMDBID: 2, PersonName: "Late", Category: "cast", CharacterName: "Late Role"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185/series.jpg"`) {
		t.Fatalf("title list poster = %d, body=%q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "Search titles in your library") {
		t.Fatal("title search label does not describe titles only")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles/tv/8/progress", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Second") {
		t.Fatalf("progress page = %d, body=%q", response.Code, response.Body.String())
	}
	post := httptest.NewRequest(http.MethodPost, "/titles/tv/8/progress", strings.NewReader("season=1&episode=2"))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, post)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("progress POST = %d", response.Code)
	}
	invalid := httptest.NewRequest(http.MethodPost, "/titles/tv/8/progress", strings.NewReader("season=1&episode=99"))
	invalid.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, invalid)
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "error=invalid") {
		t.Fatalf("invalid progress POST = %d, location=%q", response.Code, response.Header().Get("Location"))
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles/tv/8", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Watched through season 1, episode 2") ||
		!strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w342/series.jpg"`) ||
		!strings.Contains(response.Body.String(), `>TELEVISION</p>`) ||
		!strings.Contains(response.Body.String(), `<section class="top-cast-panel"`) ||
		!strings.Contains(response.Body.String(), `class="top-cast-card" href="/people/2"`) ||
		strings.Contains(response.Body.String(), "credit-state watched") {
		t.Fatalf("updated title page = %d, body=%q", response.Code, response.Body.String())
	}
	titleBody := response.Body.String()
	overviewPosition := strings.Index(titleBody, `<section class="title-overview"`)
	progressPosition := strings.Index(titleBody, `<p class="title-progress">`)
	topCastPosition := strings.Index(titleBody, `<section class="top-cast-panel"`)
	if overviewPosition < 0 || progressPosition < overviewPosition || topCastPosition < progressPosition ||
		strings.Count(titleBody, `data-cast-progress-dot`) != 3 || strings.Contains(titleBody, `data-cast-next`) ||
		!strings.Contains(titleBody, `class="poster-thumb top-cast-photo" src="/people/2/photo" alt="" width="72" height="108"><span>Late</span>`) {
		t.Fatal("title progress placement or Top cast mobile progress indicator is incorrect")
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/people/2", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<details class="person-season">`) ||
		!strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185/series.jpg"`) ||
		!strings.Contains(response.Body.String(), "Episode 2 · Second") || !strings.Contains(response.Body.String(), "Remove from watched") ||
		strings.Contains(response.Body.String(), "Add to watched") ||
		!strings.Contains(response.Body.String(), `<span class="title-state-tag watched-tag">Watched</span>`) ||
		strings.Contains(response.Body.String(), `class="person-appearances-heading"`) {
		t.Fatalf("expanded person title = %d, body=%q", response.Code, response.Body.String())
	}
	rollback := httptest.NewRequest(http.MethodPost, "/titles/tv/8/progress", strings.NewReader("season=1&episode=1"))
	rollback.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, rollback)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("rollback POST = %d", response.Code)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/people/2", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "No credited episodes in your watched range.") ||
		strings.Contains(response.Body.String(), "Episode 2 · Second") ||
		!strings.Contains(response.Body.String(), `<span class="title-state-tag watched-tag">Watched</span>`) ||
		strings.Contains(response.Body.String(), `class="person-appearances-heading"`) {
		t.Fatalf("person page after rollback = %d, body=%q", response.Code, response.Body.String())
	}
}

func TestPersonTitleRemovalRedirectsAccordingToRemainingCredits(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, title := range []struct {
		id   int64
		name string
	}{{11, "First Movie"}, {12, "Second Movie"}} {
		_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "movie", TMDBID: title.id, Name: title.name})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CompleteImport(ctx, "movie", title.id, token, store.ImportData{Name: title.name, Credits: []store.CreditInput{
			{Key: "role", PersonTMDBID: 7, PersonName: "Actor", Category: "cast", CharacterName: "Role"},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	get := func(path string, status int, marker string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != status || marker != "" && !strings.Contains(response.Body.String(), marker) {
			t.Fatalf("GET %s = %d, body=%q", path, response.Code, response.Body.String())
		}
	}
	post := func(path, redirect string) {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != redirect {
			t.Fatalf("POST %s = %d, location=%q", path, response.Code, response.Header().Get("Location"))
		}
	}
	get("/people/7/titles/movie/11/remove", http.StatusOK, "Remove First Movie?")
	get("/people/7/titles/movie/99/remove", http.StatusNotFound, "")
	post("/people/7/titles/movie/11/delete", "/people/7")
	get("/people/7", http.StatusOK, "Second Movie")
	post("/people/7/titles/movie/12/delete", "/titles")
	get("/people/7", http.StatusNotFound, "")
}

func TestPeopleSearchRoute(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "showlib.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	req := store.StartImportRequest{MediaType: "movie", TMDBID: 7, Name: "Sample Show"}
	_, token, err := db.StartImport(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteImport(ctx, "movie", 7, token, store.ImportData{Name: req.Name, ReleaseDate: "2020-04-05", Overview: "Sample overview", Credits: []store.CreditInput{
		{Key: "role", PersonTMDBID: 9, PersonName: "Sam Actor", ProfilePath: "/sam.jpg", Category: "cast", CharacterName: "Captain Test"},
		{Key: "role-2", PersonTMDBID: 9, PersonName: "Sam Actor", Category: "cast", CharacterName: "Other Character"},
		{Key: "crew", PersonTMDBID: 10, PersonName: "Casey Crew", Category: "crew", Job: "Director"},
	}}); err != nil {
		t.Fatal(err)
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := newTestHandler(db, client)
	for _, tc := range []struct {
		path  string
		found bool
	}{
		{"/people", false},
		{"/people?q=Sample+Show", false},
		{"/people?q=Captain+Test", true},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", tc.path, response.Code)
		}
		found := strings.Contains(response.Body.String(), `class="item-link" href="/people/9"`)
		if found != tc.found {
			t.Errorf("GET %s: person link found=%v, want %v", tc.path, found, tc.found)
		}
	}
	for _, path := range []string{"/people/9", "/titles/movie/7"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "View on TMDB") {
			t.Errorf("GET %s: status=%d, unwanted source link=%v", path, response.Code,
				strings.Contains(response.Body.String(), "View on TMDB"))
		}
		if path == "/people/9" && !strings.Contains(response.Body.String(), `>PROFILE</p>`) {
			t.Error("person page heading is incorrect")
		}
		if path == "/titles/movie/7" && !strings.Contains(response.Body.String(), `>FILM</p>`) {
			t.Error("movie page heading is incorrect")
		}
		if path == "/titles/movie/7" && !strings.Contains(response.Body.String(), `<span class="title-state-tag watched-tag">Watched</span>`) {
			t.Error("movie page watched tag is missing")
		}
	}
	for _, tc := range []struct{ path, want, unwanted string }{
		{"/people?q=Sam+Actor", `<span class="people-list-name">Sam Actor</span>`, "Captain Test</span>"},
		{"/people?q=Captain+Test", `<span class="people-list-match-label">Role:</span> Captain Test</span>`, "Other Character</span>"},
		{"/people?q=Captn+Test", `<span class="people-list-match-label">Role:</span> Captain Test</span>`, "Other Character</span>"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tc.path, nil))
		body := response.Body.String()
		if response.Code != http.StatusOK || !strings.Contains(body, tc.want) || tc.unwanted != "" && strings.Contains(body, tc.unwanted) {
			t.Fatalf("GET %s: status=%d, expected %q without %q", tc.path, response.Code, tc.want, tc.unwanted)
		}
		if !strings.Contains(body, `src="/people/9/photo"`) || !strings.Contains(body, `width="72" height="108"`) {
			t.Fatalf("GET %s: person headshot missing or incorrectly sized", tc.path)
		}
	}
	crewResult := httptest.NewRecorder()
	handler.ServeHTTP(crewResult, httptest.NewRequest(http.MethodGet, "/people?q=Director", nil))
	if crewResult.Code != http.StatusOK || !strings.Contains(crewResult.Body.String(), `href="/people/10"`) ||
		!strings.Contains(crewResult.Body.String(), `<span class="people-list-match-label">Crew:</span> Director</span>`) {
		t.Fatalf("crew search result = %d, body=%q", crewResult.Code, crewResult.Body.String())
	}
	photo := httptest.NewRecorder()
	handler.ServeHTTP(photo, httptest.NewRequest(http.MethodGet, "/people/9/photo", nil))
	if photo.Code != http.StatusFound || photo.Header().Get("Location") != "https://image.tmdb.org/t/p/w185/sam.jpg" {
		t.Fatalf("headshot response = %d, location %q", photo.Code, photo.Header().Get("Location"))
	}
	personPage := httptest.NewRecorder()
	creditLoads := 0
	for _, id := range []int64{8, 20} {
		if err := db.AddToWatchlist(ctx, store.WatchlistTitle{MediaType: "movie", TMDBID: id, Name: fmt.Sprintf("Another Film %02d", id)}); err != nil {
			t.Fatal(err)
		}
	}
	appearanceHandler := newHandler(db, client, func(context.Context, int64) (tmdb.PersonCombinedCredits, error) {
		creditLoads++
		credits := tmdb.PersonCombinedCredits{Cast: []tmdb.PersonCredit{
			{ID: 7, MediaType: "movie", Title: "Sample Show", ReleaseDate: "2020-04-05", Overview: "Sample overview", Character: "Captain Test"},
		}}
		for id := int64(8); id < 22; id++ {
			credits.Cast = append(credits.Cast, tmdb.PersonCredit{ID: id, MediaType: "movie", Title: fmt.Sprintf("Another Film %02d", id), ReleaseDate: "2019-03-02", Overview: "Another overview", Character: "Detective"})
		}
		return credits, nil
	})
	appearanceHandler.ServeHTTP(personPage, httptest.NewRequest(http.MethodGet, "/people/9", nil))
	personBody := personPage.Body.String()
	if personPage.Code != http.StatusOK || !strings.Contains(personBody, `src="/people/9/photo?size=w342" alt="" width="208" height="312"`) ||
		!strings.Contains(personBody, `<section data-progressive-list aria-label="Appearances"><h2>Appears in:</h2>`) ||
		strings.Contains(personBody, `action="/add/movie/8"`) || !strings.Contains(personBody, `action="/add/movie/9" method="post" data-inline-title-action`) || strings.Contains(personBody, `action="/add/movie/7"`) ||
		!strings.Contains(personBody, `data-endpoint="/people/9/appearances/more" data-offset="12"`) ||
		!strings.Contains(personBody, `<span class="title-list-name person-title-name">Sample Show</span><span class="muted title-list-meta">movie · 2020</span><span class="title-entry-state" data-title-state><span class="title-state-tag watched-tag">Watched</span>`) ||
		strings.Contains(personBody, `class="person-appearances-heading"`) ||
		!strings.Contains(personBody, `<h3 class="person-title-overview-heading">Overview</h3>`) ||
		!strings.Contains(personBody, `<p class="person-title-overview">Sample overview</p>`) ||
		!strings.Contains(personBody, `<span class="muted title-list-meta">movie · 2019</span><span class="title-entry-state" data-title-state><span class="title-state-tag watchlisted-tag">Watchlisted</span>`) ||
		strings.Contains(personBody, `action="/watchlist/movie/8" method="post"`) ||
		!strings.Contains(personBody, `action="/watchlist/movie/9" method="post"`) ||
		!strings.Contains(personBody, `<button class="watchlist-button" type="submit" data-title-action="watchlisted">Add to watchlist</button>`) ||
		!strings.Contains(personBody, `data-title-remove="watched" hidden><form action="/people/9/titles/movie/9/remove" method="get"><button class="danger" type="submit">Remove from watched</button>`) ||
		!strings.Contains(personBody, `action="/watchlist/movie/8/delete" method="post"><input type="hidden" name="person_id" value="9"><button class="danger" type="submit">Remove from watchlist</button>`) ||
		!strings.Contains(personBody, `data-title-remove="watchlisted" hidden><input type="hidden" name="person_id" value="9"><button class="danger" type="submit">Remove from watchlist</button>`) ||
		strings.Contains(personBody, `action="/watchlist/movie/7"`) ||
		!strings.Contains(personBody, `<span class="person-title-detail-label">Role:</span> Captain Test, Other Character`) ||
		!strings.Contains(personBody, `<span class="person-title-detail-label">Release date:</span> April 5, 2020`) {
		t.Fatalf("person page hero or watched/unwatched actions are incorrect")
	}
	moreAppearances := httptest.NewRecorder()
	appearanceHandler.ServeHTTP(moreAppearances, httptest.NewRequest(http.MethodGet, "/people/9/appearances/more?offset=12", nil))
	if moreAppearances.Code != http.StatusOK || strings.Count(moreAppearances.Body.String(), `<details class="person-title" data-title-entry>`) != 3 ||
		moreAppearances.Header().Get("X-Has-More") != "" || creditLoads != 1 ||
		!strings.Contains(moreAppearances.Body.String(), `<span class="title-state-tag watchlisted-tag">Watchlisted</span>`) ||
		strings.Contains(moreAppearances.Body.String(), `action="/watchlist/movie/20"`) ||
		!strings.Contains(moreAppearances.Body.String(), `action="/watchlist/movie/21"`) {
		t.Fatalf("progressive person appearances = status %d, rows %d, has-more %q, credit loads %d",
			moreAppearances.Code, strings.Count(moreAppearances.Body.String(), `<details class="person-title" data-title-entry>`),
			moreAppearances.Header().Get("X-Has-More"), creditLoads)
	}
	photo = httptest.NewRecorder()
	handler.ServeHTTP(photo, httptest.NewRequest(http.MethodGet, "/people/9/photo?size=w342", nil))
	if photo.Code != http.StatusFound || photo.Header().Get("Location") != "https://image.tmdb.org/t/p/w342/sam.jpg" {
		t.Fatalf("full-size headshot response = %d, location %q", photo.Code, photo.Header().Get("Location"))
	}
	for _, path := range []string{"/titles?q=Sample+Shw", "/titles?q=Sam+Actr"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `href="/titles/movie/7"`) {
			t.Fatalf("fuzzy title search %s = %d, body=%q", path, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles/movie/7?q=Captn+Test", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Cast (1)") ||
		strings.Contains(response.Body.String(), "Other Character") {
		t.Fatalf("fuzzy credit filter = %d, body=%q", response.Code, response.Body.String())
	}
}
