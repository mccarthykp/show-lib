package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGetMovieKeepsCredentialServerSide(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/movie/769" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected request path or authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":769,"title":"Goodfellas","release_date":"1990-09-12"}`))
	}))
	defer server.Close()
	client, err := New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	movie, err := client.GetMovie(context.Background(), 769)
	if err != nil || movie.Title != "Goodfellas" {
		t.Fatalf("movie = %+v, err = %v", movie, err)
	}
}

func TestRetriesRateLimitAndServerError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		switch attempts {
		case 1:
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"id":769,"title":"Goodfellas"}`))
		}
	}))
	defer server.Close()
	client, err := New("test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	movie, err := client.GetMovie(context.Background(), 769)
	if err != nil || movie.Title != "Goodfellas" || attempts != 3 {
		t.Fatalf("movie=%+v, attempts=%d, err=%v", movie, attempts, err)
	}
}

func TestRateLimitWaitRespectsCancellationAndHidesAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, err := New("", "private-api-key")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = client.GetMovie(ctx, 769)
	if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "private-api-key") {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestGetSeriesWithAPIKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tv/1398" || r.URL.Query().Get("api_key") != "test-api-key" {
			t.Errorf("unexpected request path or API key")
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("API key request unexpectedly included a bearer token")
		}
		_, _ = w.Write([]byte(`{"id":1398,"name":"The Sopranos","first_air_date":"1999-01-10"}`))
	}))
	defer server.Close()
	client, err := New("", "test-api-key")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	series, err := client.GetSeries(context.Background(), 1398)
	if err != nil || series.Name != "The Sopranos" {
		t.Fatalf("series = %+v, err = %v", series, err)
	}
}
