package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenMigratesAndPersistsMultipleCredits(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "data", "showlib.sqlite")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	var migrationCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 4 {
		t.Fatalf("migration count = %d, want 4", migrationCount)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO titles(media_type, tmdb_id, name, import_status, watched)
		VALUES ('tv', 1398, 'The Sopranos', 'complete', 1)`)
	if err != nil {
		t.Fatal(err)
	}
	titleID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO people(tmdb_id, name) VALUES (1, 'A Person')"); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"Role A", "Role B"} {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO credits(title_id, person_tmdb_id, credit_key, category, character_name)
			VALUES (?, 1, ?, 'cast', ?)`, titleID, role, role); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO credits(title_id, person_tmdb_id, credit_key, category)
		VALUES (?, 999, 'missing-person', 'cast')`, titleID); err == nil {
		t.Fatal("foreign key constraint did not reject missing person")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	title, err := s.GetTitle(ctx, "tv", 1398)
	if err != nil || title.Name != "The Sopranos" {
		t.Fatalf("reopened title = %+v, err = %v", title, err)
	}
	missing, err := s.MissingTitleMetadata(ctx)
	if err != nil || len(missing) != 1 || missing[0].TMDBID != 1398 {
		t.Fatalf("missing metadata = %+v, err = %v", missing, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE titles SET poster_path = '/existing.jpg' WHERE tmdb_id = 1398`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitleMetadata(ctx, "tv", 1398, "/replacement.jpg", "A crime family story."); err != nil {
		t.Fatal(err)
	}
	title, err = s.GetTitle(ctx, "tv", 1398)
	if err != nil || title.PosterPath.String != "/existing.jpg" || title.Overview.String != "A crime family story." {
		t.Fatalf("backfilled metadata = %+v, err = %v", title, err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE titles SET watched_season = 6, watched_episode = 21,
		watched_episode_tmdb_id = 1234 WHERE media_type = 'tv' AND tmdb_id = 1398`); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTitleMetadata(ctx, "tv", 1398, "/new.jpg", "New overview"); err != nil {
		t.Fatal(err)
	}
	title, err = s.GetTitle(ctx, "tv", 1398)
	if err != nil || !title.Watched || title.WatchedSeason.Int64 != 6 || title.WatchedEpisode.Int64 != 21 ||
		title.PosterPath.String != "/existing.jpg" || title.Overview.String != "A crime family story." {
		t.Fatalf("metadata refresh changed watched title = %+v, err = %v", title, err)
	}
	missing, err = s.MissingTitleMetadata(ctx)
	if err != nil || len(missing) != 0 {
		t.Fatalf("remaining metadata = %+v, err = %v", missing, err)
	}
	var creditCount int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM credits WHERE title_id = ?", titleID).Scan(&creditCount); err != nil {
		t.Fatal(err)
	}
	if creditCount != 2 {
		t.Fatalf("credit count = %d, want 2 distinct roles", creditCount)
	}
	titles, err := s.ListTitles(ctx)
	if err != nil || len(titles) != 1 {
		t.Fatalf("listed titles = %d, err = %v", len(titles), err)
	}
	people, err := s.ListPeople(ctx)
	if err != nil || len(people) != 1 {
		t.Fatalf("listed people = %d, err = %v", len(people), err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO titles(media_type, tmdb_id, name, import_status, watched)
		VALUES ('movie', 769, 'Goodfellas', 'importing', 1)`); err != nil {
		t.Fatal(err)
	}
	titles, err = s.ListTitles(ctx)
	if err != nil || len(titles) != 1 {
		t.Fatalf("listed titles with incomplete import = %d, err = %v", len(titles), err)
	}
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if migrationCount != 4 {
		t.Fatalf("reopened migration count = %d, want 4", migrationCount)
	}
	if _, err := s.GetPerson(ctx, 999); err != sql.ErrNoRows {
		t.Fatalf("missing person error = %v, want sql.ErrNoRows", err)
	}
}

func TestLegacyLibraryUpgradePreservesSavedTitle(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.ExecContext(ctx, `
		CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
		INSERT INTO schema_migrations(version) VALUES (1);
		CREATE TABLE titles (id INTEGER PRIMARY KEY, media_type TEXT, tmdb_id INTEGER, name TEXT,
			release_year INTEGER, release_date TEXT, import_status TEXT, import_error TEXT, watched INTEGER,
			watched_season INTEGER, watched_episode INTEGER, watched_episode_tmdb_id INTEGER,
			episode_sync_status TEXT, episode_sync_error TEXT, updated_at TEXT);
		CREATE TABLE people (tmdb_id INTEGER PRIMARY KEY, name TEXT);
		INSERT INTO titles(media_type, tmdb_id, name, import_status, watched, episode_sync_status)
			VALUES ('tv', 1398, 'The Sopranos', 'complete', 1, 'pending');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	title, err := s.GetTitle(ctx, "tv", 1398)
	if err != nil || title.Name != "The Sopranos" || !title.Watched {
		t.Fatalf("upgraded title = %+v, err = %v", title, err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations").Scan(&count); err != nil || count != 4 {
		t.Fatalf("migration count = %d, err = %v", count, err)
	}
}
