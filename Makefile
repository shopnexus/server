.PHONY: up down run migrate lint mock test-up test-down test-unit test-int test-e2e test

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

TEST_DATABASE_URL ?= postgres://server2:server2@localhost:56433/server2_test?sslmode=disable
export TEST_DATABASE_URL
TEST_RESTATE_INGRESS_URL ?= http://127.0.0.1:56581
TEST_RESTATE_ADMIN_URL ?= http://127.0.0.1:56571
export TEST_RESTATE_INGRESS_URL TEST_RESTATE_ADMIN_URL

test-up:
	docker compose -f docker-compose.test.yml up -d --wait

test-down:
	docker compose -f docker-compose.test.yml down -v

test-unit:
	go test -short -race ./...

test-int:
	go test -race ./internal/...

test-e2e:
	go test -race ./e2e/...

test: test-unit test-int test-e2e
