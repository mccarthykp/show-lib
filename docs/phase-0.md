# Phase 0 — TMDB data and access validation

**Status:** Complete for the MVP data model. Live probe run: 2026-09-24 UTC. **Implementation language:** Go.

The local TMDB API key worked with API v3. The probe used the key from the ignored `.env` file and did not include it in the report. The probe and its output are [cmd/tmdbprobe/main.go](../cmd/tmdbprobe/main.go) and [tmdb-phase0-results.json](tmdb-phase0-results.json).

## Live findings

| Title | TMDB ID | Aggregate/movie cast | Aggregate/movie crew | Episode samples |
| --- | ---: | ---: | ---: | --- |
| *Goodfellas* | `movie:769` | 142 | 86 | Not applicable |
| *The Sopranos* | `tv:1398` | 807 | 91 | S1E1, S1E2, S2E1, S2E2 |
| *Nurse Jackie* | `tv:18053` | 482 | 57 | S1E1, S1E2, S2E1, S2E2 |

Both series returned cast, guest stars, and crew for each sampled episode. For example, *The Sopranos* S1E1 returned 11 cast, 18 guest stars, and 8 crew entries; *Nurse Jackie* S1E1 returned 6 cast, 18 guest stars, and 3 crew entries. The series aggregate rosters shared 29 TMDB person IDs. The sampled episodes shared two person IDs, demonstrating that episode-scoped relationships are possible. Edie Falco appears in the episode-credit examples for both shows with different character names.

The sample checked only eight episodes, so its counts do not describe complete series coverage. Every sampled episode credit's person ID was present in that show's aggregate roster. This is useful evidence for merging aggregate and episode data, but it is not a guarantee for all TMDB titles.

## Field mapping for implementation

| Show-lib field or rule | TMDB source | Mapping |
| --- | --- | --- |
| Movie/series identity | Search and title details | Store `media_type` plus numeric `id`; a movie ID and TV ID are different identities. |
| Person identity | Cast/crew credit | Store numeric person `id`; use it to join credits across library titles. |
| Movie cast and crew | `GET /movie/{id}/credits` | Preserve each cast `character` and crew `department`/`job`, plus source order when present. |
| Full available series roster | `GET /tv/{id}/aggregate_credits` | Include every returned cast `roles[]` and crew `jobs[]` item; do not use the latest-season `/credits` endpoint for this roster. |
| Season/episode choices | `GET /tv/{id}/season/{season_number}` | Use each episode's numeric `id`, `season_number`, `episode_number`, and `name` to validate the watch marker. |
| Watched-range credit | `GET /tv/{id}/season/{season_number}/episode/{episode_number}/credits` | Merge `cast`, `guest_stars`, and `crew` by person ID and episode; retain distinct characters/jobs. Only a returned episode credit establishes watched-range eligibility. |
| Role on a connected title | Movie or episode credit | Show the character or crew job from that title, not a generic profession or role from another title. |

The Go probe uses [movie credits](https://developer.themoviedb.org/reference/movie-credits), [series aggregate credits](https://developer.themoviedb.org/reference/tv-series-aggregate-credits), [season details](https://developer.themoviedb.org/reference/tv-season-details), and [episode credits](https://developer.themoviedb.org/reference/tv-episode-credits). TMDB says the ordinary [series credits](https://developer.themoviedb.org/reference/tv-series-credits) endpoint represents the latest season, so the implementation must use aggregate credits for the full roster.

## Rules and remaining data limits

- Aggregate `roles[]` and `jobs[]` include episode counts, but those counts do not identify episode numbers. They cannot by themselves establish whether a credit is within a user's watched range.
- A person missing from one episode payload is **unknown**, not proven absent. The UI can say an appearance is confirmed when an episode credit exists; it must not claim absence from missing data.
- The complete cast/crew requirement means *all credits returned by TMDB*, with no app-imposed top-billed cutoff. TMDB itself can have incomplete community-supplied data.
- Cast, guest-star, and crew arrays can refer to the same person in different contexts. Preserve multiple roles/jobs; deduplicate only repeated identities or identical credit facts.
- A series with many episodes requires many episode-credit calls. Fetch up to the watched marker, reuse stored results when progressing forward, and respect TMDB `429` responses. The probe deliberately sampled four episodes per series rather than bulk-fetching every episode.
- Specials in season 0 need an explicit watch marker decision; the implementation must not silently count them as watched.

**Phase 0 exit gate:** Passed for the proposed data model and representative titles. The app can build a full TMDB-available roster and evidence-based watched-range connections from these endpoints. Episode coverage must continue to be treated as fallible in the UI, as specified above.

## Reproduce the probe

1. Copy `.env.example` to `.env` and set either `TMDB_API_KEY` or `TMDB_READ_ACCESS_TOKEN`. Keep `.env` local and private.
2. Run `go run ./cmd/tmdbprobe -out docs/tmdb-phase0-results.json` from the repository root. It can also read the credential from the process environment.
3. Review the JSON counts and examples. The report contains title/person credit data, not the credential.

The repository's `.gitignore` excludes `.env`, local database/storage, Go build output, and local Docker overrides. `.dockerignore` keeps credentials and local storage out of future Docker build contexts. Keep `.env.example`, `go.mod`, application source, Dockerfiles, and normal Compose files tracked.
