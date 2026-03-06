-- name: GetConversation :one
SELECT *
FROM "chat"."conversation"
WHERE "id" = sqlc.arg('id');

-- name: GetConversationByParticipants :one
SELECT *
FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('customer_id') AND "vendor_id" = sqlc.arg('vendor_id');

-- name: ListConversationByAccount :many
SELECT *
FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('account_id') OR "vendor_id" = sqlc.arg('account_id')
ORDER BY "last_message_at" DESC NULLS LAST
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountConversationByAccount :one
SELECT COUNT(*)
FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('account_id') OR "vendor_id" = sqlc.arg('account_id');

-- name: CreateConversation :one
INSERT INTO "chat"."conversation" ("customer_id", "vendor_id")
VALUES ($1, $2)
RETURNING *;

-- name: UpdateConversationLastMessage :exec
UPDATE "chat"."conversation"
SET "last_message_at" = CURRENT_TIMESTAMP
WHERE "id" = sqlc.arg('id');
