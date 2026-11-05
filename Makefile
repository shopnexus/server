.PHONY: env run test down

TEST_STACK := docker compose -f docker-compose.test.yml

IDENTITY := .restate/identity.pem

# Writes .env from .env.example on first use and later adds the variables
# the example gained, then fills in the keys: JWT, STAFF_KEY and the Restate
# request identity key, whose private half is $(IDENTITY). Keys already set
# stay.
env:
	@go run ./cmd/envgen -env .env -example .env.example -restate $(IDENTITY)

# Starts the development stack and the server. Migrations apply and the
# object store bucket is prepared every time.
run: env
	docker compose up -d --wait
	go run ./cmd/migrate
	go run ./cmd/objectstore-init
	go run ./cmd/server

# Runs every test against the test stack, then golangci-lint, which also
# runs go vet and checks gofumpt formatting. Restate is
# recreated each time: its journal lives in tmpfs and grows with every run.
test: env
	$(TEST_STACK) up -d --wait
	$(TEST_STACK) up -d --wait --force-recreate restate
	@until curl -sf http://127.0.0.1:56571/health >/dev/null; do sleep 0.5; done
	go test -race ./...
	golangci-lint run ./...

# Stops both stacks. The development database is kept; the test stack's is
# in tmpfs and goes.
down:
	docker compose down
	$(TEST_STACK) down
