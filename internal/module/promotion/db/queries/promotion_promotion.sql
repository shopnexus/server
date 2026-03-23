-- name: GetPromotion :one
SELECT *
FROM "promotion"."promotion"
WHERE ("id" = sqlc.narg('id')) OR ("code" = sqlc.narg('code'));

-- name: CountPromotion :one
SELECT COUNT(*)
FROM "promotion"."promotion"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("owner_id" = ANY(sqlc.slice('owner_id')) OR sqlc.slice('owner_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("title" = ANY(sqlc.slice('title')) OR sqlc.slice('title') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("auto_apply" = ANY(sqlc.slice('auto_apply')) OR sqlc.slice('auto_apply') IS NULL) AND
    ("group" = ANY(sqlc.slice('group')) OR sqlc.slice('group') IS NULL) AND
    ("date_started" = ANY(sqlc.slice('date_started')) OR sqlc.slice('date_started') IS NULL) AND
    ("date_started" > sqlc.narg('date_started_from') OR sqlc.narg('date_started_from') IS NULL) AND
    ("date_started" < sqlc.narg('date_started_to') OR sqlc.narg('date_started_to') IS NULL) AND
    ("date_ended" = ANY(sqlc.slice('date_ended')) OR sqlc.slice('date_ended') IS NULL) AND
    ("date_ended" > sqlc.narg('date_ended_from') OR sqlc.narg('date_ended_from') IS NULL) AND
    ("date_ended" < sqlc.narg('date_ended_to') OR sqlc.narg('date_ended_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);

-- name: ListPromotion :many
SELECT *
FROM "promotion"."promotion"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("owner_id" = ANY(sqlc.slice('owner_id')) OR sqlc.slice('owner_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("title" = ANY(sqlc.slice('title')) OR sqlc.slice('title') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("auto_apply" = ANY(sqlc.slice('auto_apply')) OR sqlc.slice('auto_apply') IS NULL) AND
    ("group" = ANY(sqlc.slice('group')) OR sqlc.slice('group') IS NULL) AND
    ("date_started" = ANY(sqlc.slice('date_started')) OR sqlc.slice('date_started') IS NULL) AND
    ("date_started" > sqlc.narg('date_started_from') OR sqlc.narg('date_started_from') IS NULL) AND
    ("date_started" < sqlc.narg('date_started_to') OR sqlc.narg('date_started_to') IS NULL) AND
    ("date_ended" = ANY(sqlc.slice('date_ended')) OR sqlc.slice('date_ended') IS NULL) AND
    ("date_ended" > sqlc.narg('date_ended_from') OR sqlc.narg('date_ended_from') IS NULL) AND
    ("date_ended" < sqlc.narg('date_ended_to') OR sqlc.narg('date_ended_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountPromotion :many
SELECT sqlc.embed(embed_promotion), COUNT(*) OVER() as total_count
FROM "promotion"."promotion" embed_promotion
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("owner_id" = ANY(sqlc.slice('owner_id')) OR sqlc.slice('owner_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("title" = ANY(sqlc.slice('title')) OR sqlc.slice('title') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("auto_apply" = ANY(sqlc.slice('auto_apply')) OR sqlc.slice('auto_apply') IS NULL) AND
    ("group" = ANY(sqlc.slice('group')) OR sqlc.slice('group') IS NULL) AND
    ("date_started" = ANY(sqlc.slice('date_started')) OR sqlc.slice('date_started') IS NULL) AND
    ("date_started" > sqlc.narg('date_started_from') OR sqlc.narg('date_started_from') IS NULL) AND
    ("date_started" < sqlc.narg('date_started_to') OR sqlc.narg('date_started_to') IS NULL) AND
    ("date_ended" = ANY(sqlc.slice('date_ended')) OR sqlc.slice('date_ended') IS NULL) AND
    ("date_ended" > sqlc.narg('date_ended_from') OR sqlc.narg('date_ended_from') IS NULL) AND
    ("date_ended" < sqlc.narg('date_ended_to') OR sqlc.narg('date_ended_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreatePromotion :one
INSERT INTO "promotion"."promotion" ("id", "code", "owner_id", "type", "title", "description", "is_active", "auto_apply", "group", "priority", "data", "date_started", "date_ended", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateBatchPromotion :batchone
INSERT INTO "promotion"."promotion" ("id", "code", "owner_id", "type", "title", "description", "is_active", "auto_apply", "group", "priority", "data", "date_started", "date_ended", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: CreateCopyPromotion :copyfrom
INSERT INTO "promotion"."promotion" ("id", "code", "owner_id", "type", "title", "description", "is_active", "auto_apply", "group", "priority", "data", "date_started", "date_ended", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: CreateDefaultPromotion :one
INSERT INTO "promotion"."promotion" ("code", "owner_id", "type", "title", "description", "is_active", "auto_apply", "group", "priority", "data", "date_started", "date_ended")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: CreateCopyDefaultPromotion :copyfrom
INSERT INTO "promotion"."promotion" ("code", "owner_id", "type", "title", "description", "is_active", "auto_apply", "group", "priority", "data", "date_started", "date_ended")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);

-- name: UpdatePromotion :one
UPDATE "promotion"."promotion"
SET "code" = COALESCE(sqlc.narg('code'), "code"),
    "owner_id" = CASE WHEN sqlc.arg('null_owner_id')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('owner_id'), "owner_id") END,
    "type" = COALESCE(sqlc.narg('type'), "type"),
    "title" = COALESCE(sqlc.narg('title'), "title"),
    "description" = CASE WHEN sqlc.arg('null_description')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('description'), "description") END,
    "is_active" = COALESCE(sqlc.narg('is_active'), "is_active"),
    "auto_apply" = COALESCE(sqlc.narg('auto_apply'), "auto_apply"),
    "group" = COALESCE(sqlc.narg('group'), "group"),
    "priority" = COALESCE(sqlc.narg('priority'), "priority"),
    "data" = CASE WHEN sqlc.arg('null_data')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('data'), "data") END,
    "date_started" = COALESCE(sqlc.narg('date_started'), "date_started"),
    "date_ended" = CASE WHEN sqlc.arg('null_date_ended')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('date_ended'), "date_ended") END,
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeletePromotion :exec
DELETE FROM "promotion"."promotion"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("owner_id" = ANY(sqlc.slice('owner_id')) OR sqlc.slice('owner_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("title" = ANY(sqlc.slice('title')) OR sqlc.slice('title') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("auto_apply" = ANY(sqlc.slice('auto_apply')) OR sqlc.slice('auto_apply') IS NULL) AND
    ("group" = ANY(sqlc.slice('group')) OR sqlc.slice('group') IS NULL) AND
    ("date_started" = ANY(sqlc.slice('date_started')) OR sqlc.slice('date_started') IS NULL) AND
    ("date_started" > sqlc.narg('date_started_from') OR sqlc.narg('date_started_from') IS NULL) AND
    ("date_started" < sqlc.narg('date_started_to') OR sqlc.narg('date_started_to') IS NULL) AND
    ("date_ended" = ANY(sqlc.slice('date_ended')) OR sqlc.slice('date_ended') IS NULL) AND
    ("date_ended" > sqlc.narg('date_ended_from') OR sqlc.narg('date_ended_from') IS NULL) AND
    ("date_ended" < sqlc.narg('date_ended_to') OR sqlc.narg('date_ended_to') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);
