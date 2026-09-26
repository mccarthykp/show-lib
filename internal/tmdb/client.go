package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.themoviedb.org/3"

type Client struct {
	http    *http.Client
	baseURL string
	token   string
	apiKey  string
}

type Movie struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	ReleaseDate string `json:"release_date"`
	PosterPath  string `json:"poster_path"`
	Overview    string `json:"overview"`
}

type Season struct {
	SeasonNumber int    `json:"season_number"`
	EpisodeCount int    `json:"episode_count"`
	Name         string `json:"name"`
}

type Series struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	FirstAirDate string   `json:"first_air_date"`
	PosterPath   string   `json:"poster_path"`
	Overview     string   `json:"overview"`
	Seasons      []Season `json:"seasons"`
}

type SearchItem struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	PosterPath   string `json:"poster_path"`
}

type SearchResults struct {
	Page       int          `json:"page"`
	TotalPages int          `json:"total_pages"`
	Results    []SearchItem `json:"results"`
}

type MovieCredits struct {
	Cast []CastCredit `json:"cast"`
	Crew []CrewCredit `json:"crew"`
}

type CastCredit struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	CreditID    string `json:"credit_id"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
}

type CrewCredit struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	CreditID    string `json:"credit_id"`
	Department  string `json:"department"`
	Job         string `json:"job"`
}

type AggregateCredits struct {
	Cast []AggregateCast `json:"cast"`
	Crew []AggregateCrew `json:"crew"`
}

type AggregateCast struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Order       int    `json:"order"`
	Roles       []struct {
		CreditID  string `json:"credit_id"`
		Character string `json:"character"`
	} `json:"roles"`
}

type AggregateCrew struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ProfilePath string `json:"profile_path"`
	Department  string `json:"department"`
	Jobs        []struct {
		CreditID string `json:"credit_id"`
		Job      string `json:"job"`
	} `json:"jobs"`
}

type Episode struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	AirDate       string `json:"air_date"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
}

type SeasonDetails struct {
	Name     string    `json:"name"`
	Episodes []Episode `json:"episodes"`
}

type EpisodeCredits struct {
	Cast       []CastCredit `json:"cast"`
	GuestStars []CastCredit `json:"guest_stars"`
	Crew       []CrewCredit `json:"crew"`
}

type Person struct {
	ID          int64  `json:"id"`
	ProfilePath string `json:"profile_path"`
}

func New(token, apiKey string) (*Client, error) {
	if strings.TrimSpace(token) == "" && strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("TMDB credential is required")
	}
	return &Client{
		http: &http.Client{Timeout: 15 * time.Second}, baseURL: defaultBaseURL,
		token: strings.TrimSpace(token), apiKey: strings.TrimSpace(apiKey),
	}, nil
}

func (c *Client) GetMovie(ctx context.Context, id int64) (Movie, error) {
	if id <= 0 {
		return Movie{}, errors.New("TMDB movie ID must be positive")
	}
	var movie Movie
	err := c.get(ctx, "/movie/"+strconv.FormatInt(id, 10), &movie)
	return movie, err
}

func (c *Client) GetSeries(ctx context.Context, id int64) (Series, error) {
	if id <= 0 {
		return Series{}, errors.New("TMDB series ID must be positive")
	}
	var series Series
	err := c.get(ctx, "/tv/"+strconv.FormatInt(id, 10), &series)
	return series, err
}

func (c *Client) SearchMovies(ctx context.Context, query string, page int) (SearchResults, error) {
	return c.search(ctx, "movie", query, page)
}

func (c *Client) SearchSeries(ctx context.Context, query string, page int) (SearchResults, error) {
	return c.search(ctx, "tv", query, page)
}

func (c *Client) search(ctx context.Context, mediaType, query string, page int) (SearchResults, error) {
	if strings.TrimSpace(query) == "" || page < 1 {
		return SearchResults{}, errors.New("search requires a query and positive page")
	}
	var results SearchResults
	params := url.Values{"query": {query}, "page": {strconv.Itoa(page)}}
	err := c.getQuery(ctx, "/search/"+mediaType, params, &results)
	return results, err
}

func (c *Client) GetMovieCredits(ctx context.Context, id int64) (MovieCredits, error) {
	if id <= 0 {
		return MovieCredits{}, errors.New("TMDB movie ID must be positive")
	}
	var credits MovieCredits
	err := c.get(ctx, "/movie/"+strconv.FormatInt(id, 10)+"/credits", &credits)
	return credits, err
}

func (c *Client) GetPerson(ctx context.Context, id int64) (Person, error) {
	if id <= 0 {
		return Person{}, errors.New("TMDB person ID must be positive")
	}
	var person Person
	err := c.get(ctx, "/person/"+strconv.FormatInt(id, 10), &person)
	return person, err
}

func (c *Client) GetSeriesAggregateCredits(ctx context.Context, id int64) (AggregateCredits, error) {
	if id <= 0 {
		return AggregateCredits{}, errors.New("TMDB series ID must be positive")
	}
	var credits AggregateCredits
	err := c.get(ctx, "/tv/"+strconv.FormatInt(id, 10)+"/aggregate_credits", &credits)
	return credits, err
}

func (c *Client) GetSeason(ctx context.Context, id int64, season int) (SeasonDetails, error) {
	if id <= 0 || season < 0 {
		return SeasonDetails{}, errors.New("invalid TMDB season request")
	}
	var details SeasonDetails
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", id, season), &details)
	return details, err
}

func (c *Client) GetEpisodeCredits(ctx context.Context, id int64, season, episode int) (EpisodeCredits, error) {
	if id <= 0 || season < 0 || episode <= 0 {
		return EpisodeCredits{}, errors.New("invalid TMDB episode credits request")
	}
	var credits EpisodeCredits
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d/episode/%d/credits", id, season, episode), &credits)
	return credits, err
}

func (c *Client) get(ctx context.Context, path string, dst any) error {
	return c.getQuery(ctx, path, nil, dst)
}

func (c *Client) getQuery(ctx context.Context, path string, query url.Values, dst any) error {
	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return errors.New("invalid TMDB endpoint")
	}
	if query == nil {
		query = url.Values{}
	}
	if c.token == "" {
		query.Set("api_key", c.apiKey)
	}
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return errors.New("create TMDB request")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // Strip the URL, which may contain an API key.
		}
		return fmt.Errorf("TMDB request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB returned HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(dst); err != nil {
		return fmt.Errorf("decode TMDB response: %w", err)
	}
	return nil
}
