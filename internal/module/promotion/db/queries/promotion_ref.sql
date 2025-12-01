-- name: GetRef :one
SELECT *
FROM "promotion"."ref"
WHERE ("id" = sqlc.narg('id')) OR ("promotion_id" = sqlc.narg('promotion_id') AND "ref_type" = sqlc.narg('ref_type') AND "ref_id" = sqlc.narg('ref_id'));

-- name: CountRef :one
SELECT COUNT(*)
FROM "promotion"."ref"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("promotion_id" = ANY(sqlc.slice('promotion_id')) OR sqlc.slice('promotion_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL)
);

-- name: ListRef :many
SELECT *
FROM "promotion"."ref"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("promotion_id" = ANY(sqlc.slice('promotion_id')) OR sqlc.slice('promotion_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountRef :many
SELECT sqlc.embed(embed_ref), COUNT(*) OVER() as total_count
FROM "promotion"."ref" embed_ref
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("promotion_id" = ANY(sqlc.slice('promotion_id')) OR sqlc.slice('promotion_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateRef :one
INSERT INTO "promotion"."ref" ("promotion_id", "ref_type", "ref_id")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateBatchRef :batchone
INSERT INTO "promotion"."ref" ("promotion_id", "ref_type", "ref_id")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyRef :copyfrom
INSERT INTO "promotion"."ref" ("promotion_id", "ref_type", "ref_id")
VALUES ($1, $2, $3);

-- name: CreateDefaultRef :one
INSERT INTO "promotion"."ref" ("promotion_id", "ref_type", "ref_id")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultRef :copyfrom
INSERT INTO "promotion"."ref" ("promotion_id", "ref_type", "ref_id")
VALUES ($1, $2, $3);

-- name: UpdateRef :one
UPDATE "promotion"."ref"
SET "promotion_id" = COALESCE(sqlc.narg('promotion_id'), "promotion_id"),
    "ref_type" = COALESCE(sqlc.narg('ref_type'), "ref_type"),
    "ref_id" = COALESCE(sqlc.narg('ref_id'), "ref_id")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteRef :exec
DELETE FROM "promotion"."ref"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("promotion_id" = ANY(sqlc.slice('promotion_id')) OR sqlc.slice('promotion_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL)
);