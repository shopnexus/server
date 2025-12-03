-- name: GetSerial :one
SELECT *
FROM "inventory"."serial"
WHERE ("id" = sqlc.narg('id'));

-- name: CountSerial :one
SELECT COUNT(*)
FROM "inventory"."serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListSerial :many
SELECT *
FROM "inventory"."serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountSerial :many
SELECT sqlc.embed(embed_serial), COUNT(*) OVER() as total_count
FROM "inventory"."serial" embed_serial
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateSerial :one
INSERT INTO "inventory"."serial" ("id", "ref_type", "ref_id", "status", "date_created")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateBatchSerial :batchone
INSERT INTO "inventory"."serial" ("id", "ref_type", "ref_id", "status", "date_created")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopySerial :copyfrom
INSERT INTO "inventory"."serial" ("id", "ref_type", "ref_id", "status", "date_created")
VALUES ($1, $2, $3, $4, $5);

-- name: CreateDefaultSerial :one
INSERT INTO "inventory"."serial" ("id", "ref_type", "ref_id")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultSerial :copyfrom
INSERT INTO "inventory"."serial" ("id", "ref_type", "ref_id")
VALUES ($1, $2, $3);

-- name: UpdateSerial :one
UPDATE "inventory"."serial"
SET "ref_type" = COALESCE(sqlc.narg('ref_type'), "ref_type"),
    "ref_id" = COALESCE(sqlc.narg('ref_id'), "ref_id"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSerial :exec
DELETE FROM "inventory"."serial"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);