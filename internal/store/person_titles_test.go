package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestPersonTitlesGroupRolesAndCreditedEpisodes(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	season, episode := 2, 1
	episodeID := int64(301)
	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "tv", TMDBID: 1398, Name: "The Sopranos",
		Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "tv", 1398, token, ImportData{Name: "The Sopranos", Overview: "Series overview", Credits: []CreditInput{
		{Key: "role-a", PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Carmela Soprano"},
		{Key: "role-b", PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Another Role"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, movieToken, err := s.StartImport(ctx, StartImportRequest{MediaType: "movie", TMDBID: 42, Name: "A Movie"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 42, movieToken, ImportData{Name: "A Movie", ReleaseDate: "2003-05-17", Overview: "Movie overview", Credits: []CreditInput{
		{Key: "movie", PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Lead"},
	}}); err != nil {
		t.Fatal(err)
	}
	titleID, started, err := s.StartEpisodeSync(ctx, 1398)
	if err != nil || !started {
		t.Fatalf("sync start = %v %v", started, err)
	}
	_, started, err = s.StartEpisodeSync(ctx, 1398)
	if err != nil || started {
		t.Fatalf("duplicate sync start = %v %v", started, err)
	}
	if err := s.CompleteEpisodeSync(ctx, 1398, titleID, []EpisodeInput{
		{TMDBID: 100, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1, Name: "Pilot", AirDate: "1999-01-10", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Carmela Soprano"},
			{PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Another Role"},
		}},
		{TMDBID: 200, SeasonNumber: 2, SeasonName: "Season 2", EpisodeNumber: 1, Name: "Guy Walks into a Psychiatrist's Office...", AirDate: "2000-01-16", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Carmela Soprano"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	titles, err := s.PersonTitles(ctx, 1)
	if err != nil || len(titles) != 2 {
		t.Fatalf("person titles = %+v, err=%v", titles, err)
	}
	if titles[0].Name != "A Movie" || titles[0].Period != "2003-05-17" || titles[0].Overview != "Movie overview" || titles[0].Roles != "Lead" {
		t.Fatalf("movie = %+v", titles[0])
	}
	tv := titles[1]
	if tv.Name != "The Sopranos" || tv.Overview != "Series overview" || tv.Roles != "Carmela Soprano, Another Role" || tv.Period != "1999-2000, 2 eps" ||
		len(tv.Seasons) != 2 || len(tv.Seasons[0].Episodes) != 1 || tv.Seasons[0].Episodes[0].Name != "Pilot" {
		t.Fatalf("TV credits = %+v", tv)
	}
	if err := s.UpdateProgress(ctx, 1398, 1, 1); err != nil {
		t.Fatal(err)
	}
	titles, err = s.PersonTitles(ctx, 1)
	if err != nil || len(titles) != 2 {
		t.Fatalf("person titles after rollback = %+v, err=%v", titles, err)
	}
	tv = titles[1]
	if tv.Period != "1999, 1 eps" || len(tv.Seasons) != 1 || tv.Seasons[0].Number != 1 ||
		len(tv.Seasons[0].Episodes) != 1 || tv.Seasons[0].Episodes[0].Name != "Pilot" {
		t.Fatalf("expanded watched episodes after rollback = %+v", tv)
	}
	if err := s.UpdateProgress(ctx, 1398, 2, 1); err != nil {
		t.Fatal(err)
	}
	titles, err = s.PersonTitles(ctx, 1)
	if err != nil || titles[1].Period != "1999-2000, 2 eps" || len(titles[1].Seasons) != 2 {
		t.Fatalf("person credits after progress restored = %+v, err=%v", titles, err)
	}
}

func TestEpisodeOnlyCreditHandlesMissingDepartment(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	season, episode := 1, 1
	episodeID := int64(801)
	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "tv", TMDBID: 80, Name: "Series",
		Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "tv", 80, token, ImportData{Name: "Series"}); err != nil {
		t.Fatal(err)
	}
	titleID, started, err := s.StartEpisodeSync(ctx, 80)
	if err != nil || !started {
		t.Fatalf("episode sync = %v, %v", started, err)
	}
	if err := s.CompleteEpisodeSync(ctx, 80, titleID, []EpisodeInput{{TMDBID: episodeID,
		SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1, Name: "Pilot", AirDate: "2020-01-01",
		Credits: []EpisodePersonCreditInput{{PersonTMDBID: 9, PersonName: "Guest Actor", Category: "cast", CharacterName: "Paramedic"}},
	}}); err != nil {
		t.Fatal(err)
	}
	var department string
	if err := s.db.QueryRowContext(ctx, "SELECT department FROM credits WHERE title_id = ?", titleID).Scan(&department); err != nil || department != "" {
		t.Fatalf("new episode credit department = %q, err=%v", department, err)
	}
	// A prior development build stored NULL for episode-only cast departments.
	if _, err := s.db.ExecContext(ctx, "UPDATE credits SET department = NULL WHERE title_id = ?", titleID); err != nil {
		t.Fatal(err)
	}
	credits, err := s.TitleCredits(ctx, "tv", 80)
	if err != nil || len(credits) != 1 || credits[0].Department != "" {
		t.Fatalf("title credits = %+v, err=%v", credits, err)
	}
	titles, err := s.PersonTitles(ctx, 9)
	if err != nil || len(titles) != 1 || titles[0].Period != "2020, 1 eps" {
		t.Fatalf("person titles = %+v, err=%v", titles, err)
	}
	people, err := s.SearchPeople(ctx, "Paramedic")
	if err != nil || len(people) != 1 || people[0].Name != "Guest Actor" {
		t.Fatalf("people search = %+v, err=%v", people, err)
	}
	people, err = s.SearchPeople(ctx, "Series")
	if err != nil || len(people) != 0 {
		t.Fatalf("title search returned people = %+v, err=%v", people, err)
	}
}
