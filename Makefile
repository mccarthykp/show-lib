.DEFAULT_GOAL := help
.PHONY: help init check-env setup up down logs status migrate-library

COMPOSE = env SHOWLIB_UID=$$(id -u) SHOWLIB_GID=$$(id -g) docker compose

help:
	@printf 'make init    Create .env from .env.example (once)\n'
	@printf 'make setup   Create the data directory, build, and start Show-lib\n'
	@printf 'make up      Same as make setup\n'
	@printf 'make down    Stop Show-lib without removing database files\n'
	@printf 'make logs    Follow service logs\n'
	@printf 'make status  Show service status\n'
	@printf 'make migrate-library  Import the bundled library into an empty database (service must be stopped)\n'

init:
	@if [ -f .env ]; then printf '.env already exists; left unchanged\n'; else install -m 600 .env.example .env; printf 'Created .env; add a TMDB credential and choose SHOWLIB_DATA_DIR\n'; fi

check-env:
	@test -f .env || { printf 'Run make init, then configure .env\n'; exit 1; }
	@grep -Eq '^(TMDB_READ_ACCESS_TOKEN|TMDB_API_KEY)=.+$$' .env || { printf 'Set TMDB_READ_ACCESS_TOKEN or TMDB_API_KEY in .env\n'; exit 1; }
	@grep -Eq '^SHOWLIB_DATA_DIR=.+$$' .env || { printf 'Set SHOWLIB_DATA_DIR in .env\n'; exit 1; }

setup: check-env
	@set -a; . ./.env; set +a; install -d -m 700 "$$SHOWLIB_DATA_DIR"
	$(COMPOSE) up --build -d
	$(COMPOSE) ps

up: setup

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f --tail=100 showlib

status:
	$(COMPOSE) ps

migrate-library: check-env
	@test -z "$$($(COMPOSE) ps --status running -q showlib)" || { printf 'Stop Show-lib with make down before importing the library snapshot\n'; exit 1; }
	$(COMPOSE) run --rm --no-deps showlib showlib migrate-library
