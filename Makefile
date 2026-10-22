.PHONY: run test down

TEST_STACK := docker compose -f docker-compose.test.yml

IDENTITY := .restate/identity.pem

# Starts the development stack and the server. On first use it copies
# .env.example to .env and adds fresh keys, including the key Restate signs
# requests with; migrations apply every time.
run:
	@test -f .env || cp .env.example .env
	@grep -q '^OTP_KEY=' .env || go run ./cmd/keygen >> .env
	@{ test -f $(IDENTITY) && grep -q '^RESTATE_IDENTITY_KEY=' .env; } || \
		{ sed -i '/^RESTATE_IDENTITY_KEY=/d' .env; go run ./cmd/keygen -restate $(IDENTITY) >> .env; }
	docker compose up -d --wait
	go run ./cmd/migrate
	go run ./cmd/server

# Runs every test, go vet and gofmt against the test stack. Restate is
# recreated each time: its journal lives in tmpfs and grows with every run.
test:
	@go run ./cmd/keygen -restate $(IDENTITY) >/dev/null
	$(TEST_STACK) up -d --wait
	$(TEST_STACK) up -d --wait --force-recreate restate
	@until curl -sf http://127.0.0.1:56571/health >/dev/null; do sleep 0.5; done
	go test -race ./...
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Stops both stacks. The development database is kept; the test stack's is
# in tmpfs and goes.
down:
	docker compose down
	$(TEST_STACK) down
