.PHONY: run dev generate pgtempl migrate seed seedcmt build register schema erdiagram

# Downgrade protobuf registration conflict to warning (restate-sdk vs milvus share "internal.proto")
export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn

dev:
	air

run:
	go run ./cmd/server/

build:
	go build -o bin/server ./cmd/server/

generate:
	go generate ./...

pgtempl:
	go run ./cmd/pgtempl/ -module all

migrate:
	go run ./cmd/migrate/

seed:
	go run ./cmd/seed/

seedcmt:
	go run ./cmd/seedcmt/

# Register Go service endpoint with Restate runtime
# Requires: Restate cluster running (docker-compose) and Go server running (make run/dev)
RESTATE_ADMIN ?= http://localhost:9470
RESTATE_SERVICE ?= http://host.docker.internal:8081

register:
	curl -s $(RESTATE_ADMIN)/deployments -H 'content-type: application/json' \
		-d '{"uri": "$(RESTATE_SERVICE)"}' | jq .
	@echo "Services registered with Restate"

schema:
	go run ./cmd/erdiagram/
