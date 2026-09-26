# Phase 1 — Local Go foundation

**Status:** Complete. The running Docker service is available at `http://127.0.0.1:8080` on the development machine. Title import and relationship screens are scheduled for later phases.

## Delivered

- Go application entry point with graceful shutdown, configuration validation, and a server-side TMDB API v3 client. The client accepts either a Read Access Token or API key and keeps credentials out of responses and error messages.
- SQLite database under project-local `./data`, with a versioned, transactional migration. The schema stores movie/TV identity as `(media_type, tmdb_id)`, people by TMDB ID, distinct cast/crew roles, episode records, episode appearances, import status, and series watch progress.
- Browser routes for `/titles`, `/titles/{movie|tv}/{id}`, `/people`, and `/people/{id}`, plus `/healthz`. Library lists exclude incomplete or unwatched title rows.
- Multi-stage Dockerfile and Compose setup with a persistent bind mount, localhost-only published port by default, and a container health check. `.env` and `data/` remain excluded from Git and Docker build context.

## Local startup

The setup commands and configurable ports are in [README.md](../README.md). The normal startup command is `make up`. The default host URL is `http://127.0.0.1:8080/titles`; `GET /healthz` returns `{"status":"ok"}` when the database is available. Compose reads the TMDB credential from local `.env`.

The native Go command is `go run ./cmd/showlib`. It uses `127.0.0.1:8080` and `data/showlib.sqlite` unless `SHOWLIB_LISTEN_ADDR` and `SHOWLIB_DB_PATH` are set. The container listens internally on port 8080 while Compose publishes only the configured host address.

## Exit-gate verification

- `go test ./...` and `go vet ./...` passed. The database test created the schema, retained separate credits for one person's two roles, enforced foreign keys, reopened the file, and confirmed the data and migration ledger survived.
- `docker compose config --quiet` and `docker compose up --build -d` succeeded. The container reached `healthy`, and `GET /healthz` returned `{"status":"ok"}`.
- A temporary watched-title row was visible through its title route after `docker compose restart showlib`. The temporary row was removed after verification; the local library is empty.
- Starting without either TMDB credential stopped with a clear setup error. The error did not print the secret.

## Next phase

Phase 2 will connect the existing TMDB client and routes to title search, add/remove actions, and movie/series roster imports. The current pages intentionally show empty library states until imports exist. Tailscale routing remains deferred until the home-server details are supplied.
