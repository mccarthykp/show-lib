package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestImportRetryIdentityAndRemoval(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	req := StartImportRequest{MediaType: "movie", TMDBID: 769, Name: "Goodfellas"}
	state, token, err := s.StartImport(ctx, req)
	if err != nil || state != ImportStarted || token == "" {
		t.Fatalf("start = %q, token=%v, err=%v", state, token != "", err)
	}
	state, _, err = s.StartImport(ctx, req)
	if err != nil || state != ImportRunning {
		t.Fatalf("duplicate in-progress start = %q, err=%v", state, err)
	}
	data := ImportData{Name: "Goodfellas", Overview: "A crime drama.", Credits: []CreditInput{
		{Key: "role-1", PersonTMDBID: 10, PersonName: "Shared Person", Category: "cast", CharacterName: "A"},
		{Key: "role-2", PersonTMDBID: 10, PersonName: "Shared Person", Category: "cast", CharacterName: "B"},
	}}
	if err := s.CompleteImport(ctx, "movie", 769, "wrong-token", data); !errors.Is(err, ErrImportCancelled) {
		t.Fatalf("wrong token completion = %v", err)
	}
	if err := s.CompleteImport(ctx, "movie", 769, token, data); err != nil {
		t.Fatal(err)
	}
	if title, err := s.GetTitle(ctx, "movie", 769); err != nil || title.Overview.String != data.Overview {
		t.Fatalf("imported overview = %+v, err=%v", title, err)
	}
	state, _, err = s.StartImport(ctx, req)
	if err != nil || state != ImportExists {
		t.Fatalf("duplicate completed start = %q, err=%v", state, err)
	}
	credits, err := s.TitleCredits(ctx, "movie", 769)
	if err != nil || len(credits) != 2 {
		t.Fatalf("distinct credits = %d, err=%v", len(credits), err)
	}
	second := StartImportRequest{MediaType: "movie", TMDBID: 238, Name: "Another movie"}
	_, secondToken, err := s.StartImport(ctx, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 238, secondToken, ImportData{Name: second.Name,
		Credits: []CreditInput{{Key: "shared", PersonTMDBID: 10, PersonName: "Shared Person", Category: "crew", Job: "Director"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTitle(ctx, "movie", 769); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPerson(ctx, 10); err != nil {
		t.Fatal("shared person was removed with first title:", err)
	}
	state, newToken, err := s.StartImport(ctx, req)
	if err != nil || state != ImportStarted || newToken == token {
		t.Fatalf("re-add = %q, distinct token=%v, err=%v", state, newToken != token, err)
	}
	if err := s.CompleteImport(ctx, "movie", 769, token, data); !errors.Is(err, ErrImportCancelled) {
		t.Fatalf("old import could complete re-added title: %v", err)
	}
	if err := s.FailImport(ctx, "movie", 769, newToken, "temporary failure"); err != nil {
		t.Fatal(err)
	}
	title, err := s.GetTitle(ctx, "movie", 769)
	if err != nil || title.ImportStatus != "failed" {
		t.Fatalf("failed status = %+v, err=%v", title, err)
	}
	state, retryToken, err := s.StartImport(ctx, req)
	if err != nil || state != ImportStarted || retryToken == newToken {
		t.Fatalf("retry = %q, distinct token=%v, err=%v", state, retryToken != newToken, err)
	}
	if err := s.CompleteImport(ctx, "movie", 769, retryToken, data); err != nil {
		t.Fatal(err)
	}
}

func TestSearchPeopleByPersonAndCredit(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	req := StartImportRequest{MediaType: "movie", TMDBID: 42, Name: "The Test Show"}
	_, token, err := s.StartImport(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 42, token, ImportData{Name: req.Name, Credits: []CreditInput{
		{Key: "cast", PersonTMDBID: 10, PersonName: "Alex Rivera", Category: "cast", CharacterName: "Detective Morgan"},
		{Key: "crew", PersonTMDBID: 20, PersonName: "Jamie Chen", Category: "crew", Department: "Writing", Job: "Screenplay"},
		{Key: "crew-2", PersonTMDBID: 20, PersonName: "Jamie Chen", Category: "crew", Department: "Writing", Job: "Story"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, secondToken, err := s.StartImport(ctx, StartImportRequest{MediaType: "movie", TMDBID: 43, Name: "Story Time"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 43, secondToken, ImportData{Name: "Story Time", Credits: []CreditInput{
		{Key: "editor", PersonTMDBID: 20, PersonName: "Jamie Chen", Category: "crew", Job: "Editor"},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		ids   []int64
	}{
		{"alex", []int64{10}},
		{"test show", nil},
		{"detective morgan", []int64{10}},
		{"screenplay", []int64{20}},
		{"writing", []int64{20}},
		{"missing", nil},
		{"  ", nil},
	} {
		people, err := s.SearchPeople(ctx, tc.query)
		if err != nil {
			t.Fatalf("search %q: %v", tc.query, err)
		}
		if len(people) != len(tc.ids) {
			t.Fatalf("search %q: got %v, want IDs %v", tc.query, people, tc.ids)
		}
		for i, id := range tc.ids {
			if people[i].TMDBID != id {
				t.Fatalf("search %q: got %v, want IDs %v", tc.query, people, tc.ids)
			}
		}
	}
	for _, tc := range []struct{ query, want, label string }{
		{"Alex Rivera", "", ""},
		{"jamie", "", ""},
		{"screenplay", "Screenplay", "Crew"},
		{"story", "Story", "Crew"},
		{"writing", "Writing", "Crew"},
		{"Alex Rivea", "", ""},
		{"Detectiv Morgan", "Detective Morgan", "Role"},
		{"writng", "Writing", "Crew"},
	} {
		people, err := s.SearchPeople(ctx, tc.query)
		if err != nil || len(people) != 1 || people[0].Roles != tc.want || people[0].MatchLabel != tc.label {
			t.Fatalf("search %q roles = %+v, want %q (%s), err=%v", tc.query, people, tc.want, tc.label, err)
		}
	}
	people, err := s.SearchPeople(ctx, "The Tst Show")
	if err != nil || len(people) != 0 {
		t.Fatalf("title query returned people = %+v, err=%v", people, err)
	}
	_, directToken, err := s.StartImport(ctx, StartImportRequest{MediaType: "movie", TMDBID: 44, Name: "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 44, directToken, ImportData{Name: "Alex"}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		ids   []int64
	}{
		{"alex", []int64{44, 42}},
		{"The Tst Show", []int64{42}},
		{"Alex Rivea", []int64{42}},
		{"Jamie Che", []int64{43, 42}},
		{"Stoy Time", []int64{43}},
	} {
		titles, err := s.SearchTitles(ctx, tc.query)
		if err != nil || len(titles) != len(tc.ids) {
			t.Fatalf("title search %q = %+v, err=%v", tc.query, titles, err)
		}
		for i, id := range tc.ids {
			if titles[i].TMDBID != id {
				t.Fatalf("title search %q = %+v, want IDs %v", tc.query, titles, tc.ids)
			}
		}
	}
}

func TestPeopleSearchDoesNotSkipRepeatedRoleLetters(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "movie", TMDBID: 45, Name: "Sample Film"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 45, token, ImportData{Name: "Sample Film", Credits: []CreditInput{
		{Key: "edie", PersonTMDBID: 1, PersonName: "Edie Falco", Category: "cast", CharacterName: "Lead"},
		{Key: "eddie", PersonTMDBID: 2, PersonName: "Theo James", Category: "cast", CharacterName: "Eddie"},
	}}); err != nil {
		t.Fatal(err)
	}
	people, err := s.SearchPeople(ctx, "edie")
	if err != nil || len(people) != 1 || people[0].Name != "Edie Falco" {
		t.Fatalf("people matching edie = %+v, err=%v", people, err)
	}
}
