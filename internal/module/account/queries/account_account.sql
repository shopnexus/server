-- name: GetAccount :one
SELECT *
FROM "account"."account"
WHERE ("id" = sqlc.narg('id')) OR ("phone" = sqlc.narg('phone')) OR ("email" = sqlc.narg('email')) OR ("username" = sqlc.narg('username'));

-- name: CountAccount :one
SELECT COUNT(*)
FROM "account"."account"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("email" = ANY(sqlc.slice('email')) OR sqlc.slice('email') IS NULL) AND
    ("username" = ANY(sqlc.slice('username')) OR sqlc.slice('username') IS NULL) AND
    ("password" = ANY(sqlc.slice('password')) OR sqlc.slice('password') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);

-- name: ListAccount :many
SELECT *
FROM "account"."account"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("email" = ANY(sqlc.slice('email')) OR sqlc.slice('email') IS NULL) AND
    ("username" = ANY(sqlc.slice('username')) OR sqlc.slice('username') IS NULL) AND
    ("password" = ANY(sqlc.slice('password')) OR sqlc.slice('password') IS NULL) AND
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

-- name: ListCountAccount :many
SELECT sqlc.embed(embed_account), COUNT(*) OVER() as total_count
FROM "account"."account" embed_account
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("email" = ANY(sqlc.slice('email')) OR sqlc.slice('email') IS NULL) AND
    ("username" = ANY(sqlc.slice('username')) OR sqlc.slice('username') IS NULL) AND
    ("password" = ANY(sqlc.slice('password')) OR sqlc.slice('password') IS NULL) AND
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

-- name: CreateAccount :one
INSERT INTO "account"."account" ("id", "type", "status", "phone", "email", "username", "password", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateBatchAccount :batchone
INSERT INTO "account"."account" ("id", "type", "status", "phone", "email", "username", "password", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: CreateCopyAccount :copyfrom
INSERT INTO "account"."account" ("id", "type", "status", "phone", "email", "username", "password", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: CreateDefaultAccount :one
INSERT INTO "account"."account" ("type", "phone", "email", "username", "password")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyDefaultAccount :copyfrom
INSERT INTO "account"."account" ("type", "phone", "email", "username", "password")
VALUES ($1, $2, $3, $4, $5);

-- name: UpdateAccount :one
UPDATE "account"."account"
SET "type" = COALESCE(sqlc.narg('type'), "type"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "phone" = CASE WHEN sqlc.arg('null_phone')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('phone'), "phone") END,
    "email" = CASE WHEN sqlc.arg('null_email')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('email'), "email") END,
    "username" = CASE WHEN sqlc.arg('null_username')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('username'), "username") END,
    "password" = CASE WHEN sqlc.arg('null_password')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('password'), "password") END,
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteAccount :exec
DELETE FROM "account"."account"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("phone" = ANY(sqlc.slice('phone')) OR sqlc.slice('phone') IS NULL) AND
    ("email" = ANY(sqlc.slice('email')) OR sqlc.slice('email') IS NULL) AND
    ("username" = ANY(sqlc.slice('username')) OR sqlc.slice('username') IS NULL) AND
    ("password" = ANY(sqlc.slice('password')) OR sqlc.slice('password') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);