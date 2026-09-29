CREATE TABLE titles (
    id INTEGER PRIMARY KEY,
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv')),
    tmdb_id INTEGER NOT NULL CHECK (tmdb_id > 0),
    name TEXT NOT NULL,
    release_year INTEGER,
    release_date TEXT,
    poster_path TEXT,
    overview TEXT,
    import_status TEXT NOT NULL DEFAULT 'importing' CHECK (import_status IN ('importing', 'complete', 'failed')),
    import_error TEXT,
    import_token TEXT,
    watched INTEGER NOT NULL DEFAULT 0 CHECK (watched IN (0, 1)),
    watched_season INTEGER,
    watched_episode INTEGER,
    watched_episode_tmdb_id INTEGER,
    episode_sync_status TEXT NOT NULL DEFAULT 'pending',
    episode_sync_error TEXT,
    source_updated_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (media_type, tmdb_id),
    CHECK (media_type = 'tv' OR (watched_season IS NULL AND watched_episode IS NULL AND watched_episode_tmdb_id IS NULL)),
    CHECK ((watched_season IS NULL AND watched_episode IS NULL AND watched_episode_tmdb_id IS NULL) OR
           (watched_season IS NOT NULL AND watched_episode IS NOT NULL AND watched_episode_tmdb_id IS NOT NULL))
);

CREATE TABLE people (
    tmdb_id INTEGER PRIMARY KEY CHECK (tmdb_id > 0),
    name TEXT NOT NULL,
    profile_path TEXT,
    profile_checked_at TEXT,
    source_updated_at TEXT,
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE credits (
    id INTEGER PRIMARY KEY,
    title_id INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    person_tmdb_id INTEGER NOT NULL REFERENCES people(tmdb_id) ON DELETE CASCADE,
    credit_key TEXT NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('cast', 'crew')),
    character_name TEXT,
    department TEXT,
    job TEXT,
    billing_order INTEGER,
    UNIQUE (title_id, credit_key)
);

CREATE TABLE episodes (
    id INTEGER PRIMARY KEY,
    series_title_id INTEGER NOT NULL REFERENCES titles(id) ON DELETE CASCADE,
    tmdb_id INTEGER NOT NULL CHECK (tmdb_id > 0),
    season_number INTEGER,
    episode_number INTEGER,
    name TEXT,
    season_name TEXT,
    air_date TEXT,
    UNIQUE (series_title_id, tmdb_id),
    UNIQUE (series_title_id, season_number, episode_number)
);

CREATE TABLE episode_person_credits (
    episode_id INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    person_tmdb_id INTEGER NOT NULL REFERENCES people(tmdb_id) ON DELETE CASCADE,
    category TEXT NOT NULL CHECK (category IN ('cast', 'crew')),
    character_name TEXT NOT NULL DEFAULT '',
    job TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (episode_id, person_tmdb_id, category, character_name, job)
);

CREATE TABLE watchlist (
    media_type TEXT NOT NULL CHECK (media_type IN ('movie', 'tv')),
    tmdb_id INTEGER NOT NULL CHECK (tmdb_id > 0),
    name TEXT NOT NULL,
    release_year INTEGER,
    release_date TEXT,
    poster_path TEXT,
    overview TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (media_type, tmdb_id)
);

CREATE INDEX credits_person_idx ON credits(person_tmdb_id);
CREATE INDEX credits_title_idx ON credits(title_id);
CREATE INDEX episodes_series_idx ON episodes(series_title_id, season_number, episode_number);
CREATE INDEX episode_person_idx ON episode_person_credits(person_tmdb_id, episode_id);
CREATE INDEX watchlist_created_idx ON watchlist(created_at DESC, media_type, tmdb_id);
