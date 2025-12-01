-- name: GetResourceReference :one
SELECT *
FROM "common"."resource_reference"
WHERE ("id" = sqlc.narg('id'));

-- name: CountResourceReference :one
SELECT COUNT(*)
FROM "common"."resource_reference"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("rs_id" = ANY(sqlc.slice('rs_id')) OR sqlc.slice('rs_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("order" = ANY(sqlc.slice('order')) OR sqlc.slice('order') IS NULL) AND
    ("order" > sqlc.narg('order_from') OR sqlc.narg('order_from') IS NULL) AND
    ("order" < sqlc.narg('order_to') OR sqlc.narg('order_to') IS NULL)
);

-- name: ListResourceReference :many
SELECT *
FROM "common"."resource_reference"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("rs_id" = ANY(sqlc.slice('rs_id')) OR sqlc.slice('rs_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("order" = ANY(sqlc.slice('order')) OR sqlc.slice('order') IS NULL) AND
    ("order" > sqlc.narg('order_from') OR sqlc.narg('order_from') IS NULL) AND
    ("order" < sqlc.narg('order_to') OR sqlc.narg('order_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountResourceReference :many
SELECT sqlc.embed(embed_resource_reference), COUNT(*) OVER() as total_count
FROM "common"."resource_reference" embed_resource_reference
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("rs_id" = ANY(sqlc.slice('rs_id')) OR sqlc.slice('rs_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("order" = ANY(sqlc.slice('order')) OR sqlc.slice('order') IS NULL) AND
    ("order" > sqlc.narg('order_from') OR sqlc.narg('order_from') IS NULL) AND
    ("order" < sqlc.narg('order_to') OR sqlc.narg('order_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateResourceReference :one
INSERT INTO "common"."resource_reference" ("rs_id", "ref_type", "ref_id", "order")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateBatchResourceReference :batchone
INSERT INTO "common"."resource_reference" ("rs_id", "ref_type", "ref_id", "order")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCopyResourceReference :copyfrom
INSERT INTO "common"."resource_reference" ("rs_id", "ref_type", "ref_id", "order")
VALUES ($1, $2, $3, $4);

-- name: CreateDefaultResourceReference :one
INSERT INTO "common"."resource_reference" ("rs_id", "ref_type", "ref_id", "order")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCopyDefaultResourceReference :copyfrom
INSERT INTO "common"."resource_reference" ("rs_id", "ref_type", "ref_id", "order")
VALUES ($1, $2, $3, $4);

-- name: UpdateResourceReference :one
UPDATE "common"."resource_reference"
SET "rs_id" = COALESCE(sqlc.narg('rs_id'), "rs_id"),
    "ref_type" = COALESCE(sqlc.narg('ref_type'), "ref_type"),
    "ref_id" = COALESCE(sqlc.narg('ref_id'), "ref_id"),
    "order" = COALESCE(sqlc.narg('order'), "order")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteResourceReference :exec
DELETE FROM "common"."resource_reference"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("rs_id" = ANY(sqlc.slice('rs_id')) OR sqlc.slice('rs_id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("order" = ANY(sqlc.slice('order')) OR sqlc.slice('order') IS NULL) AND
    ("order" > sqlc.narg('order_from') OR sqlc.narg('order_from') IS NULL) AND
    ("order" < sqlc.narg('order_to') OR sqlc.narg('order_to') IS NULL)
);