package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

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
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, client)
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
	handler := New(db, client)
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
	handler := New(db, client)
	for _, tc := range []struct {
		path       string
		status     int
		bodyMarker string
	}{
		{"/healthz", http.StatusOK, `"status":"ok"`},
		{"/favicon.svg", http.StatusOK, `<svg xmlns="http://www.w3.org/2000/svg"`},
		{"/favicon.ico", http.StatusFound, `/favicon.svg?v=3`},
		{"/assets/live-search.js", http.StatusOK, "AbortController"},
		{"/titles", http.StatusOK, "Your watched library is empty"},
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
		if (tc.path == "/titles" || tc.path == "/people" || tc.path == "/add") &&
			!strings.Contains(response.Body.String(), `rel="icon" type="image/svg+xml" sizes="any" href="/favicon.svg?v=3"`) {
			t.Error("page does not link the film reel favicon")
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
		{Key: "late", PersonTMDBID: 2, PersonName: "Late", Category: "cast", CharacterName: "Late Role"},
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
	handler := New(db, client)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185/series.jpg"`) {
		t.Fatalf("title list poster = %d, body=%q", response.Code, response.Body.String())
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
		!strings.Contains(response.Body.String(), "Watched credit") {
		t.Fatalf("updated title page = %d, body=%q", response.Code, response.Body.String())
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/people/2", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<details class="person-season">`) ||
		!strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185/series.jpg"`) ||
		!strings.Contains(response.Body.String(), "Episode 2 · Second") || !strings.Contains(response.Body.String(), "Remove this title") {
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
		strings.Contains(response.Body.String(), "Episode 2 · Second") {
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
	handler := New(db, client)
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
	if err := db.CompleteImport(ctx, "movie", 7, token, store.ImportData{Name: req.Name, Credits: []store.CreditInput{
		{Key: "role", PersonTMDBID: 9, PersonName: "Sam Actor", ProfilePath: "/sam.jpg", Category: "cast", CharacterName: "Captain Test"},
		{Key: "role-2", PersonTMDBID: 9, PersonName: "Sam Actor", Category: "cast", CharacterName: "Other Character"},
	}}); err != nil {
		t.Fatal(err)
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, client)
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
	}
	for _, tc := range []struct{ path, want, unwanted string }{
		{"/people?q=Sam+Actor", `<span>Sam Actor</span></span></a>`, "Captain Test</span></a>"},
		{"/people?q=Captain+Test", `<span class="muted">Captain Test</span></a>`, "Other Character</span></a>"},
		{"/people?q=Captn+Test", `<span class="muted">Captain Test</span></a>`, "Other Character</span></a>"},
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
	photo := httptest.NewRecorder()
	handler.ServeHTTP(photo, httptest.NewRequest(http.MethodGet, "/people/9/photo", nil))
	if photo.Code != http.StatusFound || photo.Header().Get("Location") != "https://image.tmdb.org/t/p/w185/sam.jpg" {
		t.Fatalf("headshot response = %d, location %q", photo.Code, photo.Header().Get("Location"))
	}
	personPage := httptest.NewRecorder()
	handler.ServeHTTP(personPage, httptest.NewRequest(http.MethodGet, "/people/9", nil))
	if personPage.Code != http.StatusOK || !strings.Contains(personPage.Body.String(), `src="/people/9/photo?size=w342" alt="" width="208" height="312"`) {
		t.Fatalf("person page hero is missing its full-size headshot")
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
