-- name: GetProductSpuTag :one
SELECT *
FROM "catalog"."product_spu_tag"
WHERE ("id" = sqlc.narg('id')) OR ("spu_id" = sqlc.narg('spu_id') AND "tag" = sqlc.narg('tag'));

-- name: CountProductSpuTag :one
SELECT COUNT(*)
FROM "catalog"."product_spu_tag"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("tag" = ANY(sqlc.slice('tag')) OR sqlc.slice('tag') IS NULL)
);

-- name: ListProductSpuTag :many
SELECT *
FROM "catalog"."product_spu_tag"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("tag" = ANY(sqlc.slice('tag')) OR sqlc.slice('tag') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountProductSpuTag :many
SELECT sqlc.embed(embed_product_spu_tag), COUNT(*) OVER() as total_count
FROM "catalog"."product_spu_tag" embed_product_spu_tag
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("tag" = ANY(sqlc.slice('tag')) OR sqlc.slice('tag') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateProductSpuTag :one
INSERT INTO "catalog"."product_spu_tag" ("spu_id", "tag")
VALUES ($1, $2)
RETURNING *;

-- name: CreateBatchProductSpuTag :batchone
INSERT INTO "catalog"."product_spu_tag" ("spu_id", "tag")
VALUES ($1, $2)
RETURNING *;

-- name: CreateCopyProductSpuTag :copyfrom
INSERT INTO "catalog"."product_spu_tag" ("spu_id", "tag")
VALUES ($1, $2);

-- name: CreateDefaultProductSpuTag :one
INSERT INTO "catalog"."product_spu_tag" ("spu_id", "tag")
VALUES ($1, $2)
RETURNING *;

-- name: CreateCopyDefaultProductSpuTag :copyfrom
INSERT INTO "catalog"."product_spu_tag" ("spu_id", "tag")
VALUES ($1, $2);

-- name: UpdateProductSpuTag :one
UPDATE "catalog"."product_spu_tag"
SET "spu_id" = COALESCE(sqlc.narg('spu_id'), "spu_id"),
    "tag" = COALESCE(sqlc.narg('tag'), "tag")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteProductSpuTag :exec
DELETE FROM "catalog"."product_spu_tag"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("tag" = ANY(sqlc.slice('tag')) OR sqlc.slice('tag') IS NULL)
);