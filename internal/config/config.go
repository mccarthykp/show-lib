package config

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ListenAddr   string
	DatabasePath string
	TMDBToken    string
	TMDBAPIKey   string
}

// Load reads process environment first, then a local .env for unset values.
// It never includes credential values in errors.
func Load() (Config, error) {
	dotEnv, err := readDotEnv(".env")
	if err != nil {
		return Config{}, fmt.Errorf("read .env: %w", err)
	}
	value := func(key, fallback string) string {
		if v, ok := os.LookupEnv(key); ok {
			return strings.TrimSpace(v)
		}
		if v, ok := dotEnv[key]; ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	cfg := Config{
		ListenAddr:   value("SHOWLIB_LISTEN_ADDR", "127.0.0.1:8080"),
		DatabasePath: value("SHOWLIB_DB_PATH", "data/showlib.sqlite"),
		TMDBToken:    value("TMDB_READ_ACCESS_TOKEN", ""),
		TMDBAPIKey:   value("TMDB_API_KEY", ""),
	}
	if cfg.TMDBToken == "" && cfg.TMDBAPIKey == "" {
		return Config{}, errors.New("missing TMDB credential: set TMDB_READ_ACCESS_TOKEN or TMDB_API_KEY in the environment or .env")
	}
	if cfg.DatabasePath == "" {
		return Config{}, errors.New("SHOWLIB_DB_PATH must not be empty")
	}
	host, port, err := net.SplitHostPort(cfg.ListenAddr)
	if err != nil || host == "" && port == "" {
		return Config{}, errors.New("SHOWLIB_LISTEN_ADDR must be a host:port address")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return Config{}, errors.New("SHOWLIB_LISTEN_ADDR must use a port from 1 to 65535")
	}
	return cfg, nil
}

func readDotEnv(path string) (map[string]string, error) {
	values := make(map[string]string)
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(raw), "\"'")
	}
	return values, scanner.Err()
}
