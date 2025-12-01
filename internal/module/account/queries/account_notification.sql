-- name: GetNotification :one
SELECT *
FROM "account"."notification"
WHERE ("id" = sqlc.narg('id'));

-- name: CountNotification :one
SELECT COUNT(*)
FROM "account"."notification"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("channel" = ANY(sqlc.slice('channel')) OR sqlc.slice('channel') IS NULL) AND
    ("is_read" = ANY(sqlc.slice('is_read')) OR sqlc.slice('is_read') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL) AND
    ("date_sent" = ANY(sqlc.slice('date_sent')) OR sqlc.slice('date_sent') IS NULL) AND
    ("date_sent" > sqlc.narg('date_sent_from') OR sqlc.narg('date_sent_from') IS NULL) AND
    ("date_sent" < sqlc.narg('date_sent_to') OR sqlc.narg('date_sent_to') IS NULL) AND
    ("date_scheduled" = ANY(sqlc.slice('date_scheduled')) OR sqlc.slice('date_scheduled') IS NULL) AND
    ("date_scheduled" > sqlc.narg('date_scheduled_from') OR sqlc.narg('date_scheduled_from') IS NULL) AND
    ("date_scheduled" < sqlc.narg('date_scheduled_to') OR sqlc.narg('date_scheduled_to') IS NULL)
);

-- name: ListNotification :many
SELECT *
FROM "account"."notification"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("channel" = ANY(sqlc.slice('channel')) OR sqlc.slice('channel') IS NULL) AND
    ("is_read" = ANY(sqlc.slice('is_read')) OR sqlc.slice('is_read') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL) AND
    ("date_sent" = ANY(sqlc.slice('date_sent')) OR sqlc.slice('date_sent') IS NULL) AND
    ("date_sent" > sqlc.narg('date_sent_from') OR sqlc.narg('date_sent_from') IS NULL) AND
    ("date_sent" < sqlc.narg('date_sent_to') OR sqlc.narg('date_sent_to') IS NULL) AND
    ("date_scheduled" = ANY(sqlc.slice('date_scheduled')) OR sqlc.slice('date_scheduled') IS NULL) AND
    ("date_scheduled" > sqlc.narg('date_scheduled_from') OR sqlc.narg('date_scheduled_from') IS NULL) AND
    ("date_scheduled" < sqlc.narg('date_scheduled_to') OR sqlc.narg('date_scheduled_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountNotification :many
SELECT sqlc.embed(embed_notification), COUNT(*) OVER() as total_count
FROM "account"."notification" embed_notification
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("channel" = ANY(sqlc.slice('channel')) OR sqlc.slice('channel') IS NULL) AND
    ("is_read" = ANY(sqlc.slice('is_read')) OR sqlc.slice('is_read') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL) AND
    ("date_sent" = ANY(sqlc.slice('date_sent')) OR sqlc.slice('date_sent') IS NULL) AND
    ("date_sent" > sqlc.narg('date_sent_from') OR sqlc.narg('date_sent_from') IS NULL) AND
    ("date_sent" < sqlc.narg('date_sent_to') OR sqlc.narg('date_sent_to') IS NULL) AND
    ("date_scheduled" = ANY(sqlc.slice('date_scheduled')) OR sqlc.slice('date_scheduled') IS NULL) AND
    ("date_scheduled" > sqlc.narg('date_scheduled_from') OR sqlc.narg('date_scheduled_from') IS NULL) AND
    ("date_scheduled" < sqlc.narg('date_scheduled_to') OR sqlc.narg('date_scheduled_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateNotification :one
INSERT INTO "account"."notification" ("account_id", "type", "channel", "is_read", "content", "date_created", "date_updated", "date_sent", "date_scheduled")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateBatchNotification :batchone
INSERT INTO "account"."notification" ("account_id", "type", "channel", "is_read", "content", "date_created", "date_updated", "date_sent", "date_scheduled")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateCopyNotification :copyfrom
INSERT INTO "account"."notification" ("account_id", "type", "channel", "is_read", "content", "date_created", "date_updated", "date_sent", "date_scheduled")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CreateDefaultNotification :one
INSERT INTO "account"."notification" ("account_id", "type", "channel", "content", "date_sent", "date_scheduled")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopyDefaultNotification :copyfrom
INSERT INTO "account"."notification" ("account_id", "type", "channel", "content", "date_sent", "date_scheduled")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateNotification :one
UPDATE "account"."notification"
SET "account_id" = COALESCE(sqlc.narg('account_id'), "account_id"),
    "type" = COALESCE(sqlc.narg('type'), "type"),
    "channel" = COALESCE(sqlc.narg('channel'), "channel"),
    "is_read" = COALESCE(sqlc.narg('is_read'), "is_read"),
    "content" = COALESCE(sqlc.narg('content'), "content"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated"),
    "date_sent" = CASE WHEN sqlc.arg('null_date_sent')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('date_sent'), "date_sent") END,
    "date_scheduled" = CASE WHEN sqlc.arg('null_date_scheduled')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('date_scheduled'), "date_scheduled") END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteNotification :exec
DELETE FROM "account"."notification"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("channel" = ANY(sqlc.slice('channel')) OR sqlc.slice('channel') IS NULL) AND
    ("is_read" = ANY(sqlc.slice('is_read')) OR sqlc.slice('is_read') IS NULL) AND
    ("content" = ANY(sqlc.slice('content')) OR sqlc.slice('content') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL) AND
    ("date_sent" = ANY(sqlc.slice('date_sent')) OR sqlc.slice('date_sent') IS NULL) AND
    ("date_sent" > sqlc.narg('date_sent_from') OR sqlc.narg('date_sent_from') IS NULL) AND
    ("date_sent" < sqlc.narg('date_sent_to') OR sqlc.narg('date_sent_to') IS NULL) AND
    ("date_scheduled" = ANY(sqlc.slice('date_scheduled')) OR sqlc.slice('date_scheduled') IS NULL) AND
    ("date_scheduled" > sqlc.narg('date_scheduled_from') OR sqlc.narg('date_scheduled_from') IS NULL) AND
    ("date_scheduled" < sqlc.narg('date_scheduled_to') OR sqlc.narg('date_scheduled_to') IS NULL)
);