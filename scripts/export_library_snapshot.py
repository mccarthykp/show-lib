#!/usr/bin/env python3
"""Create the opt-in 0002 data migration from a consistent SQLite backup."""

import gzip
import sqlite3
import sys
from pathlib import Path
from urllib.parse import quote


TABLES = ("titles", "people", "credits", "episodes", "episode_person_credits", "watchlist")


def literal(value):
    if value is None:
        return "NULL"
    if isinstance(value, str):
        return "CAST(X'" + value.encode("utf-8").hex() + "' AS TEXT)"
    if isinstance(value, bytes):
        return "X'" + value.hex() + "'"
    return str(value)


def main():
    if len(sys.argv) != 3:
        raise SystemExit("usage: export_library_snapshot.py SOURCE.sqlite OUTPUT.sql.gz")
    source_path, output_path = Path(sys.argv[1]).resolve(), Path(sys.argv[2]).resolve()
    if not source_path.is_file():
        raise SystemExit("source database does not exist")
    if output_path.exists():
        raise SystemExit("output exists; choose a new path so the snapshot is not overwritten")

    source = sqlite3.connect("file:" + quote(str(source_path)) + "?mode=ro", uri=True)
    snapshot = sqlite3.connect(":memory:")
    try:
        source.backup(snapshot)
        if snapshot.execute("PRAGMA integrity_check").fetchone()[0] != "ok":
            raise SystemExit("source database failed its integrity check")
        foreign_key_errors = list(snapshot.execute("PRAGMA foreign_key_check"))
        if foreign_key_errors:
            raise SystemExit(
                f"source database has {len(foreign_key_errors)} foreign-key violations"
            )
        with output_path.open("wb") as output:
            with gzip.GzipFile(filename="", mode="wb", fileobj=output, compresslevel=9, mtime=0) as compressed:
                compressed.write(b"-- Show-lib opt-in library migration 0002; generated from a consistent database backup.\n")
                for table in TABLES:
                    columns = [row[1] for row in snapshot.execute(f"PRAGMA table_info({table})")]
                    if not columns:
                        raise SystemExit(f"missing source table: {table}")
                    names = ", ".join('"' + name + '"' for name in columns)
                    prefix = f'INSERT INTO "{table}" ({names}) VALUES ('
                    count = 0
                    for row in snapshot.execute(f"SELECT * FROM {table} ORDER BY rowid"):
                        compressed.write((prefix + ", ".join(map(literal, row)) + ");\n").encode("ascii"))
                        count += 1
                    print(f"{table}: {count}")
    finally:
        snapshot.close()
        source.close()


if __name__ == "__main__":
    main()
