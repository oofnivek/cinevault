.PHONY: run

run:
	set -a; [ -f .env ] && . ./.env; set +a; go run .
