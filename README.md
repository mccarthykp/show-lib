# Show-lib

Show-lib is a private, single-library web app for tracking watched movies and TV shows and finding shared cast and crew across them. It runs in Docker, stores data in a local SQLite file, and uses [TMDB](https://www.themoviedb.org/) for title and credit data.

## Start with Docker

You need Docker Compose and a TMDB API key or API Read Access Token.

1. Run `make setup` to create a local `.env` file.
2. Open `.env` and set **either** `TMDB_API_KEY=your_key` **or** `TMDB_READ_ACCESS_TOKEN=your_token`.
3. Run `make up` from this directory.
4. Open [http://127.0.0.1:8080/titles](http://127.0.0.1:8080/titles).

Use `make status` to check the container, `make logs` to follow its logs, and `make down` to stop it. `make down` keeps the database. `.env` and the default `data/` directory are ignored by Git. The default host bind is `127.0.0.1`; `SHOWLIB_HOST_BIND` and `SHOWLIB_PORT` in `.env` change the bind address and port.

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

Titles and People search update as you type and load more results as you scroll. The app has one shared library and no account system. For detailed behavior and the MVP plan, see [docs/spec.md](docs/spec.md).

## TMDB attribution

![The Movie Database (TMDB) logo](https://www.themoviedb.org/assets/2/v4/logos/v2/blue_long_2-9665a76b1ae401a510ec1e0ca40ddcb3b0cfe45f1d51b77a308fea0845885648.svg)

This product uses the TMDB API but is not endorsed or certified by TMDB. Movie, TV, cast, and crew data comes from [The Movie Database](https://www.themoviedb.org).
