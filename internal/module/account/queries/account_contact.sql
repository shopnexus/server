-- name: GetContact :one
SELECT *
FROM "account"."contact"
WHERE ("id" = sqlc.narg('id'));

-- name: CountContact :one
SELECT COUNT(*)
FROM "account"."contact"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("full_name" = ANY(sqlc.slice('full_name')) OR sqlc.slice('full_name') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("phone_verified" = ANY(sqlc.slice('phone_verified')) OR sqlc.slice('phone_verified') IS NULL) AND
    ("address" = ANY(sqlc.slice('address')) OR sqlc.slice('address') IS NULL) AND
    ("address_type" = ANY(sqlc.slice('address_type')) OR sqlc.slice('address_type') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);

-- name: ListContact :many
SELECT *
FROM "account"."contact"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("full_name" = ANY(sqlc.slice('full_name')) OR sqlc.slice('full_name') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("phone_verified" = ANY(sqlc.slice('phone_verified')) OR sqlc.slice('phone_verified') IS NULL) AND
    ("address" = ANY(sqlc.slice('address')) OR sqlc.slice('address') IS NULL) AND
    ("address_type" = ANY(sqlc.slice('address_type')) OR sqlc.slice('address_type') IS NULL) AND
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

-- name: ListCountContact :many
SELECT sqlc.embed(embed_contact), COUNT(*) OVER() as total_count
FROM "account"."contact" embed_contact
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("full_name" = ANY(sqlc.slice('full_name')) OR sqlc.slice('full_name') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("phone_verified" = ANY(sqlc.slice('phone_verified')) OR sqlc.slice('phone_verified') IS NULL) AND
    ("address" = ANY(sqlc.slice('address')) OR sqlc.slice('address') IS NULL) AND
    ("address_type" = ANY(sqlc.slice('address_type')) OR sqlc.slice('address_type') IS NULL) AND
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

-- name: CreateContact :one
INSERT INTO "account"."contact" ("id", "account_id", "full_name", "phone", "phone_verified", "address", "address_type", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateBatchContact :batchone
INSERT INTO "account"."contact" ("id", "account_id", "full_name", "phone", "phone_verified", "address", "address_type", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateCopyContact :copyfrom
INSERT INTO "account"."contact" ("id", "account_id", "full_name", "phone", "phone_verified", "address", "address_type", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CreateDefaultContact :one
INSERT INTO "account"."contact" ("account_id", "full_name", "phone", "address", "address_type")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyDefaultContact :copyfrom
INSERT INTO "account"."contact" ("account_id", "full_name", "phone", "address", "address_type")
VALUES ($1, $2, $3, $4, $5);

-- name: UpdateContact :one
UPDATE "account"."contact"
SET "account_id" = COALESCE(sqlc.narg('account_id'), "account_id"),
    "full_name" = COALESCE(sqlc.narg('full_name'), "full_name"),
    "phone" = COALESCE(sqlc.narg('phone'), "phone"),
    "phone_verified" = COALESCE(sqlc.narg('phone_verified'), "phone_verified"),
    "address" = COALESCE(sqlc.narg('address'), "address"),
    "address_type" = COALESCE(sqlc.narg('address_type'), "address_type"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteContact :exec
DELETE FROM "account"."contact"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("full_name" = ANY(sqlc.slice('full_name')) OR sqlc.slice('full_name') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("phone_verified" = ANY(sqlc.slice('phone_verified')) OR sqlc.slice('phone_verified') IS NULL) AND
    ("address" = ANY(sqlc.slice('address')) OR sqlc.slice('address') IS NULL) AND
    ("address_type" = ANY(sqlc.slice('address_type')) OR sqlc.slice('address_type') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);