.PHONY: env run test down models search-eval loadtest loadtest-sessions

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

K6 := grafana/k6:2.3.0
# psql for the database report, from the image the stacks run.
PSQL_IMAGE := paradedb/paradedb:0.26.4-pg18
# The settings loadtest/lib/config.js reads, passed on when set.
LOADTEST_VARS := BASE_URL PROFILE GUEST_RATE MEMBER_RATE TRADE_RATE SOCKETS TIME_SCALE THINK_SCALE SOCKET_MINUTES SESSIONS_FILE SPREAD_ADDRESSES
LOADTEST_NETWORK ?= host
LOADTEST_PSQL = docker run --rm -i --network $(LOADTEST_NETWORK) $(PSQL_IMAGE) psql -X -q "$(PGSTATS_URL)"
# K6_OTLP, host:port of an OTLP/HTTP receiver such as the stack's lgtm,
# sends k6's metrics there, beside the server's.
LOADTEST_OTEL = $(if $(K6_OTLP),-e K6_OTEL_EXPORTER_PROTOCOL=http/protobuf -e K6_OTEL_HTTP_EXPORTER_ENDPOINT=$(K6_OTLP) \
	-e K6_OTEL_HTTP_EXPORTER_INSECURE=true -e K6_OTEL_METRIC_PREFIX=k6_)

# Runs the k6 load test in loadtest/ (its README.md), against make run's
# server unless BASE_URL says otherwise:
#   make loadtest PROFILE=load GUEST_RATE=20 MEMBER_RATE=5 SOCKETS=500
# With PGSTATS_URL, a superuser's URL of the stack's database, it resets
# pg_stat_statements first and writes its report to loadtest/results/ after.
loadtest:
	$(if $(PGSTATS_URL),$(LOADTEST_PSQL) < loadtest/pgstats-reset.sql >/dev/null)
	docker run --rm --network $(LOADTEST_NETWORK) -v $(CURDIR)/loadtest:/loadtest -w /loadtest \
		$(foreach v,$(LOADTEST_VARS),$(if $($(v)),-e $(v)=$($(v)))) $(LOADTEST_OTEL) \
		$(K6) run $(if $(K6_OTLP),-o opentelemetry) main.js; status=$$?; \
	$(if $(PGSTATS_URL),mkdir -p loadtest/results && f=loadtest/results/pgstats-$$(date +%Y%m%d-%H%M%S).txt && \
		$(LOADTEST_PSQL) < loadtest/pgstats-report.sql > $$f && echo "database report: $$f";) exit $$status

# Signs in the sessions the signed-in VUs use, SESSIONS of them across
# ACCOUNTS accounts, and TRADES trades for the trade VUs, into
# loadtest/sessions.json. Run it before each run.
loadtest-sessions: env
	go run ./cmd/loadsessions -accounts $(or $(ACCOUNTS),200) -sessions $(or $(SESSIONS),1000) \
		-trades $(or $(TRADES),0) -sellers $(or $(SELLERS),20) -listings $(or $(LISTINGS),10) > loadtest/sessions.json
