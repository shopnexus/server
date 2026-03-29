-- name: CreateChatMessage :one
INSERT INTO "chat"."message" ("conversation_id", "sender_id", "type", "content", "metadata")
VALUES (sqlc.arg('conversation_id'), sqlc.arg('sender_id'), sqlc.arg('type'), sqlc.arg('content'), sqlc.arg('metadata'))
RETURNING *;

-- name: ListMessageByConversation :many
SELECT * FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id')
ORDER BY "date_created" DESC
LIMIT sqlc.arg('limit')::int
OFFSET sqlc.arg('offset')::int;

-- name: CountMessageByConversation :one
SELECT COUNT(*) FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id');

-- name: CountUnreadMessages :one
SELECT COUNT(*) FROM "chat"."message"
WHERE "conversation_id" = sqlc.arg('conversation_id')
    AND "sender_id" != sqlc.arg('reader_id')
    AND "status" != 'Read';

-- name: MarkMessagesRead :exec
UPDATE "chat"."message"
SET "status" = 'Read'
WHERE "conversation_id" = sqlc.arg('conversation_id')
    AND "sender_id" != sqlc.arg('reader_id')
    AND "status" != 'Read';
