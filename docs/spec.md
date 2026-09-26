# Show-lib MVP specification

## Purpose

Show-lib is a private, browser-based library of movies and TV series the user has watched. Its main job is to reveal when a cast or crew member connects two or more titles in that library. A connection includes the person's credit on each title, with season and episode details when TMDB provides them.

This specification is based on [README.md](../README.md). The data source is The Movie Database (TMDB) API. A TMDB API Read Access Token or API key must be configured for the app.

The application and its TMDB integration will be written in Go. See [Phase 0 findings](phase-0.md) for the validated TMDB field mapping and sample results.

## MVP outcome

A user can search TMDB for a movie or series, add it to the watched library, and open its detail page. When adding a series, the user records the last season and episode watched. Show-lib imports the title, all cast and crew credits available from TMDB, and people. The title page identifies cast or crew members who connect it to another watched title **within the user's watched range**, links to those titles and people, and shows the person's role on each title. A person page shows every library title on which they have a credit, clearly distinguishing credits confirmed within the watched range from those whose episode placement is later or unknown. Advancing or correcting series progress recalculates the visible connections; removing a title removes its connections.

The relationship is derived from shared TMDB person IDs and credits; it does not require a manually maintained graph or a graph visualization.

## Users and access

- The MVP has one user and one shared library. Separate profiles and per-user progress are outside scope.
- During development, the Dockerized app is accessed through localhost. Home-server and Tailscale routing details will be supplied after development; deployment configuration must make the host binding easy to change.
- The app must remain useful with its already imported library if TMDB is temporarily unavailable; searching/importing new titles may then show an error.

## Functional requirements

### 1. Find and add a watched title

1. The library has an **Add title** control. A title search covers TMDB movies and TV series together, and results show type and release year for disambiguation. Titles are added by selecting a search result. Movie and TV titles with the same numeric TMDB ID remain distinct internally.
2. Results eligible for the MVP are movies and TV series. Search results show enough information to select the intended title, including title type and release year. A green check mark identifies titles already in the watched library.
3. Selecting a movie and confirming **Add to watched** creates one watched-library entry keyed by TMDB media type and title ID. For a TV series, the user must first choose the last season and episode watched; the selected episode is included in the watched range. Available episode choices come from TMDB season details, so an invalid season/episode cannot be saved. Adding the same title again does not create a duplicate.
4. Import fetches the title's name, type, release year or first-air year, poster path, overview, and all cast and crew credits available from TMDB. It resolves each credited person's TMDB person ID, display name, credit category, character name(s) for cast, and department/job for crew. Movie credits come from the movie credits endpoint; a series' full credited roster comes from series aggregate credits rather than the latest-season credits endpoint.
5. For a TV series, cache regular-season episode credits across the full series so person pages can show complete credit history. Use the saved progress marker to determine which of those credits are confirmed within the watched range. The UI displays a concise episode summary (for example, `S1 E2, S1 E4`) with an expandable list when long. Aggregate series roles or episode counts alone do not identify the exact episodes; if a credit has no episode-level match, the UI says `Episode details unavailable` and does not treat it as watched-range evidence.
6. Long imports run without freezing the page. The user sees an importing, complete, or failed state. Failed imports can be retried without creating duplicate people or credits. A partially imported title is not presented as a complete watched title.
7. Show cover art immediately before each title on the Titles page and on a person's title list. On a title's own page, show a larger poster beside the title, with type and year directly below the title and a TMDB overview below that. Space these elements evenly alongside the poster. A neutral placeholder keeps the layout consistent when TMDB has no poster; a short unavailable message covers missing overviews. Existing saved titles receive poster and overview metadata without removing or re-adding them. Headshots and other media are outside MVP scope.

### 2. Update series watch progress

1. A series detail page shows the last watched season and episode and has an **Update progress** control. The user can move the marker forward or correct it backward to any valid episode in the series.
2. Moving progress forward uses cached episode credits, fetching any missing episodes, adds newly reached cast and crew to the eligible relationship set, and recomputes connections. Moving it backward removes eligibility for credits that occur only after the corrected marker. Future episode credits do not create visible connections.
3. The update is atomic from the user's point of view: the displayed progress and relationships agree after it completes. If fetching newly needed credits fails, the old progress and relationships remain visible and the user can retry.
4. Progress is ordered by TMDB season and episode numbers. Season 0 specials and episodes with unknown order are not considered watched unless the user explicitly selects them or TMDB places them unambiguously before or at the selected marker.

### 3. Browse and search the library

1. The home page lists watched movies and series, with title, type, year, and series progress where applicable. It has a search box that filters the watched library by title and person name.
2. The navigation has distinct **Titles** and **People** views. The People view lists people credited on at least one library title and supports name search. It distinguishes people with a watched-range credit from those whose series episode placement is later or unknown.
3. Each title and person has a stable, directly linkable detail page. Links within the app use these pages so the user can move between a title, a person, and another connected title.
4. Empty library, no search results, loading, and import errors have clear UI states.
5. Titles, People, Add results, and title-page credit filters match typed characters in their original order, ignoring spaces and dashes on both sides. Exact and compact matches rank ahead of matches with more skipped characters. Results with reordered or extra typed characters do not match. Add searches can only rank candidates returned by TMDB.

### 4. Title detail and connections

1. A title page shows its title, type, year, watched status, and series progress when applicable.
2. It lists the **entire cast and crew returned by TMDB**, without an arbitrary top-billed cutoff. Cast and crew are separated and remain searchable or filterable for large titles. Within each section, TMDB order is used where available; otherwise entries sort by name.
3. Every credit shows the person's display name and link to their page, character name(s) or crew job(s), and season/episode appearances when available. Multiple roles or jobs for one person remain visible. The full series credit list may include people whose episode appearances are beyond the watched marker or unknown; these credits are distinguished from watched-range credits.
4. A collapsed **N connections** container counts each eligible shared person-to-title link and expands to list every person with a credit inside this title's watched range who also has an eligible credit in at least one other watched title. For each person, it lists all such titles with links, that person's specific role/job on each, and available season/episode details on each side of the connection. A movie's full credit list counts as watched; a series credit counts only when an appearance is known at or before the watched marker.
5. When there are no connections, show only **No connections** as primary text, without an expandable container.

### 5. Person detail

1. A person page shows their TMDB display name.
2. It lists **all library movies and series** in which they have an imported credit, with character name(s) or crew job(s). A series title row shows the earliest and latest credited years and episode total **within the saved watched range**; advancing or correcting progress updates the summary. A movie row shows its release date. Expanding a title stays on the person page. Each credited season has its own expandable container listing only credited episodes within the watched range. Credits only in later or unmapped series episodes are labelled separately and do not establish a relationship to another title.
3. Each expanded title has a remove button with confirmation. After removal, stay on the person's page if they have another saved title; otherwise redirect to Titles.
4. A person credited on only one library title still has a page. A person no longer credited on any library title disappears from the People view after the last relevant title is removed.

### 6. Remove a title

1. A watched title can be removed from its detail page after an explicit confirmation in the UI.
2. Removal deletes the watched-library entry and its associated connections immediately. Shared person records and credits needed by other watched titles remain intact; orphaned records may be cleaned up.
3. Adding the title again imports or refreshes it cleanly.
4. Removing a title must preserve other saved titles and any people or credits shared with them.

## Data and relationship rules

- **Title:** TMDB media type (`movie` or `tv`) plus TMDB title ID, display title, release/first-air year, source/update timestamps, import status, watched status, and last watched season/episode for series.
- **Person:** TMDB person ID, display name, source/update timestamps.
- **Credit:** title ID, person ID, cast/crew category, character name(s) or crew job(s), billing/order when known. A person may have several credits or roles on one title; the model must preserve them rather than overwrite them.
- **Episode appearance:** credit or person/title association, TMDB episode ID, season number, episode number, and episode title when available. Unknown season or episode numbers are allowed.
- **Eligible credit:** for a movie, an imported credit; for a series, a credit backed by at least one known episode appearance at or before the saved progress marker. Series-level credits with no episode mapping remain in the full cast/crew list but are marked as having unknown watched-range status and cannot establish a connection.
- **Connection:** two distinct watched titles share the same TMDB person ID through eligible credits. It is computed from credits and the progress marker so additions, removals, progress updates, and refreshed metadata are reflected without manual edits. The app does not show a person's full TMDB filmography as part of the watched-library relationship view.
- TMDB media type and title ID identify titles; TMDB person ID identifies people. Repeated imports and refreshes must avoid duplicates.
- Missing TMDB fields are stored as unknown and rendered honestly; they are not fabricated.

## Source and import constraints

Use the TMDB API v3 for title search, details, credits, and episode data. The app needs a TMDB API Read Access Token or API key; the token stays on the server and is never sent to the browser. The user supplies it through deployment configuration, not a committed file. Show-lib does not scrape TMDB pages.

- Search uses TMDB [movie search](https://developer.themoviedb.org/reference/search-movie) and [TV search](https://developer.themoviedb.org/reference/search-tv). Direct URLs/IDs resolve to the matching media type.
- Movie cast and crew come from [movie credits](https://developer.themoviedb.org/reference/movie-credits).
- The complete *TMDB-available* series roster comes from [TV series aggregate credits](https://developer.themoviedb.org/reference/tv-series-aggregate-credits). TMDB's standard [TV series credits](https://developer.themoviedb.org/reference/tv-series-credits) endpoint describes the latest season and must not be mistaken for the full-series roster.
- Episode choices come from [TV season details](https://developer.themoviedb.org/reference/tv-season-details). Watched-range appearances are checked against [TV episode credits](https://developer.themoviedb.org/reference/tv-episode-credits). A series-level aggregate credit alone is not proof of appearance in a particular episode.

TMDB states that its API is [free for noncommercial use with attribution](https://developer.themoviedb.org/docs/faq).

Imports should be repeatable and idempotent. A title refresh replaces or reconciles its credits and episode appearances while preserving watched status and series progress. Pagination, transient failures, potentially large series imports, and TMDB `429` responses must be handled by the server. TMDB's [rate-limit guidance](https://developer.themoviedb.org/docs/rate-limiting) calls for respecting `429` rather than relying on a fixed request quota. Do not claim that a missing TMDB credit proves a person was absent from an episode.

## Interface outline

1. **Titles:** watched-title grid/list, library search, Add title.
2. **Add title:** TMDB title search, result selection, series progress prompt, import progress/error.
3. **Title detail:** title information, series progress/update control, full cast, full crew, watched-range connections to other titles, Remove title.
4. **People:** search people in watched titles by their name, a saved title, character, or crew job. Name matches show only the person's name, role/job matches show only matching roles/jobs, and title matches show the roles/jobs on that title. Results link to person pages; an empty query shows only the search form.
5. **Person detail:** name and every library-title credit, with watched-range status.
6. **Project README:** TMDB logo and attribution notice at the bottom of `README.md`; the local application has no attribution footer or About page.

The layout should be clean and responsive on desktop and phone browsers. Long credit lists and episode lists must remain navigable through search/filtering and progressive display without silently omitting entries.

## Deployment and operations

- Package the app with Docker and a documented local startup and restart procedure. By default, publish its port on `127.0.0.1` for localhost access during development.
- Store the library in persistent server-local storage within the project directory, mounted into the container (for example, `./data`). Keep runtime data and credentials out of version control. Provide a documented backup and restore procedure for that data.
- Keep hostname, port, and host binding configurable so localhost can later be swapped for the owner's Tailscale setup without changing application code. The specific Tailscale domain and routing will be provided at the end of development. Do not expose the TMDB credential in logs or client responses.
- Provide basic health status and useful server logs for failed imports. Startup should fail clearly when required credentials or database settings are missing.

## Acceptance criteria

1. From an empty library, adding *The Sopranos* and *Nurse Jackie* prompts for the last watched episode of each and yields one watched entry for each with their available full credits.
2. On either title page, a person credited on both within the saved watched ranges appears in the connections section. The other title is linked and the person's respective role on each title is shown, with season/episode details where TMDB supplies them.
3. Opening that person's page shows both watched titles and their respective credits; titles absent from the user's library are not listed.
4. A movie with shared cast or crew connects to a watched series by the same rules.
5. A title page allows the user to inspect all imported cast and crew, including credits beyond any initial visible portion of a long list.
6. Removing one watched title removes its connections from the remaining title and updates the person page. Re-adding it restores them without duplicates.
7. Adding a series and stopping at an earlier episode excludes connections made only by people who first appear later. Those people can still be inspected in the full credits and on their person pages, labelled as not confirmed within the watched range until their episode placement is known. Advancing progress to that later episode adds their connections; correcting progress backward removes those connections again.
8. Duplicate additions, unavailable episode data, interrupted imports, TMDB `429` responses, and temporary TMDB errors have the behaviors described above.
9. The project README ends with TMDB's attribution notice and an approved TMDB logo.
10. The Dockerized app is reachable on localhost and retains its library and watch progress across a container restart. Host binding can be changed through configuration for later Tailscale deployment.

## Phased development plan to MVP 1.0.0

Complete the phases in order. Each phase ends with a working, reviewable increment and the listed exit gate. Keep the feature set defined above as the 1.0.0 scope; a phase is complete only when its behavior works through the app, not just through an isolated API call.

### Phase 0 — Confirm TMDB data and access

**Status:** Complete. The live probe, results, field mapping, and limitations are recorded in [phase-0.md](phase-0.md).

- Configure a server-side TMDB credential for local development and document how to provide it without committing it.
- Probe movie credits, series aggregate credits, season details, and episode credits using a movie and at least two multi-season series, including *The Sopranos* and *Nurse Jackie*. Check recurring cast, guest cast, crew jobs, and whether episode credits can substantiate watched-range connections.
- Record the data mapping and any coverage gaps. Treat an aggregate series role or episode count as insufficient to identify exact episodes; define the UI's `unknown` state for unmapped credits.
- **Exit gate:** the chosen TMDB endpoints and stored fields can support the full roster and evidence-based watched-range rules in this spec. Any unresolved API gap is documented before building behavior that depends on it.

### Phase 1 — Build the local foundation

**Status:** Complete. The Go service, SQLite migration, Docker setup, and exit-gate checks are recorded in [phase-1.md](phase-1.md).

- Create the application skeleton, Docker configuration, project-local persistent data mount, database schema, migrations, and server-side TMDB client.
- Model movies and series by `(media type, TMDB title ID)`, people by TMDB person ID, and credits and episode appearances without collapsing multiple roles or jobs. Add an import state and a saved progress marker for series.
- Establish title/person routes, a basic health check, configuration validation, and localhost-only binding by default.
- **Exit gate:** the app starts with one documented Docker command, a restart preserves database data, and missing TMDB credentials produce a clear setup error without exposing secrets.

### Phase 2 — Add and manage titles

**Status:** Complete. Implementation and exit-gate checks are recorded in [phase-2.md](phase-2.md).

- Implement TMDB movie/TV title search, result disambiguation, duplicate prevention, and add/remove actions.
- Import movies with all TMDB-available cast and crew. Import the full TMDB-available roster for a series from aggregate credits and offer valid season/episode choices before adding it.
- Make imports retryable and idempotent. Show importing, complete, and failed states; keep incomplete imports out of the completed watched library. Removing a title updates the library without deleting shared people needed by other titles.
- **Exit gate:** a movie and a multi-season series can be added, browsed, removed, and re-added through the UI without duplicate records or lost data after restart.

### Phase 3 — Make series progress accurate

**Status:** Complete. Episode-credit caching, progress updates, watched-range labels, and exit-gate checks are recorded in [phase-3.md](phase-3.md).

- Fetch and store regular-season episode credits across each series, merge them with aggregate credits, and retain episode-specific character and crew job details when TMDB supplies them. Apply the saved progress marker when determining watched-range eligibility.
- Implement progress updates in both directions. Use cached episode credits and fetch missing data on advance; recompute eligibility on rollback. Publish a new marker and its relationship data together only after required fetches succeed.
- Distinguish credits confirmed within the watched range from later or unknown episode placements in title and person views. Keep season 0 specials and unnumbered episodes from creating inferred connections.
- **Exit gate:** advancing to an episode adds newly eligible people, moving back removes their eligibility, and failed updates leave the prior marker and visible data intact.

### Phase 4 — Surface people and connections

**Status:** Complete. Fuzzy search, watched-range connection grouping, and exit-gate checks are recorded in [phase-4.md](phase-4.md).

- Enable fuzzy search wherever there is a search bar, with results updating as the user types after a short pause. Keep normal form submission available.
- Compute connections from shared TMDB person IDs only where both credits are eligible within the watched ranges. Show every linked title and the person's specific role on each; keep future or unmapped credits out of the connection list.
- Recompute visible results after add, removal, progress change, and metadata refresh. Let's place these connection references in a container just below the title on a title's page. If multiple people have connections from the same show/movie, comma separate the names so it reads "name1, name2, name3 also appear in title_name". If a person has multiple connections, that person should be put on a new line in the same container that reads "name also appears in title1, title2, title3."
- **Exit gate:** the *Sopranos*/*Nurse Jackie* flow and a movie-to-series connection satisfy acceptance criteria 1–7 through the browser.

### Phase 5 — Harden and release 1.0.0

- Handle TMDB outages, `429` responses, interrupted imports, missing fields, and large series without freezing the UI or presenting partial data as complete. Verify retries and refreshes preserve watched state and progress.
- Verify README attribution. Finish responsive layouts, navigation, loading/error states, and the Docker setup, backup, restore, and troubleshooting instructions.
- Keep new-install migrations consolidated into a clean baseline before the 1.0.0 release. Preserve all existing development-library data; verify the current database still opens and its saved titles remain available after each migration cleanup.
- Run the full acceptance checklist above against a clean database and again after a container restart. Check that the TMDB credential is absent from client responses, logs, and version control; verify localhost is the default binding and the host binding is configurable.
- **Exit gate:** all acceptance criteria pass, no unresolved blocker remains, and the app is tagged **1.0.0** with documented local setup and recovery steps. Tailscale routing is configured when the owner provides the home-server details; it requires no application-code change.

## Open decisions

1. **TMDB credential:** A local API key was supplied for Phase 0. Each later deployment must configure its own API Read Access Token or API key through a secret environment setting.
2. **Episode-credit coverage:** Phase 0 validated representative recurring cast, guest cast, and crew payloads. During implementation, keep missing episode mapping marked as unknown rather than inferring a connection outside the verified watched range.
3. **Tailscale deployment:** The home-server environment, hostname, and routing will be supplied at the end of development. Localhost is the active development target.

-----------

When entering a show or movie on the People page search bar, ensure each person has their role/job listed next to their real name. If a person has multiple roles or jobs for the searched show/movie, comma separate them.

Additionally, when on a specific person's page, shows and movies should have the earliest to the latest year that person was active on the show, as well as how many episodes they are credited for. For example, Edie Falco's page should have a "The Sopranos" container, next to the title should say "Carmela Soprano", and "1999-2007, 86 eps" should be fixed to the right side of the container. A movie should just show the release date. Clicking on the show or movie where they are credited should not simply go to that show or movie's page, but should stay on the person's page and expand the show or movie's container to list each season and episode they are accredited for, with the name of each season and name of each episode.

The expanded person-page list is limited to episodes the user has watched. A person-name search shows only real names, while a role/job search shows only the matching role or job next to each real name.
