-- name: GetSearchSync :one
SELECT *
FROM "catalog"."search_sync"
WHERE ("id" = sqlc.narg('id')) OR ("ref_type" = sqlc.narg('ref_type') AND "ref_id" = sqlc.narg('ref_id'));

-- name: CountSearchSync :one
SELECT COUNT(*)
FROM "catalog"."search_sync"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("is_stale_embedding" = ANY(sqlc.slice('is_stale_embedding')) OR sqlc.slice('is_stale_embedding') IS NULL) AND
    ("is_stale_metadata" = ANY(sqlc.slice('is_stale_metadata')) OR sqlc.slice('is_stale_metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);

-- name: ListSearchSync :many
SELECT *
FROM "catalog"."search_sync"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("is_stale_embedding" = ANY(sqlc.slice('is_stale_embedding')) OR sqlc.slice('is_stale_embedding') IS NULL) AND
    ("is_stale_metadata" = ANY(sqlc.slice('is_stale_metadata')) OR sqlc.slice('is_stale_metadata') IS NULL) AND
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

-- name: ListCountSearchSync :many
SELECT sqlc.embed(embed_search_sync), COUNT(*) OVER() as total_count
FROM "catalog"."search_sync" embed_search_sync
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("is_stale_embedding" = ANY(sqlc.slice('is_stale_embedding')) OR sqlc.slice('is_stale_embedding') IS NULL) AND
    ("is_stale_metadata" = ANY(sqlc.slice('is_stale_metadata')) OR sqlc.slice('is_stale_metadata') IS NULL) AND
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

-- name: CreateSearchSync :one
INSERT INTO "catalog"."search_sync" ("ref_type", "ref_id", "is_stale_embedding", "is_stale_metadata", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateBatchSearchSync :batchone
INSERT INTO "catalog"."search_sync" ("ref_type", "ref_id", "is_stale_embedding", "is_stale_metadata", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopySearchSync :copyfrom
INSERT INTO "catalog"."search_sync" ("ref_type", "ref_id", "is_stale_embedding", "is_stale_metadata", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreateDefaultSearchSync :one
INSERT INTO "catalog"."search_sync" ("ref_type", "ref_id")
VALUES ($1, $2)
RETURNING *;

-- name: CreateCopyDefaultSearchSync :copyfrom
INSERT INTO "catalog"."search_sync" ("ref_type", "ref_id")
VALUES ($1, $2);

-- name: UpdateSearchSync :one
UPDATE "catalog"."search_sync"
SET "ref_type" = COALESCE(sqlc.narg('ref_type'), "ref_type"),
    "ref_id" = COALESCE(sqlc.narg('ref_id'), "ref_id"),
    "is_stale_embedding" = COALESCE(sqlc.narg('is_stale_embedding'), "is_stale_embedding"),
    "is_stale_metadata" = COALESCE(sqlc.narg('is_stale_metadata'), "is_stale_metadata"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteSearchSync :exec
DELETE FROM "catalog"."search_sync"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("ref_type" = ANY(sqlc.slice('ref_type')) OR sqlc.slice('ref_type') IS NULL) AND
    ("ref_id" = ANY(sqlc.slice('ref_id')) OR sqlc.slice('ref_id') IS NULL) AND
    ("is_stale_embedding" = ANY(sqlc.slice('is_stale_embedding')) OR sqlc.slice('is_stale_embedding') IS NULL) AND
    ("is_stale_metadata" = ANY(sqlc.slice('is_stale_metadata')) OR sqlc.slice('is_stale_metadata') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);