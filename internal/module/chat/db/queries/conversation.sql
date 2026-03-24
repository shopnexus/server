-- name: GetConversationByParticipants :one
SELECT * FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('customer_id') AND "vendor_id" = sqlc.arg('vendor_id');

-- name: GetConversationByID :one
SELECT * FROM "chat"."conversation"
WHERE "id" = sqlc.arg('id');

-- name: ListConversationByAccount :many
SELECT * FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('account_id') OR "vendor_id" = sqlc.arg('account_id')
ORDER BY "last_message_at" DESC NULLS LAST
LIMIT sqlc.arg('limit')::int
OFFSET sqlc.arg('offset')::int;

-- name: CountConversationByAccount :one
SELECT COUNT(*) FROM "chat"."conversation"
WHERE "customer_id" = sqlc.arg('account_id') OR "vendor_id" = sqlc.arg('account_id');

-- name: UpdateConversationLastMessage :exec
UPDATE "chat"."conversation"
SET "last_message_at" = NOW()
WHERE "id" = sqlc.arg('id');
