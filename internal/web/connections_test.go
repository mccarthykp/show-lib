package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

func TestConnectionsFollowWatchedProgressAndRemoval(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cast := func(id int64, name, role string) store.CreditInput {
		return store.CreditInput{Key: name, PersonTMDBID: id, PersonName: name, Category: "cast", CharacterName: role}
	}
	epCast := func(id int64, name, role string) store.EpisodePersonCreditInput {
		return store.EpisodePersonCreditInput{PersonTMDBID: id, PersonName: name, Category: "cast", CharacterName: role}
	}
	addSeries := func(id int64, name string, credits []store.CreditInput, episodes []store.EpisodeInput) {
		t.Helper()
		season, episode := 1, 1
		episodeID := episodes[0].TMDBID
		_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "tv", TMDBID: id,
			Name: name, Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.CompleteImport(ctx, "tv", id, token, store.ImportData{Name: name, Credits: credits}); err != nil {
			t.Fatal(err)
		}
		titleID, started, err := db.StartEpisodeSync(ctx, id)
		if err != nil || !started {
			t.Fatalf("start %s episode sync = %v, %v", name, started, err)
		}
		if err := db.CompleteEpisodeSync(ctx, id, titleID, episodes); err != nil {
			t.Fatal(err)
		}
	}
	anchorCredits := []store.CreditInput{
		cast(1, "Anna", "Anchor Lead"), cast(2, "Ben", "Anchor Friend"),
		cast(3, "Cal", "Anchor Doctor"), cast(4, "Future", "Anchor Guest"),
		cast(5, "Unmapped", "Unknown Role"),
	}
	anchorEpisodes := []store.EpisodeInput{{TMDBID: 101, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1,
		Name: "Pilot", Credits: []store.EpisodePersonCreditInput{
			epCast(1, "Anna", "Anchor Lead"), epCast(2, "Ben", "Anchor Friend"),
			epCast(3, "Cal", "Anchor Doctor"), epCast(4, "Future", "Anchor Guest"),
		}}}
	addSeries(1, "Anchor", anchorCredits, anchorEpisodes)
	secondCredits := []store.CreditInput{
		cast(1, "Anna", "Second Lead"), cast(2, "Ben", "Second Friend"),
		cast(3, "Cal", "Second Doctor"), cast(4, "Future", "Second Guest"),
		cast(5, "Unmapped", "Unknown Role"),
	}
	secondEpisodes := []store.EpisodeInput{
		{TMDBID: 201, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1,
			Name: "First", Credits: []store.EpisodePersonCreditInput{
				epCast(1, "Anna", "Second Lead"), epCast(2, "Ben", "Second Friend"), epCast(3, "Cal", "Second Doctor"),
			}},
		{TMDBID: 202, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 2,
			Name: "Later", Credits: []store.EpisodePersonCreditInput{epCast(4, "Future", "Second Guest")}},
	}
	addSeries(2, "Second", secondCredits, secondEpisodes)
	_, token, err := db.StartImport(ctx, store.StartImportRequest{MediaType: "movie", TMDBID: 3, Name: "Movie"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CompleteImport(ctx, "movie", 3, token, store.ImportData{Name: "Movie", Credits: []store.CreditInput{
		cast(1, "Anna", "Movie Lead"),
	}}); err != nil {
		t.Fatal(err)
	}
	client, err := tmdb.New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	handler := New(db, client)
	panel := func(path string) string {
		t.Helper()
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		body := response.Body.String()
		start := strings.Index(body, `<section class="connection-panel"`)
		end := -1
		if start >= 0 {
			if offset := strings.Index(body[start:], "</section>"); offset >= 0 {
				end = start + offset
			}
		}
		if response.Code != http.StatusOK || start < 0 || end < start {
			t.Fatalf("GET %s = %d, body=%q", path, response.Code, body)
		}
		return body[start:end]
	}
	anchor := panel("/titles/tv/1")
	if !strings.Contains(anchor, `<summary>4 connections</summary>`) || strings.Contains(anchor, `<details class="connection-disclosure" open`) {
		t.Fatalf("initial connection summary = %q", anchor)
	}
	for _, want := range []string{
		"Ben</a>, <a href=\"/people/3\">Cal</a> <span class=\"connection-relation\">also appear in</span> <a href=\"/titles/tv/2\">Second</a>",
		"Anna</a> <span class=\"connection-relation\">also appears in</span> <a href=\"/titles/movie/3\">Movie</a>, <a href=\"/titles/tv/2\">Second</a>",
		"<a href=\"/people/2\">Ben</a>: Second Friend in <a href=\"/titles/tv/2\">Second</a>",
		`<li class="connection-current-credit">Anchor Lead in Anchor`,
		"<li>Movie Lead in <a href=\"/titles/movie/3\">Movie</a>",
		"1 credited episode in Anchor",
		"1 credited episode in Second",
	} {
		if !strings.Contains(anchor, want) {
			t.Errorf("anchor connections missing %q", want)
		}
	}
	currentCredit := strings.Index(anchor, `<li class="connection-current-credit">Anchor Lead in Anchor`)
	firstOtherCredit := strings.Index(anchor, `<li>Movie Lead in <a href="/titles/movie/3">Movie</a>`)
	if currentCredit < 0 || firstOtherCredit <= currentCredit || strings.Count(anchor, "Anchor Lead in Anchor") != 1 ||
		strings.Contains(anchor, `<li><a href="/people/1">Anna</a>:`) {
		t.Fatal("person-centered details do not show one current-title credit before the other credits")
	}
	if strings.Contains(anchor, "Future</a>") {
		t.Fatal("future-only credit appeared before watched progress")
	}
	if strings.Contains(anchor, "Unmapped</a>") {
		t.Fatal("unmapped aggregate credit appeared in connections")
	}
	if movie := panel("/titles/movie/3"); !strings.Contains(movie, `<summary>2 connections</summary>`) || !strings.Contains(movie, `Anna</a> <span class="connection-relation">also appears in</span>`) ||
		!strings.Contains(movie, "Anchor</a>, <a href=\"/titles/tv/2\">Second</a>") {
		t.Fatalf("movie-to-series connections = %q", movie)
	}
	if err := db.UpdateProgress(ctx, 2, 1, 2); err != nil {
		t.Fatal(err)
	}
	if advanced := panel("/titles/tv/1"); !strings.Contains(advanced, `<summary>5 connections</summary>`) || !strings.Contains(advanced, `Future</a> <span class="connection-relation">also appear in</span>`) {
		t.Fatal("advancing progress did not reveal future credit")
	}
	if err := db.UpdateProgress(ctx, 2, 1, 1); err != nil {
		t.Fatal(err)
	}
	if rolledBack := panel("/titles/tv/1"); !strings.Contains(rolledBack, `<summary>4 connections</summary>`) || strings.Contains(rolledBack, "Future</a>") {
		t.Fatal("rolling back progress kept future credit in connections")
	}
	if err := db.DeleteTitle(ctx, "tv", 2); err != nil {
		t.Fatal(err)
	}
	if removed := panel("/titles/tv/1"); !strings.Contains(removed, `<summary>1 connection</summary>`) || strings.Contains(removed, "Second</a>") || !strings.Contains(removed, "Movie</a>") {
		t.Fatalf("connections after removal = %q", removed)
	}
	addSeries(2, "Second", secondCredits, secondEpisodes)
	if restored := panel("/titles/tv/1"); !strings.Contains(restored, `<summary>4 connections</summary>`) || !strings.Contains(restored, "Second</a>") {
		t.Fatal("re-added title did not restore connection")
	}
	if err := db.DeleteTitle(ctx, "tv", 2); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteTitle(ctx, "movie", 3); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/titles/tv/1", nil))
	if body := response.Body.String(); response.Code != http.StatusOK || !strings.Contains(body, `<p class="no-connections">No connections</p>`) || strings.Contains(body, `class="connection-panel"`) {
		t.Fatalf("title without connections = %d, body=%q", response.Code, body)
	}
}
