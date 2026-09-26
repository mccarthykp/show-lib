# Phase 3 — Accurate series progress

**Status:** Complete. The local Docker service is available at `http://127.0.0.1:8080`.

## Delivered

- TV imports cache episode cast, guest cast, and crew credits for every regular season. The cache preserves season and episode names, air dates, character names, and crew jobs. Episode credit fetching runs in the background and can be retried after a failure. Previously imported series are fetched when their title or person page is opened.
- Title credits distinguish a confirmed watched appearance from an appearance only in later episodes and a credit whose episode placement is unknown. Episode summaries expand to show all known episodes. Person-page rows show credited years and episode counts only within the watched range; each credited season expands separately to show its watched episodes.
- A TV title's **Update progress** page uses cached episode choices. Saving a new marker and its episode ID is one database transaction. Advancing and rolling back immediately recompute watched-range eligibility from the marker without changing the cached full-series history.
- Season 0 specials never become eligible by ordinal inference. A selected special is validated with TMDB season details and episode credits, then saved with the progress marker in one transaction. Only that exact special episode ID counts as watched. If TMDB is unavailable or the selected episode is invalid, the prior marker remains intact.
- Episode refresh replaces stale episode-only credits after a complete fetch. A failed refresh leaves the previous episode cache intact and blocks progress updates until retry succeeds.

## Exit-gate verification

- `go test ./...` and `go vet ./...` passed. Store tests cover later-to-watched eligibility on advance, rollback, unknown aggregate-only credits, invalid updates, failed import retry, selected-special exact matching, and transaction rollback on invalid special credit data. Web tests cover the progress form and update route.
- The rebuilt local service rendered *The Sopranos* progress form with seasons 1–6 and the saved marker at season 6, episode 21. Its title page rendered watched-credit labels and expandable episode summaries. Edie Falco's person-page summary now changes with the saved watch marker.

## Next phase

Phase 4 will use these watched-range statuses to show connections between saved titles and provide more ways to browse long title and people lists.
