# Show-lib

Show-lib is a private, single-library web app for movies and TV. It stores the
library in SQLite and gets title, cast, crew, artwork, and episode data from
[TMDB](https://www.themoviedb.org/).

## Set up locally

Requirements: Docker with Docker Compose, `make`, and either a TMDB API key or
API Read Access Token.

1. Create the local configuration:

   ```sh
   make init
   ```

2. Edit `.env`. Set one TMDB credential and choose where the persistent SQLite
   data will live:

   ```dotenv
   TMDB_API_KEY=your_key
   SHOWLIB_DATA_DIR=/absolute/path/to/show-lib-data
   ```

   `TMDB_READ_ACCESS_TOKEN` can be used instead of `TMDB_API_KEY`. A relative
   path such as `./data` is also valid.

3. Build and start the service:

   ```sh
   make setup
   ```

4. Open <http://127.0.0.1:8080/titles>.

`make setup` creates the configured data directory, builds the image, starts
the container, and runs migration 1 to create an empty database. It does not
load the bundled personal library.

Useful commands:

```text
make status  Show the container state
make logs    Follow application logs
make down    Stop the app without deleting data
make up      Build and start the app again
```

## Restore the bundled library

Migration 2 is an optional snapshot of the current personal library: 263
watched titles, 30 watchlist titles, watched progress, people, credits, and
episode data as of September 29, 2026. It makes no TMDB requests. Because the
snapshot is part of the repository and image, keep both private.

It can only be applied to the empty database created by migration 1:

```sh
make setup
make down
make migrate-library
make up
```

The import is transactional and refuses to run when any library or watchlist
table already contains data, or when migration 2 is already recorded.

## Functionality

- Search TMDB for movies and TV shows, preview details, and add titles as
  watched or to the watchlist.
- Track the latest watched season and episode, including specials.
- Browse and filter watched titles and the watchlist; choose a random watchlist
  title.
- Search people by name, character, or crew role.
- View a person's watched and unwatched appearances, grouped TV episodes, and
  add or remove titles directly from those results.
- Find cast and crew connections between watched titles.
- Retry interrupted title imports and episode-credit syncs without losing
  saved progress.

The app has one shared library and no user accounts. Titles and People search
update as you type and paginate automatically.

## Data and backups

The database file is:

```text
SHOWLIB_DATA_DIR/SHOWLIB_DB_FILENAME
```

`SHOWLIB_DB_FILENAME` defaults to `showlib.sqlite`. Stop the service and back up
the whole data directory so SQLite journal files are included:

```sh
make down
cp -a /path/to/show-lib-data /path/to/backup/show-lib-data
make up
```

To restore, stop the app, preserve the current data directory, replace it with
the backup, and run `make up`. Changing `SHOWLIB_DATA_DIR` creates a new database;
it does not move an existing one.

## Configuration

The remaining `.env` settings are optional:

```dotenv
SHOWLIB_DB_FILENAME=showlib.sqlite
SHOWLIB_HOST_BIND=127.0.0.1
SHOWLIB_PORT=8080
```

`SHOWLIB_HOST_BIND=127.0.0.1` keeps the service local to the machine. Change it
only when intentionally exposing the app through a trusted network or proxy.

For a native Go run, use `go run ./cmd/showlib`. It reads `.env` and accepts
`SHOWLIB_DB_PATH` as the direct database-file path.

## TMDB attribution

This product uses the TMDB API but is not endorsed or certified by TMDB.
