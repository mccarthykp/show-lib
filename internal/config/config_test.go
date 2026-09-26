package config

import (
	"strings"
	"testing"
)

func TestLoadRequiresCredentialWithoutLeakingIt(t *testing.T) {
	t.Setenv("TMDB_READ_ACCESS_TOKEN", "")
	t.Setenv("TMDB_API_KEY", "")
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "missing TMDB credential") {
		t.Fatalf("expected missing credential error, got %v", err)
	}
	t.Setenv("TMDB_API_KEY", "private-test-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TMDBAPIKey != "private-test-key" || cfg.ListenAddr != "127.0.0.1:8080" {
		t.Fatalf("unexpected config values: address=%q key_set=%v", cfg.ListenAddr, cfg.TMDBAPIKey != "")
	}
}
