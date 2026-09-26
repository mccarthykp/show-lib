package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"show-lib/internal/config"
	"show-lib/internal/store"
	"show-lib/internal/tmdb"
	"show-lib/internal/web"
)

func main() {
	var err error
	if len(os.Args) == 2 && os.Args[1] == "migrate-library" {
		err = migrateLibrary()
	} else if len(os.Args) == 1 {
		err = run()
	} else {
		err = errors.New("usage: showlib [migrate-library]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "show-lib:", err)
		os.Exit(1)
	}
}

func migrateLibrary() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	db, err := store.OpenForLibraryMigration(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()
	titles, err := db.ApplyLibrarySnapshot(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("Library migration 2 complete: %d titles restored.\n", titles)
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := store.Open(ctx, cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer db.Close()
	tmdbClient, err := tmdb.New(cfg.TMDBToken, cfg.TMDBAPIKey)
	if err != nil {
		return fmt.Errorf("TMDB client: %w", err)
	}
	server := &http.Server{
		Addr: cfg.ListenAddr, Handler: web.New(db, tmdbClient),
		ReadHeaderTimeout: 5 * time.Second,
	}
	stopCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go backfillTitleMetadata(stopCtx, db, tmdbClient)
	go func() {
		<-stopCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: %v", err)
		}
	}()
	log.Printf("listening on %s", cfg.ListenAddr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func backfillTitleMetadata(ctx context.Context, db *store.Store, client *tmdb.Client) {
	titles, err := db.MissingTitleMetadata(ctx)
	if err != nil {
		log.Printf("list titles needing metadata: %v", err)
		return
	}
	for _, title := range titles {
		if ctx.Err() != nil {
			return
		}
		var path, overview string
		if title.MediaType == "movie" {
			movie, err := client.GetMovie(ctx, title.TMDBID)
			if err != nil {
				log.Printf("load metadata for movie:%d: %v", title.TMDBID, err)
				continue
			}
			path = movie.PosterPath
			overview = movie.Overview
		} else {
			series, err := client.GetSeries(ctx, title.TMDBID)
			if err != nil {
				log.Printf("load metadata for tv:%d: %v", title.TMDBID, err)
				continue
			}
			path = series.PosterPath
			overview = series.Overview
		}
		if err := db.SetTitleMetadata(ctx, title.MediaType, title.TMDBID, path, overview); err != nil {
			log.Printf("save metadata for %s:%d: %v", title.MediaType, title.TMDBID, err)
		}
	}
}
