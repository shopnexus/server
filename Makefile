.PHONY: env run test down depguard models search-eval

TEST_STACK := docker compose -f docker-compose.test.yml

IDENTITY := .restate/identity.pem

# Writes .env from .env.example on first use and later adds the variables
# the example gained, then fills in the keys: JWT, CONSOLE_KEY and the Restate
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

# Rewrites the depguard rules in .golangci.yml from the modules and layers
# under internal/. Run it after adding a module or a layer package; a test
# in cmd/depguardgen fails while the rules are stale.
depguard:
	go run ./cmd/depguardgen -root .

# Downloads module ai's models into tmp/models, where .env.example's AI_*
# point; a file already there with the right sha256 is kept.
models:
	scripts/fetch-models.sh tmp/models

# Runs the search sample set (internal/discovery/eval) against the real
# models: module ai's SigLIP 2 from tmp/models, and OpenRouter's bge-m3 and
# the LLM in .env. Not part of make test. It prints recall@10 and nDCG@10 per
# query and per kind, and writes them to tmp/.
search-eval: env models
	$(TEST_STACK) up -d --wait
	$(TEST_STACK) up -d --wait --force-recreate restate
	@until curl -sf http://127.0.0.1:56571/health >/dev/null; do sleep 0.5; done
	go test -tags eval -run TestSearchEval -count=1 -v -timeout 60m ./internal/discovery/eval/
