package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestLibrarySnapshotRequiresEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO titles(media_type, tmdb_id, name, import_status, watched)
		VALUES ('movie', 769, 'Goodfellas', 'complete', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyLibrarySnapshot(ctx); err == nil {
		t.Fatal("library snapshot replaced a populated database")
	}
	title, err := s.GetTitle(ctx, "movie", 769)
	if err != nil || title.Name != "Goodfellas" {
		t.Fatalf("saved title changed: %+v, %v", title, err)
	}
	var applied int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&applied); err != nil || applied != 0 {
		t.Fatalf("library migration recorded after refusal: %d, %v", applied, err)
	}
}

func TestLibrarySnapshotRestoresFullLibrary(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if titles, err := s.ListTitles(ctx); err != nil || len(titles) != 0 {
		t.Fatalf("fresh library is not empty: %d titles, %v", len(titles), err)
	}
	count, err := s.ApplyLibrarySnapshot(ctx)
	if err != nil || count != 185 {
		t.Fatalf("library snapshot = %d titles, %v", count, err)
	}
	if _, err := s.ApplyLibrarySnapshot(ctx); err == nil {
		t.Fatal("library snapshot ran twice")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	titles, err := s.ListTitles(ctx)
	if err != nil || len(titles) != 185 {
		t.Fatalf("reopened library = %d titles, %v", len(titles), err)
	}
	for _, tc := range []struct {
		id      int64
		season  int64
		episode int64
	}{{1398, 6, 21}, {18053, 7, 12}} {
		title, err := s.GetTitle(ctx, "tv", tc.id)
		if err != nil || !title.Watched || title.WatchedSeason.Int64 != tc.season || title.WatchedEpisode.Int64 != tc.episode {
			t.Fatalf("restored series %d: %+v, %v", tc.id, title, err)
		}
	}
	for table, want := range map[string]int{"people": 38093, "credits": 52376, "episodes": 5679, "episode_person_credits": 129776} {
		var got int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&got); err != nil || got != want {
			t.Fatalf("restored %s = %d, want %d, err = %v", table, got, want, err)
		}
	}
	rows, err := s.db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("restored library has foreign-key violations")
	}
}
