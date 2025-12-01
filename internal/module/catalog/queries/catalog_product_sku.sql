-- name: GetProductSku :one
SELECT *
FROM "catalog"."product_sku"
WHERE ("id" = sqlc.narg('id'));

-- name: CountProductSku :one
SELECT COUNT(*)
FROM "catalog"."product_sku"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("price" = ANY(sqlc.slice('price')) OR sqlc.slice('price') IS NULL) AND
    ("price" > sqlc.narg('price_from') OR sqlc.narg('price_from') IS NULL) AND
    ("price" < sqlc.narg('price_to') OR sqlc.narg('price_to') IS NULL) AND
    ("can_combine" = ANY(sqlc.slice('can_combine')) OR sqlc.slice('can_combine') IS NULL) AND
    ("attributes" = ANY(sqlc.slice('attributes')) OR sqlc.slice('attributes') IS NULL) AND
    ("package_details" = ANY(sqlc.slice('package_details')) OR sqlc.slice('package_details') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_deleted" = ANY(sqlc.slice('date_deleted')) OR sqlc.slice('date_deleted') IS NULL) AND
    ("date_deleted" > sqlc.narg('date_deleted_from') OR sqlc.narg('date_deleted_from') IS NULL) AND
    ("date_deleted" < sqlc.narg('date_deleted_to') OR sqlc.narg('date_deleted_to') IS NULL)
);

-- name: ListProductSku :many
SELECT *
FROM "catalog"."product_sku"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("price" = ANY(sqlc.slice('price')) OR sqlc.slice('price') IS NULL) AND
    ("price" > sqlc.narg('price_from') OR sqlc.narg('price_from') IS NULL) AND
    ("price" < sqlc.narg('price_to') OR sqlc.narg('price_to') IS NULL) AND
    ("can_combine" = ANY(sqlc.slice('can_combine')) OR sqlc.slice('can_combine') IS NULL) AND
    ("attributes" = ANY(sqlc.slice('attributes')) OR sqlc.slice('attributes') IS NULL) AND
    ("package_details" = ANY(sqlc.slice('package_details')) OR sqlc.slice('package_details') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_deleted" = ANY(sqlc.slice('date_deleted')) OR sqlc.slice('date_deleted') IS NULL) AND
    ("date_deleted" > sqlc.narg('date_deleted_from') OR sqlc.narg('date_deleted_from') IS NULL) AND
    ("date_deleted" < sqlc.narg('date_deleted_to') OR sqlc.narg('date_deleted_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountProductSku :many
SELECT sqlc.embed(embed_product_sku), COUNT(*) OVER() as total_count
FROM "catalog"."product_sku" embed_product_sku
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("price" = ANY(sqlc.slice('price')) OR sqlc.slice('price') IS NULL) AND
    ("price" > sqlc.narg('price_from') OR sqlc.narg('price_from') IS NULL) AND
    ("price" < sqlc.narg('price_to') OR sqlc.narg('price_to') IS NULL) AND
    ("can_combine" = ANY(sqlc.slice('can_combine')) OR sqlc.slice('can_combine') IS NULL) AND
    ("attributes" = ANY(sqlc.slice('attributes')) OR sqlc.slice('attributes') IS NULL) AND
    ("package_details" = ANY(sqlc.slice('package_details')) OR sqlc.slice('package_details') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_deleted" = ANY(sqlc.slice('date_deleted')) OR sqlc.slice('date_deleted') IS NULL) AND
    ("date_deleted" > sqlc.narg('date_deleted_from') OR sqlc.narg('date_deleted_from') IS NULL) AND
    ("date_deleted" < sqlc.narg('date_deleted_to') OR sqlc.narg('date_deleted_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateProductSku :one
INSERT INTO "catalog"."product_sku" ("id", "spu_id", "price", "can_combine", "attributes", "package_details", "date_created", "date_deleted")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateBatchProductSku :batchone
INSERT INTO "catalog"."product_sku" ("id", "spu_id", "price", "can_combine", "attributes", "package_details", "date_created", "date_deleted")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateCopyProductSku :copyfrom
INSERT INTO "catalog"."product_sku" ("id", "spu_id", "price", "can_combine", "attributes", "package_details", "date_created", "date_deleted")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: CreateDefaultProductSku :one
INSERT INTO "catalog"."product_sku" ("spu_id", "price", "can_combine", "attributes", "package_details", "date_deleted")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopyDefaultProductSku :copyfrom
INSERT INTO "catalog"."product_sku" ("spu_id", "price", "can_combine", "attributes", "package_details", "date_deleted")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateProductSku :one
UPDATE "catalog"."product_sku"
SET "spu_id" = COALESCE(sqlc.narg('spu_id'), "spu_id"),
    "price" = COALESCE(sqlc.narg('price'), "price"),
    "can_combine" = COALESCE(sqlc.narg('can_combine'), "can_combine"),
    "attributes" = COALESCE(sqlc.narg('attributes'), "attributes"),
    "package_details" = COALESCE(sqlc.narg('package_details'), "package_details"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_deleted" = CASE WHEN sqlc.arg('null_date_deleted')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('date_deleted'), "date_deleted") END
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteProductSku :exec
DELETE FROM "catalog"."product_sku"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL) AND
    ("price" = ANY(sqlc.slice('price')) OR sqlc.slice('price') IS NULL) AND
    ("price" > sqlc.narg('price_from') OR sqlc.narg('price_from') IS NULL) AND
    ("price" < sqlc.narg('price_to') OR sqlc.narg('price_to') IS NULL) AND
    ("can_combine" = ANY(sqlc.slice('can_combine')) OR sqlc.slice('can_combine') IS NULL) AND
    ("attributes" = ANY(sqlc.slice('attributes')) OR sqlc.slice('attributes') IS NULL) AND
    ("package_details" = ANY(sqlc.slice('package_details')) OR sqlc.slice('package_details') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_deleted" = ANY(sqlc.slice('date_deleted')) OR sqlc.slice('date_deleted') IS NULL) AND
    ("date_deleted" > sqlc.narg('date_deleted_from') OR sqlc.narg('date_deleted_from') IS NULL) AND
    ("date_deleted" < sqlc.narg('date_deleted_to') OR sqlc.narg('date_deleted_to') IS NULL)
);