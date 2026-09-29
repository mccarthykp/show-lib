package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestWatchlistPersistsMetadataWithoutJoiningWatchedLibrary(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, filepath.Join(t.TempDir(), "watchlist.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	title := WatchlistTitle{MediaType: "movie", TMDBID: 42, Name: "A Film",
		ReleaseYear: sql.NullInt64{Int64: 2024, Valid: true},
		ReleaseDate: sql.NullString{String: "2024-03-02", Valid: true},
		PosterPath:  sql.NullString{String: "/poster.jpg", Valid: true}, Overview: "An overview."}
	if err := s.AddToWatchlist(ctx, title); err != nil {
		t.Fatal(err)
	}
	titles, err := s.ListWatchlist(ctx)
	if err != nil || len(titles) != 1 || titles[0].Name != "A Film" || titles[0].Overview != "An overview." ||
		!titles[0].ReleaseYear.Valid || titles[0].ReleaseYear.Int64 != 2024 {
		t.Fatalf("watchlist = %+v, err = %v", titles, err)
	}
	if _, err := s.GetTitle(ctx, "movie", 42); err != sql.ErrNoRows {
		t.Fatalf("watchlist title entered watched library: %v", err)
	}

	_, token, err := s.StartImport(ctx, StartImportRequest{MediaType: "movie", TMDBID: 42, Name: "A Film"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteImport(ctx, "movie", 42, token, ImportData{Name: "A Film"}); err != nil {
		t.Fatal(err)
	}
	titles, err = s.ListWatchlist(ctx)
	if err != nil || len(titles) != 0 {
		t.Fatalf("watched title remains visible in watchlist: %+v, %v", titles, err)
	}
	if err := s.RemoveFromWatchlist(ctx, "movie", 42); err != nil {
		t.Fatal(err)
	}
}
