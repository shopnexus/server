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
	go run ./cmd/pgtempl/ -module all -skip-schema-prefix

migrate:
	go run ./cmd/migrate/

seed:
	go run ./cmd/seed/

schema:
	go run ./cmd/erdiagram/
