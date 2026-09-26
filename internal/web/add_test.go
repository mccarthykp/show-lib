package web

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http/httptest"
	"strings"
	"testing"

	"show-lib/internal/store"
	"show-lib/internal/tmdb"
)

func TestAddPosterArtwork(t *testing.T) {
	var results tmdb.SearchResults
	if err := json.Unmarshal([]byte(`{"results":[{"id":1,"title":"Example Movie","poster_path":"/movie.jpg"},{"id":2,"name":"Example Show","poster_path":"/show.jpg"}]}`), &results); err != nil {
		t.Fatal(err)
	}
	movies := movieHits("Example", results.Results)
	series := seriesHits("Example", results.Results)
	if len(movies) != 1 || movies[0].PosterPath != "/movie.jpg" || len(series) != 1 || series[0].PosterPath != "/show.jpg" {
		t.Fatalf("poster paths missing from search hits: movies=%+v series=%+v", movies, series)
	}
	server := &Server{page: template.Must(template.New("page").Funcs(template.FuncMap{"posterURL": posterURL}).Parse(pageHTML))}
	markSavedSearchHits(movies, []store.Title{{MediaType: "movie", TMDBID: 1}})
	response := httptest.NewRecorder()
	server.render(response, pageData{Kind: "add", Query: "Example", Results: append(movies, series...)})
	if strings.Count(response.Body.String(), `aria-label="Already in library"`) != 1 {
		t.Fatal("saved result should have one check mark")
	}
	for _, path := range []string{"/movie.jpg", "/show.jpg"} {
		if !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185`+path+`"`) {
			t.Fatalf("search artwork %q missing", path)
		}
	}
	for _, path := range []string{"/movie.jpg", "/show.jpg"} {
		response = httptest.NewRecorder()
		server.render(response, pageData{Kind: "preview", Heading: "Example", PreviewPoster: path})
		if !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w342`+path+`"`) {
			t.Fatalf("preview artwork %q missing", path)
		}
	}
}

func TestSavedSearchHitsKeepMovieAndSeriesSeparate(t *testing.T) {
	hits := []searchHit{{MediaType: "movie", TMDBID: 42}, {MediaType: "tv", TMDBID: 42}}
	markSavedSearchHits(hits, []store.Title{{MediaType: "tv", TMDBID: 42}})
	if hits[0].InLibrary || !hits[1].InLibrary {
		t.Fatalf("saved title markers = %+v", hits)
	}
}

func TestFuzzyTMDBFallback(t *testing.T) {
	queries := fallbackQueries("Nurse Jakie")
	if len(queries) != 2 || queries[0] != "Nurse" || queries[1] != "Jaki" {
		t.Fatalf("fallback queries = %v", queries)
	}
	queries = fallbackQueries("Nurse-Jakie")
	if len(queries) != 2 || queries[0] != "Nurse" || queries[1] != "Jaki" {
		t.Fatalf("dashed fallback queries = %v", queries)
	}
	queries = fallbackQueries("th-sop")
	if len(queries) != 1 || queries[0] != "sop" {
		t.Fatalf("short dashed fallback queries = %v", queries)
	}
	queries = fallbackQueries("thsop")
	if len(queries) != 2 || queries[0] != "thso" || queries[1] != "sop" {
		t.Fatalf("compact fallback queries = %v", queries)
	}
	search := func(_ context.Context, query string, _ int) (tmdb.SearchResults, error) {
		switch query {
		case "Nurse":
			return tmdb.SearchResults{Results: []tmdb.SearchItem{{ID: 1, Name: "Nurse Jackie"}, {ID: 2, Name: "Nurse Ratchet"}}}, nil
		case "Jaki":
			return tmdb.SearchResults{Results: []tmdb.SearchItem{{ID: 1, Name: "Nurse Jackie"}}}, nil
		default:
			return tmdb.SearchResults{}, nil
		}
	}
	results := fuzzyTMDBFallback(context.Background(), "Nurse Jakie", search)
	if len(results) != 1 || results[0].Name != "Nurse Jackie" {
		t.Fatalf("fuzzy TMDB results = %+v", results)
	}
}

func TestTMDBHitsFollowTypedOrder(t *testing.T) {
	items := []tmdb.SearchItem{
		{ID: 1, Title: "Nurse Jackie", Name: "Nurse Jackie"},
		{ID: 2, Title: "Jackie Nurse", Name: "Jackie Nurse"},
	}
	movies := movieHits("nrs-jck", items)
	series := seriesHits("nrs jck", items)
	if len(movies) != 1 || movies[0].TMDBID != 1 || len(series) != 1 || series[0].TMDBID != 1 {
		t.Fatalf("ordered TMDB hits: movies=%+v series=%+v", movies, series)
	}
}
