.PHONY: run fetch-posters generate-vtt

run:
	set -a; [ -f .env ] && . ./.env; set +a; go run .

fetch-posters:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/fetch-posters

generate-vtt:
	set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/generate-vtt
