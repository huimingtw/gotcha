.PHONY: run test docker-up docker-down reset help

help: ## Show available commands
	@grep -E '^[a-zA-Z_-]+:.*?##' Makefile | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-14s %s\n", $$1, $$2}'

run: ## Run the server locally (requires Go 1.23+)
	go run .

test: ## Run all tests
	go test ./...

docker-up: ## Build and start the app + ngrok tunnel
	docker compose up --build -d

docker-down: ## Stop and remove containers
	docker compose down

reset: ## Hard reset: wipe game data and restart
	docker compose down
	rm -rf data/
	docker compose up --build -d
