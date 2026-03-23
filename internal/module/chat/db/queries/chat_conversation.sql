-- name: GetChatConversation :one
SELECT *
FROM "chat"."conversation"
WHERE ("id" = sqlc.narg('id')) OR ("customer_id" = sqlc.narg('customer_id') AND "vendor_id" = sqlc.narg('vendor_id'));

-- name: CountChatConversation :one
SELECT COUNT(*)
FROM "chat"."conversation"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("customer_id" = ANY(sqlc.slice('customer_id')) OR sqlc.slice('customer_id') IS NULL) AND
    ("vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) AND
    ("last_message_at" = ANY(sqlc.slice('last_message_at')) OR sqlc.slice('last_message_at') IS NULL) AND
    ("last_message_at" > sqlc.narg('last_message_at_from') OR sqlc.narg('last_message_at_from') IS NULL) AND
    ("last_message_at" < sqlc.narg('last_message_at_to') OR sqlc.narg('last_message_at_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListChatConversation :many
SELECT *
FROM "chat"."conversation"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("customer_id" = ANY(sqlc.slice('customer_id')) OR sqlc.slice('customer_id') IS NULL) AND
    ("vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) AND
    ("last_message_at" = ANY(sqlc.slice('last_message_at')) OR sqlc.slice('last_message_at') IS NULL) AND
    ("last_message_at" > sqlc.narg('last_message_at_from') OR sqlc.narg('last_message_at_from') IS NULL) AND
    ("last_message_at" < sqlc.narg('last_message_at_to') OR sqlc.narg('last_message_at_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountChatConversation :many
SELECT sqlc.embed(embed_conversation), COUNT(*) OVER() as total_count
FROM "chat"."conversation" embed_conversation
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("customer_id" = ANY(sqlc.slice('customer_id')) OR sqlc.slice('customer_id') IS NULL) AND
    ("vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) AND
    ("last_message_at" = ANY(sqlc.slice('last_message_at')) OR sqlc.slice('last_message_at') IS NULL) AND
    ("last_message_at" > sqlc.narg('last_message_at_from') OR sqlc.narg('last_message_at_from') IS NULL) AND
    ("last_message_at" < sqlc.narg('last_message_at_to') OR sqlc.narg('last_message_at_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateChatConversation :one
INSERT INTO "chat"."conversation" ("id", "customer_id", "vendor_id", "last_message_at", "date_created")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateBatchChatConversation :batchone
INSERT INTO "chat"."conversation" ("id", "customer_id", "vendor_id", "last_message_at", "date_created")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyChatConversation :copyfrom
INSERT INTO "chat"."conversation" ("id", "customer_id", "vendor_id", "last_message_at", "date_created")
VALUES ($1, $2, $3, $4, $5);

-- name: CreateDefaultChatConversation :one
INSERT INTO "chat"."conversation" ("customer_id", "vendor_id", "last_message_at")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultChatConversation :copyfrom
INSERT INTO "chat"."conversation" ("customer_id", "vendor_id", "last_message_at")
VALUES ($1, $2, $3);

-- name: UpdateChatConversation :one
UPDATE "chat"."conversation"
SET "customer_id" = COALESCE(sqlc.narg('customer_id'), "customer_id"),
    "vendor_id" = COALESCE(sqlc.narg('vendor_id'), "vendor_id"),
    "last_message_at" = CASE WHEN sqlc.arg('null_last_message_at')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('last_message_at'), "last_message_at") END,
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteChatConversation :exec
DELETE FROM "chat"."conversation"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("customer_id" = ANY(sqlc.slice('customer_id')) OR sqlc.slice('customer_id') IS NULL) AND
    ("vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) AND
    ("last_message_at" = ANY(sqlc.slice('last_message_at')) OR sqlc.slice('last_message_at') IS NULL) AND
    ("last_message_at" > sqlc.narg('last_message_at_from') OR sqlc.narg('last_message_at_from') IS NULL) AND
    ("last_message_at" < sqlc.narg('last_message_at_to') OR sqlc.narg('last_message_at_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);