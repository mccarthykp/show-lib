# Phase 2 — Add and manage titles

**Status:** Complete. The running Docker service is available at `http://127.0.0.1:8080`.

## Delivered

- The Add page searches TMDB movies and TV series by title, shows type and year for disambiguation, and pages through results.
- The preview page confirms the selected title. A TV series requires a valid season and last watched episode from TMDB season details before it can be added.
- Imports run in the background. Movies use TMDB movie credits; series use full series aggregate credits. Distinct cast roles and crew jobs are preserved. The title page shows importing, completed, or failed status and offers retry after failure.
- The watched library shows only completed imports. Repeated additions do not create duplicate titles or credits. Removal deletes that title and its credits, while people credited on other saved titles remain available. A removed title can be added again.
- TMDB attribution is documented at the end of the project README, using an approved logo from the [TMDB logos and attribution page](https://www.themoviedb.org/about/logos-attribution).

## Exit-gate verification

- `go test ./...` and `go vet ./...` passed. Store tests cover duplicate imports, distinct roles, failed imports, stale import tokens, re-addition, and shared-person retention. Titles are selected from TMDB search results.
- Live TMDB tests in an isolated database imported *GoodFellas* (228 credit entries) and *The Sopranos* (969 aggregate credit entries), including its season 1 episode 1 progress marker. The title and person pages rendered the imported credits. Duplicate add, invalid episode, removal, re-add, and retry of a failed import were exercised through HTTP routes.
- The rebuilt Docker service applied migration 2, became healthy, and served the Add page. Both sample titles were added through the app, remained browsable after a container restart, and were removed. A movie was re-added and removed again; a final restart left the library empty.

## Next phase

Phase 3 will fetch episode credits, distinguish watched-range evidence from unknown or future appearances, and make series progress editable. Aggregate series credits in Phase 2 represent the full credited roster; they do not yet establish that a person appeared within the saved watched range.
