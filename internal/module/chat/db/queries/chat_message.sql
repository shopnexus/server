-- name: GetMessage :one
SELECT *
FROM "chat"."message"
WHERE "id" = sqlc.arg('id');

-- name: ListMessageByConversation :many
SELECT *
FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id')
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CountMessageByConversation :one
SELECT COUNT(*)
FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id');

-- name: CreateMessage :one
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "type", "content", "metadata")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: MarkMessagesRead :exec
UPDATE "chat"."message"
SET "status" = 'Read'
WHERE "conversation_id" = sqlc.arg('conversation_id')
    AND "sender_id" != sqlc.arg('reader_id')
    AND "status" != 'Read';

-- name: CountUnreadMessages :one
SELECT COUNT(*)
FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id')
    AND "sender_id" != sqlc.arg('reader_id')
    AND "status" != 'Read';
