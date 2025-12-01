-- name: GetVendor :one
SELECT *
FROM "account"."vendor"
WHERE ("id" = sqlc.narg('id'));

-- name: CountVendor :one
SELECT COUNT(*)
FROM "account"."vendor"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
);

-- name: ListVendor :many
SELECT *
FROM "account"."vendor"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountVendor :many
SELECT sqlc.embed(embed_vendor), COUNT(*) OVER() as total_count
FROM "account"."vendor" embed_vendor
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateVendor :one
INSERT INTO "account"."vendor" ("id", "description")
VALUES ($1, $2)
RETURNING *;

-- name: CreateBatchVendor :batchone
INSERT INTO "account"."vendor" ("id", "description")
VALUES ($1, $2)
RETURNING *;

-- name: CreateCopyVendor :copyfrom
INSERT INTO "account"."vendor" ("id", "description")
VALUES ($1, $2);

-- name: CreateDefaultVendor :one
INSERT INTO "account"."vendor" ("id")
VALUES ($1)
RETURNING *;

-- name: CreateCopyDefaultVendor :copyfrom
INSERT INTO "account"."vendor" ("id")
VALUES ($1);

-- name: UpdateVendor :one
UPDATE "account"."vendor"
SET "description" = COALESCE(sqlc.narg('description'), "description")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteVendor :exec
DELETE FROM "account"."vendor"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("description" = ANY(sqlc.slice('description')) OR sqlc.slice('description') IS NULL)
);