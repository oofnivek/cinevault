.PHONY: run fetch-posters

run:
	set -a; [ -f .env ] && . ./.env; set +a; go run .

fetch-posters:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/fetch-posters
