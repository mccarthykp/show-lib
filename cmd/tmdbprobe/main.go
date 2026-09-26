// tmdbprobe checks the TMDB fields needed for Show-lib's Phase 0 data mapping.
// It prints aggregate counts and a few sample credits, never the API token.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const apiBase = "https://api.themoviedb.org/3"

type client struct {
	http   *http.Client
	token  string
	apiKey string
}

type searchResult struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type credit struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
	Job       string `json:"job"`
}

type aggregateCredit struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Roles []struct {
		Character    string `json:"character"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"roles"`
	Jobs []struct {
		Job          string `json:"job"`
		EpisodeCount int    `json:"episode_count"`
	} `json:"jobs"`
}

type aggregateCredits struct {
	Cast []aggregateCredit `json:"cast"`
	Crew []aggregateCredit `json:"crew"`
}

type episode struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	SeasonNumber  int    `json:"season_number"`
	EpisodeNumber int    `json:"episode_number"`
}

type seasonDetails struct {
	Episodes []episode `json:"episodes"`
}

type episodeCredits struct {
	Cast       []credit `json:"cast"`
	Crew       []credit `json:"crew"`
	GuestStars []credit `json:"guest_stars"`
}

type movieCredits struct {
	Cast []credit `json:"cast"`
	Crew []credit `json:"crew"`
}

type episodeSummary struct {
	Season              int      `json:"season"`
	Episode             int      `json:"episode"`
	EpisodeID           int      `json:"episode_id"`
	CastCount           int      `json:"cast_count"`
	GuestStarCount      int      `json:"guest_star_count"`
	CrewCount           int      `json:"crew_count"`
	CastExamples        []string `json:"cast_examples,omitempty"`
	GuestStarExamples   []string `json:"guest_star_examples,omitempty"`
	CrewJobExamples     []string `json:"crew_job_examples,omitempty"`
	IDsAbsentFromRoster int      `json:"ids_absent_from_aggregate_roster"`
}

type seriesSummary struct {
	Title                        string           `json:"title"`
	TMDBID                       int              `json:"tmdb_id"`
	AggregateCastCount           int              `json:"aggregate_cast_count"`
	AggregateCrewCount           int              `json:"aggregate_crew_count"`
	AggregateRoleExamples        []string         `json:"aggregate_role_examples,omitempty"`
	AggregateJobExamples         []string         `json:"aggregate_job_examples,omitempty"`
	Episodes                     []episodeSummary `json:"episodes"`
	AggregateIDsInSample         int              `json:"aggregate_ids_seen_in_sampled_episodes"`
	AggregateIDsUnmappedInSample int              `json:"aggregate_ids_not_seen_in_sampled_episodes"`
	// These are sampled episodes only; unmapped here does not mean absent from the series.
}

type movieSummary struct {
	Title           string   `json:"title"`
	TMDBID          int      `json:"tmdb_id"`
	CastCount       int      `json:"cast_count"`
	CrewCount       int      `json:"crew_count"`
	CastExamples    []string `json:"cast_examples,omitempty"`
	CrewJobExamples []string `json:"crew_job_examples,omitempty"`
}

type report struct {
	GeneratedAtUTC                       string          `json:"generated_at_utc"`
	Method                               string          `json:"method"`
	Movie                                movieSummary    `json:"movie"`
	Series                               []seriesSummary `json:"series"`
	AggregateCrossSeriesPersonCount      int             `json:"aggregate_cross_series_person_count"`
	SampledEpisodeCrossSeriesPersonCount int             `json:"sampled_episode_cross_series_person_count"`
	Notes                                []string        `json:"notes"`
}

func main() {
	outPath := flag.String("out", "", "write a JSON report to this path instead of stdout")
	flag.Parse()
	if err := run(*outPath); err != nil {
		fmt.Fprintln(os.Stderr, "tmdbprobe:", err)
		os.Exit(1)
	}
}

func run(outPath string) error {
	token := os.Getenv("TMDB_READ_ACCESS_TOKEN")
	apiKey := os.Getenv("TMDB_API_KEY")
	if token == "" {
		var err error
		token, err = valueFromDotEnv(".env", "TMDB_READ_ACCESS_TOKEN")
		if err != nil {
			return err
		}
	}
	if token == "" && apiKey == "" {
		var err error
		apiKey, err = valueFromDotEnv(".env", "TMDB_API_KEY")
		if err != nil {
			return err
		}
	}
	if token == "" && apiKey == "" {
		return errors.New("set TMDB_READ_ACCESS_TOKEN or TMDB_API_KEY in the environment or local .env")
	}
	c := client{http: &http.Client{Timeout: 20 * time.Second}, token: token, apiKey: apiKey}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	movie, err := c.probeMovie(ctx, "Goodfellas")
	if err != nil {
		return err
	}
	seriesNames := []string{"The Sopranos", "Nurse Jackie"}
	r := report{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Method:         "TMDB API v3: movie credits, TV aggregate credits, season details, and credits for the first two episodes of seasons 1 and 2 where available",
		Movie:          movie,
		Notes: []string{
			"Episode samples validate field presence; they do not establish complete series coverage.",
			"An aggregate role or episode count does not identify the episodes in which the credit occurs.",
			"A missing episode credit is unknown, not proof of absence.",
		},
	}
	aggregateSets := make([]map[int]bool, 0, len(seriesNames))
	episodeSets := make([]map[int]bool, 0, len(seriesNames))
	for _, name := range seriesNames {
		s, aggregateIDs, sampledIDs, err := c.probeSeries(ctx, name)
		if err != nil {
			return err
		}
		r.Series = append(r.Series, s)
		aggregateSets = append(aggregateSets, aggregateIDs)
		episodeSets = append(episodeSets, sampledIDs)
	}
	r.AggregateCrossSeriesPersonCount = intersectionCount(aggregateSets[0], aggregateSets[1])
	r.SampledEpisodeCrossSeriesPersonCount = intersectionCount(episodeSets[0], episodeSets[1])
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if outPath == "" {
		_, err = os.Stdout.Write(data)
		return err
	}
	return os.WriteFile(outPath, data, 0o644)
}

func (c client) probeMovie(ctx context.Context, title string) (movieSummary, error) {
	id, err := c.findExact(ctx, "movie", title)
	if err != nil {
		return movieSummary{}, err
	}
	var credits movieCredits
	if err := c.get(ctx, fmt.Sprintf("/movie/%d/credits", id), nil, &credits); err != nil {
		return movieSummary{}, err
	}
	return movieSummary{
		Title: title, TMDBID: id, CastCount: len(credits.Cast), CrewCount: len(credits.Crew),
		CastExamples: castExamples(credits.Cast), CrewJobExamples: crewExamples(credits.Crew),
	}, nil
}

func (c client) probeSeries(ctx context.Context, title string) (seriesSummary, map[int]bool, map[int]bool, error) {
	id, err := c.findExact(ctx, "tv", title)
	if err != nil {
		return seriesSummary{}, nil, nil, err
	}
	var agg aggregateCredits
	if err := c.get(ctx, fmt.Sprintf("/tv/%d/aggregate_credits", id), nil, &agg); err != nil {
		return seriesSummary{}, nil, nil, err
	}
	s := seriesSummary{Title: title, TMDBID: id, AggregateCastCount: len(agg.Cast), AggregateCrewCount: len(agg.Crew)}
	aggregateIDs := make(map[int]bool)
	for _, person := range agg.Cast {
		aggregateIDs[person.ID] = true
		if len(s.AggregateRoleExamples) < 3 && len(person.Roles) > 0 {
			s.AggregateRoleExamples = append(s.AggregateRoleExamples, person.Name+": "+person.Roles[0].Character)
		}
	}
	for _, person := range agg.Crew {
		aggregateIDs[person.ID] = true
		if len(s.AggregateJobExamples) < 3 && len(person.Jobs) > 0 {
			s.AggregateJobExamples = append(s.AggregateJobExamples, person.Name+": "+person.Jobs[0].Job)
		}
	}
	sampledIDs := make(map[int]bool)
	for _, seasonNumber := range []int{1, 2} {
		var season seasonDetails
		path := fmt.Sprintf("/tv/%d/season/%d", id, seasonNumber)
		if err := c.get(ctx, path, nil, &season); err != nil {
			return seriesSummary{}, nil, nil, err
		}
		sort.Slice(season.Episodes, func(i, j int) bool {
			return season.Episodes[i].EpisodeNumber < season.Episodes[j].EpisodeNumber
		})
		for i, ep := range season.Episodes {
			if i >= 2 {
				break
			}
			var credits episodeCredits
			path := fmt.Sprintf("/tv/%d/season/%d/episode/%d/credits", id, seasonNumber, ep.EpisodeNumber)
			if err := c.get(ctx, path, nil, &credits); err != nil {
				return seriesSummary{}, nil, nil, err
			}
			es := episodeSummary{
				Season: seasonNumber, Episode: ep.EpisodeNumber, EpisodeID: ep.ID,
				CastCount: len(credits.Cast), GuestStarCount: len(credits.GuestStars), CrewCount: len(credits.Crew),
				CastExamples: castExamples(credits.Cast), GuestStarExamples: castExamples(credits.GuestStars),
				CrewJobExamples: crewExamples(credits.Crew),
			}
			for _, person := range append(append(credits.Cast, credits.GuestStars...), credits.Crew...) {
				sampledIDs[person.ID] = true
				if !aggregateIDs[person.ID] {
					es.IDsAbsentFromRoster++
				}
			}
			s.Episodes = append(s.Episodes, es)
		}
	}
	for id := range aggregateIDs {
		if sampledIDs[id] {
			s.AggregateIDsInSample++
		} else {
			s.AggregateIDsUnmappedInSample++
		}
	}
	return s, aggregateIDs, sampledIDs, nil
}

func (c client) findExact(ctx context.Context, mediaType, title string) (int, error) {
	var result searchResponse
	if err := c.get(ctx, "/search/"+mediaType, url.Values{"query": {title}}, &result); err != nil {
		return 0, err
	}
	for _, item := range result.Results {
		if strings.EqualFold(item.Name, title) || strings.EqualFold(item.Title, title) {
			return item.ID, nil
		}
	}
	return 0, fmt.Errorf("TMDB search found no exact %s match for %q", mediaType, title)
}

func (c client) get(ctx context.Context, path string, query url.Values, target any) error {
	if query == nil {
		query = url.Values{}
	}
	if c.token == "" {
		query.Set("api_key", c.apiKey)
	}
	u := apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err // The URL may contain an API key; never include it in errors.
		}
		return fmt.Errorf("TMDB request %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB request %s returned HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 10<<20)).Decode(target)
}

func valueFromDotEnv(path, targetKey string) (string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != targetKey {
			continue
		}
		value = strings.TrimSpace(value)
		return strings.Trim(value, "\"'"), nil
	}
	return "", scanner.Err()
}

func castExamples(items []credit) []string {
	var result []string
	for _, item := range items {
		if len(result) >= 3 {
			break
		}
		result = append(result, item.Name+": "+item.Character)
	}
	return result
}

func crewExamples(items []credit) []string {
	var result []string
	for _, item := range items {
		if len(result) >= 3 {
			break
		}
		result = append(result, item.Name+": "+item.Job)
	}
	return result
}

func intersectionCount(a, b map[int]bool) int {
	count := 0
	for id := range a {
		if b[id] {
			count++
		}
	}
	return count
}
