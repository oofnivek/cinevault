.PHONY: run fetch-posters generate-vtt lookup-movie

run:
	set -a; [ -f .env ] && . ./.env; set +a; go run .

fetch-posters:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/fetch-posters

generate-vtt:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/generate-vtt

lookup-movie:
	set -a; [ -f .env ] && . ./.env; set +a; python3 scripts/lookup-movie.py "$(NAME)"
