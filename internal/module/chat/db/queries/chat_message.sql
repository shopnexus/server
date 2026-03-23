-- name: GetChatMessage :one
SELECT *
FROM "chat"."message"
WHERE ("id" = sqlc.narg('id'));

-- name: CountChatMessage :one
SELECT COUNT(*)
FROM "chat"."message"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("conversation_id" = ANY(sqlc.slice('conversation_id')) OR sqlc.slice('conversation_id') IS NULL) AND
    ("sender_id" = ANY(sqlc.slice('sender_id')) OR sqlc.slice('sender_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("metadata" = ANY(sqlc.slice('metadata')) OR sqlc.slice('metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListChatMessage :many
SELECT *
FROM "chat"."message"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("conversation_id" = ANY(sqlc.slice('conversation_id')) OR sqlc.slice('conversation_id') IS NULL) AND
    ("sender_id" = ANY(sqlc.slice('sender_id')) OR sqlc.slice('sender_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("metadata" = ANY(sqlc.slice('metadata')) OR sqlc.slice('metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountChatMessage :many
SELECT sqlc.embed(embed_message), COUNT(*) OVER() as total_count
FROM "chat"."message" embed_message
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("conversation_id" = ANY(sqlc.slice('conversation_id')) OR sqlc.slice('conversation_id') IS NULL) AND
    ("sender_id" = ANY(sqlc.slice('sender_id')) OR sqlc.slice('sender_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("metadata" = ANY(sqlc.slice('metadata')) OR sqlc.slice('metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateChatMessage :one
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "type", "content", "status", "metadata", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateBatchChatMessage :batchone
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "type", "content", "status", "metadata", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateCopyChatMessage :copyfrom
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "type", "content", "status", "metadata", "date_created")
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: CreateDefaultChatMessage :one
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "content", "metadata")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCopyDefaultChatMessage :copyfrom
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "content", "metadata")
VALUES ($1, $2, $3, $4);

-- name: UpdateChatMessage :one
UPDATE "chat"."message"
SET "conversation_id" = COALESCE(sqlc.narg('conversation_id'), "conversation_id"),
    "sender_id" = COALESCE(sqlc.narg('sender_id'), "sender_id"),
    "type" = COALESCE(sqlc.narg('type'), "type"),
    "content" = COALESCE(sqlc.narg('content'), "content"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "metadata" = CASE WHEN sqlc.arg('null_metadata')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('metadata'), "metadata") END,
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteChatMessage :exec
DELETE FROM "chat"."message"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("conversation_id" = ANY(sqlc.slice('conversation_id')) OR sqlc.slice('conversation_id') IS NULL) AND
    ("sender_id" = ANY(sqlc.slice('sender_id')) OR sqlc.slice('sender_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("metadata" = ANY(sqlc.slice('metadata')) OR sqlc.slice('metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);