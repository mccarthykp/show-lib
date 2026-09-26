# Show-lib

Show-lib is a private, single-library web app for tracking watched movies and TV shows and finding shared cast and crew across them. It runs in Docker, stores data in a local SQLite file, and uses [TMDB](https://www.themoviedb.org/) for title and credit data.

## Start with Docker

You need Docker Compose and a TMDB API key or API Read Access Token.

1. Run `make setup` to create a local `.env` file.
2. Open `.env` and set **either** `TMDB_API_KEY=your_key` **or** `TMDB_READ_ACCESS_TOKEN=your_token`.
3. Run `make up` from this directory.
4. Open [http://127.0.0.1:8080/titles](http://127.0.0.1:8080/titles).

Use `make status` to check the container, `make logs` to follow its logs, and `make down` to stop it. `make down` keeps the database. `.env` and the default `data/` directory are ignored by Git. The default host bind is `127.0.0.1`; `SHOWLIB_HOST_BIND` and `SHOWLIB_PORT` in `.env` change the bind address and port.

## Optional library migration

A first install runs **migration 1** automatically and starts with an empty library. **Migration 2** is an optional, bundled snapshot of the 185 titles saved on September 26, 2026, including watched progress, people, credits, and episode data. It uses no TMDB requests. It never runs during normal startup. The snapshot is part of the source code and Docker image, so keep the repository and image private.

To load that snapshot into a **new, empty** database:

1. Configure `.env` and run `make up` once to build the image and create the empty database.
2. Run `make down` to stop the app. If the database contains anything you want to keep, back it up using the procedure below and choose a **new database path** instead of replacing it.
3. Run `make migrate-library`. This applies migration 2 as one transaction. It refuses a database with any existing library records or a recorded migration 2, leaving its data untouched.
4. Run `make up`, open the Titles page, and confirm the library has 185 titles.

The migration is designed for a fresh database. **Do not delete or overwrite your current database to use it.** Existing installations keep their saved library and do not need migration 2. For a native Go run, stop the server and run `go run ./cmd/showlib migrate-library` with `SHOWLIB_DB_PATH` set to the empty database file.

## Back up and restore

The database is in `SHOWLIB_DATA_DIR` (default `./data`) under `SHOWLIB_DB_FILENAME` (default `showlib.sqlite`). Back up the **whole data directory**, including any SQLite `-wal` or `-shm` files. For the default location:

```sh
make down
cp -a data /path/to/backup/showlib-data
make up
```

To restore, stop the app, save a copy of the current data directory, copy the backed-up directory back to the configured `SHOWLIB_DATA_DIR`, then run `make up`. For a custom directory, substitute that path in the commands. Keep backups outside the project and protect them like the library itself. `make down` does not delete the data directory.

## Troubleshooting

- **App will not start:** Run `make status` and `make logs`. Check Docker is running, one TMDB credential is set in `.env`, the configured data directory is writable, and `SHOWLIB_PORT` is available.
- **TMDB search or import fails:** Existing saved titles remain available. Open the failed title and choose **Retry import**, or retry the search when TMDB is available. Rate limits and temporary TMDB server errors are retried briefly before an error is shown.
- **A show says episode credits are unavailable:** Its saved watch marker is retained. Open the title and choose **Retry episode credits**. Connections that need episode evidence appear after the sync completes.
- **Moved the database and the library appears empty:** Stop the app and check both `SHOWLIB_DATA_DIR` and `SHOWLIB_DB_FILENAME` in `.env`. Changing the path creates a new database; it does not move the old one.

## Choose the database file

By default, the host database is `./data/showlib.sqlite`. To choose another host path, set its directory and filename in `.env`:

```dotenv
SHOWLIB_DATA_DIR=/path/to/storage
SHOWLIB_DB_FILENAME=my-library.sqlite
```

This example stores the database at `/path/to/storage/my-library.sqlite`. Docker mounts the directory so SQLite's journal files persist beside the database. Changing the path does **not** move existing data; to relocate your library, run `make down`, move the entire current data directory, update `.env`, then run `make up`.

For a native Go run, use `go run ./cmd/showlib`. It reads `.env`; `SHOWLIB_DB_PATH` sets the database file directly (default `data/showlib.sqlite`).

## What the app does

- **Add:** Search TMDB for movies and TV shows, view artwork and details, and add a title. For a show, select the last episode watched.
- **Titles:** Browse and search the watched library. Open a title for its overview, cast, crew, watched progress, and connections to other watched titles. Update progress or remove the title.
- **People:** Search saved people by real name, character, or crew job. Open a person to see their headshot, credited roles, and watched movies or episodes. Expand shows by season and episode.
- **Connections:** See shared cast and crew between watched titles, using only credits within the saved watched range.

Titles and People search update as you type and load more results as you scroll. The app has one shared library and no account system.

## TMDB attribution

![The Movie Database (TMDB) logo](https://www.themoviedb.org/assets/2/v4/logos/v2/blue_long_2-9665a76b1ae401a510ec1e0ca40ddcb3b0cfe45f1d51b77a308fea0845885648.svg)

This product uses the TMDB API but is not endorsed or certified by TMDB. Movie, TV, cast, and crew data comes from [The Movie Database](https://www.themoviedb.org).
