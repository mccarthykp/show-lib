.DEFAULT_GOAL := help
.PHONY: help setup up down logs status

help:
	@printf 'make setup   Create .env from the example (once)\n'
	@printf 'make up      Build and start Show-lib\n'
	@printf 'make down    Stop Show-lib without removing database files\n'
	@printf 'make logs    Follow service logs\n'
	@printf 'make status  Show service status\n'

setup:
	@if [ -f .env ]; then printf '.env already exists; left unchanged\n'; else install -m 600 .env.example .env; printf 'Created .env; add your TMDB credential before make up\n'; fi

up:
	@test -f .env || { printf 'Run make setup and add your TMDB credential to .env first\n'; exit 1; }
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f --tail=100 showlib

status:
	docker compose ps
