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
	if err := json.Unmarshal([]byte(`{"results":[{"id":1,"title":"Example Movie","poster_path":"/movie.jpg"},{"id":2,"name":"Example Show","poster_path":"/show.jpg"},{"id":3,"title":"Example Fresh Movie","release_date":"2025-02-01","poster_path":"/fresh-movie.jpg","overview":"A fresh movie overview."},{"id":4,"name":"Example Fresh Show","first_air_date":"2024-03-01","poster_path":"/fresh-show.jpg","overview":"A fresh show overview."}]}`), &results); err != nil {
		t.Fatal(err)
	}
	movies := movieHits("Example", results.Results)
	series := seriesHits("Example", results.Results)
	if len(movies) != 2 || movies[0].PosterPath != "/movie.jpg" || len(series) != 2 || series[0].PosterPath != "/show.jpg" {
		t.Fatalf("poster paths missing from search hits: movies=%+v series=%+v", movies, series)
	}
	server := &Server{page: template.Must(template.New("page").Funcs(pageTemplateFuncs()).Parse(pageHTML))}
	markSavedSearchHits(movies, []store.Title{{MediaType: "movie", TMDBID: 1}})
	markWatchlistedSearchHits(series, []store.WatchlistTitle{{MediaType: "tv", TMDBID: 2}})
	response := httptest.NewRecorder()
	server.render(response, pageData{Kind: "add", Query: "Example", Results: append(movies, series...)})
	if strings.Count(response.Body.String(), `<span class="title-state-tag watched-tag">Watched</span>`) != 1 ||
		strings.Count(response.Body.String(), `<span class="title-state-tag watchlisted-tag">Watchlisted</span>`) != 1 ||
		strings.Contains(response.Body.String(), `added-check`) || strings.Contains(response.Body.String(), `aria-label="Already in library"`) {
		t.Fatal("search results should use watched and watchlisted tags without a check mark")
	}
	if !strings.Contains(response.Body.String(), `<span class="muted title-list-meta">movie</span><span class="title-entry-state" data-title-state><span class="title-state-tag watched-tag">Watched</span>`) ||
		!strings.Contains(response.Body.String(), `<span class="muted title-list-meta">tv</span><span class="title-entry-state" data-title-state><span class="title-state-tag watchlisted-tag">Watchlisted</span>`) {
		t.Fatal("search result tags should appear directly below media metadata")
	}
	if !strings.Contains(response.Body.String(), `formaction="/add/movie/3" data-title-action="watched">Add to watched</button>`) ||
		!strings.Contains(response.Body.String(), `action="/watchlist/movie/3" method="post" data-inline-title-action data-title-actions`) ||
		!strings.Contains(response.Body.String(), `href="/add/tv/4">Add to watched</a>`) ||
		!strings.Contains(response.Body.String(), `action="/watchlist/tv/4" method="post" data-inline-title-action data-title-actions`) {
		t.Fatal("untracked search results should show inline watched and watchlist actions")
	}
	if strings.Count(response.Body.String(), `<details class="person-title add-title" data-title-entry>`) != 4 ||
		!strings.Contains(response.Body.String(), `<h3 class="person-title-overview-heading">Overview</h3>`) ||
		!strings.Contains(response.Body.String(), `<p class="person-title-overview">A fresh movie overview.</p>`) ||
		!strings.Contains(response.Body.String(), `<span class="person-title-detail-label">Release date:</span> February 1, 2025</p>`) ||
		!strings.Contains(response.Body.String(), `name="release_date" value="2025-02-01"`) ||
		!strings.Contains(response.Body.String(), `name="overview" value="A fresh movie overview."`) {
		t.Fatal("search results should use expandable title cards with overview, full release date, and actions")
	}
	for _, path := range []string{"/movie.jpg", "/show.jpg"} {
		if !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w185`+path+`"`) {
			t.Fatalf("search artwork %q missing", path)
		}
	}
	for index, path := range []string{"/movie.jpg", "/show.jpg"} {
		response = httptest.NewRecorder()
		data := pageData{Kind: "preview", Heading: "Example", PreviewPoster: path}
		if index == 0 {
			data.AlreadyAdded = true
		} else {
			data.Watchlisted = true
		}
		server.render(response, data)
		if !strings.Contains(response.Body.String(), `src="https://image.tmdb.org/t/p/w342`+path+`"`) {
			t.Fatalf("preview artwork %q missing", path)
		}
		want := "Watched"
		if index == 1 {
			want = "Watchlisted"
		}
		className := "watched-tag"
		if index == 1 {
			className = "watchlisted-tag"
		}
		if !strings.Contains(response.Body.String(), `<span class="title-state-tag `+className+`">`+want+`</span>`) {
			t.Fatalf("preview state %q missing", want)
		}
	}
}

func TestSavedSearchHitsKeepMovieAndSeriesSeparate(t *testing.T) {
	hits := []searchHit{{MediaType: "movie", TMDBID: 42}, {MediaType: "tv", TMDBID: 42}}
	markSavedSearchHits(hits, []store.Title{{MediaType: "tv", TMDBID: 42}})
	markWatchlistedSearchHits(hits, []store.WatchlistTitle{{MediaType: "movie", TMDBID: 42}})
	if hits[0].InLibrary || !hits[0].InWatchlist || !hits[1].InLibrary || hits[1].InWatchlist {
		t.Fatalf("saved title markers = %+v", hits)
	}
}

func TestPreviewStoreKeepsRetrievedTitleAndSeasonDetails(t *testing.T) {
	cache := newPreviewStore()
	cache.storeMovie(tmdb.Movie{ID: 10, Title: "Cached Movie", Overview: "Already loaded"})
	cache.storeSeries(tmdb.Series{ID: 20, Name: "Cached Series", Overview: "Already loaded"})
	cache.storeSeason(20, 2, tmdb.SeasonDetails{Name: "Season 2", Episodes: []tmdb.Episode{{ID: 201, EpisodeNumber: 1}}})

	movie, movieOK := cache.movie(10)
	series, seriesOK := cache.series(20)
	season, seasonOK := cache.season(20, 2)
	if !movieOK || movie.Title != "Cached Movie" || !seriesOK || series.Name != "Cached Series" ||
		!seasonOK || season.Name != "Season 2" || len(season.Episodes) != 1 {
		t.Fatalf("cached movie=%+v series=%+v season=%+v", movie, series, season)
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
