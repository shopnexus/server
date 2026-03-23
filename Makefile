.PHONY: run dev generate migrate seed seedcmt build

# Downgrade protobuf registration conflict to warning (restate-sdk vs milvus share "internal.proto")
export GOLANG_PROTOBUF_REGISTRATION_CONFLICT=warn

dev:
	air

run:
	go run cmd/server/main.go

build:
	go build -o bin/server cmd/server/main.go

generate:
	go generate ./...

migrate:
	go run cmd/migrate/main.go

seed:
	go run cmd/seed/main.go

seedcmt:
	go run cmd/seedcmt/main.go
