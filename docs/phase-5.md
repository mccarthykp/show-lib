# Phase 5 — Release hardening

**Status:** Complete for the local 1.0.0 release. Tailscale routing awaits the home-server address and is independent of application code.

## Delivered

- TMDB requests retry HTTP 429, 408, and temporary 5xx responses with bounded backoff. `Retry-After` is respected up to 30 seconds; longer waits return a clear retryable error. Request cancellation stops the wait. TMDB API keys stay out of client error text.
- New databases start from one complete schema. Historical migrations remain available to upgrade existing databases without clearing their titles, people, or progress.
- Imports and episode sync remain separate background operations with visible loading, failure, and retry states. A failed episode sync leaves the saved watch marker unchanged and cannot produce unverified connections.
- The README now covers startup, database location, backup, restore, and common recovery steps. The TMDB attribution remains at the bottom.

## Verification

- `go test ./...` and `go vet ./...` passed. Tests cover a fresh schema, legacy upgrade with a saved title, interrupted and failed imports, progress advance and rollback, connection changes, and TMDB retry and cancellation behavior.
- Before the Docker rebuild, the existing SQLite database passed `PRAGMA integrity_check` and contained 185 completed watched titles. A separate SQLite backup was made. After rebuilding and restarting, the database again passed the integrity check and contained the same 185 titles. *The Sopranos* remained at S6 E21 and *Nurse Jackie* at S7 E12, with episode sync complete for both.
- Browser checks confirmed Titles, the two series detail pages with cast, crew, progress, and connections, and a live TMDB Add search. The local Docker service is bound to `127.0.0.1:8080`; Compose allows changing host binding and port through `.env`.
- `.env` and the database directory are ignored by Git. The TMDB credential is read server-side and is absent from the checked browser pages and test error output.

The automated tests use clean temporary SQLite databases to exercise the acceptance behavior; the existing populated library was preserved throughout release verification.
