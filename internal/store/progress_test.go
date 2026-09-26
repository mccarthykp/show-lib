package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestProgressChangesWatchedRangeWithoutChangingCredits(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	season, episode := 1, 1
	episodeID := int64(101)
	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "tv", TMDBID: 8, Name: "Series",
		Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "tv", 8, token, ImportData{Name: "Series", Credits: []CreditInput{
		{Key: "early", PersonTMDBID: 1, PersonName: "Early", Category: "cast", CharacterName: "Early Role"},
		{Key: "late", PersonTMDBID: 2, PersonName: "Late", Category: "cast", CharacterName: "Late Role"},
		{Key: "unknown", PersonTMDBID: 3, PersonName: "Unknown", Category: "crew", Job: "Director"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProgress(ctx, 8, 1, 2); !errors.Is(err, ErrEpisodeDataUnavailable) {
		t.Fatalf("update before episode import = %v", err)
	}
	titleID, started, err := s.StartEpisodeSync(ctx, 8)
	if err != nil || !started {
		t.Fatalf("start episode sync = %v, %v", started, err)
	}
	if err := s.FailEpisodeSync(ctx, titleID, "temporary TMDB error"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProgress(ctx, 8, 1, 2); !errors.Is(err, ErrEpisodeDataUnavailable) {
		t.Fatalf("update after failed episode import = %v", err)
	}
	oldTitle, err := s.GetTitle(ctx, "tv", 8)
	if err != nil || oldTitle.WatchedEpisode.Int64 != 1 {
		t.Fatalf("failed import changed progress = %+v, %v", oldTitle, err)
	}
	titleID, started, err = s.StartEpisodeSync(ctx, 8)
	if err != nil || !started {
		t.Fatalf("retry episode sync = %v, %v", started, err)
	}
	if err := s.CompleteEpisodeSync(ctx, 8, titleID, []EpisodeInput{
		{TMDBID: 101, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 1, Name: "Pilot", AirDate: "2000-01-01", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 1, PersonName: "Early", Category: "cast", CharacterName: "Early Role"},
		}},
		{TMDBID: 102, SeasonNumber: 1, SeasonName: "Season 1", EpisodeNumber: 2, Name: "Second", AirDate: "2000-01-08", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 2, PersonName: "Late", Category: "cast", CharacterName: "Late Role"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	assertCreditStatuses := func(want []string) {
		t.Helper()
		title, err := s.GetTitle(ctx, "tv", 8)
		if err != nil {
			t.Fatal(err)
		}
		credits, err := s.TitleCredits(ctx, "tv", 8)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.ApplyCreditAppearances(ctx, title, credits); err != nil {
			t.Fatal(err)
		}
		if len(credits) != 3 {
			t.Fatalf("credits = %+v", credits)
		}
		for i, status := range want {
			if credits[i].Status != status {
				t.Fatalf("credit %d status = %q, want %q", i, credits[i].Status, status)
			}
		}
	}
	assertCreditStatuses([]string{"watched", "later", "unknown"})
	personTitles, err := s.PersonTitles(ctx, 2)
	if err != nil || len(personTitles) != 1 || personTitles[0].WatchedStatus != "later" ||
		personTitles[0].Period != "" || len(personTitles[0].Seasons) != 0 {
		t.Fatalf("later person = %+v, err=%v", personTitles, err)
	}
	if err := s.UpdateProgress(ctx, 8, 1, 2); err != nil {
		t.Fatal(err)
	}
	assertCreditStatuses([]string{"watched", "watched", "unknown"})
	personTitles, err = s.PersonTitles(ctx, 2)
	if err != nil || personTitles[0].WatchedStatus != "watched" || personTitles[0].Period != "2000, 1 eps" {
		t.Fatalf("advanced person = %+v, err=%v", personTitles, err)
	}
	if err := s.UpdateProgress(ctx, 8, 1, 99); !errors.Is(err, ErrInvalidEpisode) {
		t.Fatalf("invalid update = %v", err)
	}
	title, err := s.GetTitle(ctx, "tv", 8)
	if err != nil || title.WatchedEpisode.Int64 != 2 {
		t.Fatalf("progress changed after invalid update = %+v, err=%v", title, err)
	}
	if err := s.UpdateProgress(ctx, 8, 1, 1); err != nil {
		t.Fatal(err)
	}
	assertCreditStatuses([]string{"watched", "later", "unknown"})
}

func TestSelectedSpecialDoesNotInferOtherEpisodes(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	season, episode := 1, 1
	episodeID := int64(101)
	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "tv", TMDBID: 9, Name: "Series",
		Season: &season, Episode: &episode, EpisodeTMDBID: &episodeID})
	if err != nil {
		t.Fatal(err)
	}
	credits := []CreditInput{
		{Key: "regular", PersonTMDBID: 1, PersonName: "Regular", Category: "cast", CharacterName: "Regular"},
		{Key: "special-one", PersonTMDBID: 2, PersonName: "Special One", Category: "cast", CharacterName: "One"},
		{Key: "special-two", PersonTMDBID: 3, PersonName: "Special Two", Category: "cast", CharacterName: "Two"},
	}
	if err := s.CompleteImport(ctx, "tv", 9, token, ImportData{Name: "Series", Credits: credits}); err != nil {
		t.Fatal(err)
	}
	titleID, _, err := s.StartEpisodeSync(ctx, 9)
	if err != nil {
		t.Fatal(err)
	}
	episodes := []EpisodeInput{
		{TMDBID: 101, SeasonNumber: 1, EpisodeNumber: 1, Name: "Pilot", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 1, PersonName: "Regular", Category: "cast", CharacterName: "Regular"},
		}},
		{TMDBID: 201, SeasonNumber: 0, SeasonName: "Specials", EpisodeNumber: 1, Name: "First Special", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 2, PersonName: "Special One", Category: "cast", CharacterName: "One"},
		}},
		{TMDBID: 202, SeasonNumber: 0, SeasonName: "Specials", EpisodeNumber: 2, Name: "Second Special", Credits: []EpisodePersonCreditInput{
			{PersonTMDBID: 3, PersonName: "Special Two", Category: "cast", CharacterName: "Two"},
		}},
	}
	if err := s.CompleteEpisodeSync(ctx, 9, titleID, episodes); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateProgress(ctx, 9, 0, 1); err != nil {
		t.Fatal(err)
	}
	title, err := s.GetTitle(ctx, "tv", 9)
	if err != nil {
		t.Fatal(err)
	}
	views, err := s.TitleCredits(ctx, "tv", 9)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyCreditAppearances(ctx, title, views); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"later", "watched", "later"} {
		if views[i].Status != want {
			t.Fatalf("special credit %d = %q, want %q", i, views[i].Status, want)
		}
	}
	invalid := EpisodeInput{TMDBID: 203, SeasonNumber: 0, EpisodeNumber: 3, Name: "Invalid", Credits: []EpisodePersonCreditInput{
		{PersonTMDBID: 0, PersonName: "Bad", Category: "cast"},
	}}
	if err := s.AddSpecialProgress(ctx, 9, invalid); err == nil {
		t.Fatal("invalid special credit was accepted")
	}
	title, err = s.GetTitle(ctx, "tv", 9)
	if err != nil || title.WatchedEpisodeTMDBID.Int64 != 201 {
		t.Fatalf("invalid special changed progress = %+v, %v", title, err)
	}
}
