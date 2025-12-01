-- name: GetBrand :one
SELECT *
FROM "catalog"."brand"
WHERE ("id" = sqlc.narg('id')) OR ("code" = sqlc.narg('code'));

-- name: CountBrand :one
SELECT COUNT(*)
FROM "catalog"."brand"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("name" = ANY(sqlc.slice('name')) OR sqlc.slice('name') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
);

-- name: ListBrand :many
SELECT *
FROM "catalog"."brand"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("name" = ANY(sqlc.slice('name')) OR sqlc.slice('name') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountBrand :many
SELECT sqlc.embed(embed_brand), COUNT(*) OVER() as total_count
FROM "catalog"."brand" embed_brand
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("name" = ANY(sqlc.slice('name')) OR sqlc.slice('name') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateBrand :one
INSERT INTO "catalog"."brand" ("id", "code", "name", "description")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateBatchBrand :batchone
INSERT INTO "catalog"."brand" ("id", "code", "name", "description")
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateCopyBrand :copyfrom
INSERT INTO "catalog"."brand" ("id", "code", "name", "description")
VALUES ($1, $2, $3, $4);

-- name: CreateDefaultBrand :one
INSERT INTO "catalog"."brand" ("code", "name", "description")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultBrand :copyfrom
INSERT INTO "catalog"."brand" ("code", "name", "description")
VALUES ($1, $2, $3);

-- name: UpdateBrand :one
UPDATE "catalog"."brand"
SET "code" = COALESCE(sqlc.narg('code'), "code"),
    "name" = COALESCE(sqlc.narg('name'), "name"),
    "description" = COALESCE(sqlc.narg('description'), "description")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteBrand :exec
DELETE FROM "catalog"."brand"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("code" = ANY(sqlc.slice('code')) OR sqlc.slice('code') IS NULL) AND
    ("name" = ANY(sqlc.slice('name')) OR sqlc.slice('name') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
);