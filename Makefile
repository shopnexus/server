.PHONY: up down run migrate lint mock

up:
	docker compose up -d --wait

down:
	docker compose down

run:
	go run ./cmd/server

migrate:
	go run ./cmd/migrate

lint:
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

mock:
	docker compose --profile mock up -d prism
