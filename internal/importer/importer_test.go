package importer

import (
	"encoding/json"
	"testing"

	"show-lib/internal/tmdb"
)

func TestSeriesDataPreservesSeparateRolesAndJobs(t *testing.T) {
	var credits tmdb.AggregateCredits
	if err := json.Unmarshal([]byte(`{
		"cast":[{"id":1,"name":"Actor","order":2,"roles":[
			{"credit_id":"a","character":"One"},{"credit_id":"b","character":"Two"}]}],
		"crew":[{"id":2,"name":"Crew","department":"Writing","jobs":[
			{"credit_id":"c","job":"Writer"},{"credit_id":"d","job":"Story"}]}]
	}`), &credits); err != nil {
		t.Fatal(err)
	}
	data := seriesData(tmdb.Series{Name: "Example", FirstAirDate: "2020-01-01", PosterPath: "/series.jpg", Overview: "A series summary."}, credits)
	if len(data.Credits) != 4 || data.Credits[0].CharacterName != "One" ||
		data.Credits[1].CharacterName != "Two" || data.Credits[2].Job != "Writer" ||
		data.Credits[3].Job != "Story" {
		t.Fatalf("roles/jobs were collapsed: %+v", data.Credits)
	}
	if data.ReleaseYear == nil || *data.ReleaseYear != 2020 {
		t.Fatalf("release year = %v", data.ReleaseYear)
	}
	if data.PosterPath != "/series.jpg" || data.Overview != "A series summary." {
		t.Fatalf("series metadata = %+v", data)
	}
}

func TestMovieDataPreservesReleaseDate(t *testing.T) {
	data := movieData(tmdb.Movie{Title: "A Movie", ReleaseDate: "2003-05-17", PosterPath: "/movie.jpg", Overview: "A film summary."}, tmdb.MovieCredits{})
	if data.ReleaseDate != "2003-05-17" || data.ReleaseYear == nil || *data.ReleaseYear != 2003 {
		t.Fatalf("movie date = %+v", data)
	}
	if data.PosterPath != "/movie.jpg" || data.Overview != "A film summary." {
		t.Fatalf("movie metadata = %+v", data)
	}
}
