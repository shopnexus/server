-- name: GetPayment :one
SELECT *
FROM "order"."payment"
WHERE ("id" = sqlc.narg('id'));

-- name: CountPayment :one
SELECT COUNT(*)
FROM "order"."payment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("amount" = ANY(sqlc.slice('amount')) OR sqlc.slice('amount') IS NULL) AND
    ("amount" > sqlc.narg('amount_from') OR sqlc.narg('amount_from') IS NULL) AND
    ("amount" < sqlc.narg('amount_to') OR sqlc.narg('amount_to') IS NULL) AND
    ("data" = ANY(sqlc.slice('data')) OR sqlc.slice('data') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_paid" = ANY(sqlc.slice('date_paid')) OR sqlc.slice('date_paid') IS NULL) AND
    ("date_expired" = ANY(sqlc.slice('date_expired')) OR sqlc.slice('date_expired') IS NULL) AND
    ("date_expired" > sqlc.narg('date_expired_from') OR sqlc.narg('date_expired_from') IS NULL) AND
    ("date_expired" < sqlc.narg('date_expired_to') OR sqlc.narg('date_expired_to') IS NULL)
);

-- name: ListPayment :many
SELECT *
FROM "order"."payment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("amount" = ANY(sqlc.slice('amount')) OR sqlc.slice('amount') IS NULL) AND
    ("amount" > sqlc.narg('amount_from') OR sqlc.narg('amount_from') IS NULL) AND
    ("amount" < sqlc.narg('amount_to') OR sqlc.narg('amount_to') IS NULL) AND
    ("data" = ANY(sqlc.slice('data')) OR sqlc.slice('data') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_paid" = ANY(sqlc.slice('date_paid')) OR sqlc.slice('date_paid') IS NULL) AND
    ("date_expired" = ANY(sqlc.slice('date_expired')) OR sqlc.slice('date_expired') IS NULL) AND
    ("date_expired" > sqlc.narg('date_expired_from') OR sqlc.narg('date_expired_from') IS NULL) AND
    ("date_expired" < sqlc.narg('date_expired_to') OR sqlc.narg('date_expired_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountPayment :many
SELECT sqlc.embed(embed_payment), COUNT(*) OVER() as total_count
FROM "order"."payment" embed_payment
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("amount" = ANY(sqlc.slice('amount')) OR sqlc.slice('amount') IS NULL) AND
    ("amount" > sqlc.narg('amount_from') OR sqlc.narg('amount_from') IS NULL) AND
    ("amount" < sqlc.narg('amount_to') OR sqlc.narg('amount_to') IS NULL) AND
    ("data" = ANY(sqlc.slice('data')) OR sqlc.slice('data') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_paid" = ANY(sqlc.slice('date_paid')) OR sqlc.slice('date_paid') IS NULL) AND
    ("date_expired" = ANY(sqlc.slice('date_expired')) OR sqlc.slice('date_expired') IS NULL) AND
    ("date_expired" > sqlc.narg('date_expired_from') OR sqlc.narg('date_expired_from') IS NULL) AND
    ("date_expired" < sqlc.narg('date_expired_to') OR sqlc.narg('date_expired_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreatePayment :one
INSERT INTO "order"."payment" ("account_id", "option", "status", "amount", "data", "date_created", "date_paid", "date_expired")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateBatchPayment :batchone
INSERT INTO "order"."payment" ("account_id", "option", "status", "amount", "data", "date_created", "date_paid", "date_expired")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: CreateCopyPayment :copyfrom
INSERT INTO "order"."payment" ("account_id", "option", "status", "amount", "data", "date_created", "date_paid", "date_expired")
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: CreateDefaultPayment :one
INSERT INTO "order"."payment" ("account_id", "option", "amount", "data", "date_paid", "date_expired")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopyDefaultPayment :copyfrom
INSERT INTO "order"."payment" ("account_id", "option", "amount", "data", "date_paid", "date_expired")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdatePayment :one
UPDATE "order"."payment"
SET "account_id" = COALESCE(sqlc.narg('account_id'), "account_id"),
    "option" = COALESCE(sqlc.narg('option'), "option"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "amount" = COALESCE(sqlc.narg('amount'), "amount"),
    "data" = COALESCE(sqlc.narg('data'), "data"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_paid" = CASE WHEN sqlc.arg('null_date_paid')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('date_paid'), "date_paid") END,
    "date_expired" = COALESCE(sqlc.narg('date_expired'), "date_expired")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeletePayment :exec
DELETE FROM "order"."payment"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("option" = ANY(sqlc.slice('option')) OR sqlc.slice('option') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("amount" = ANY(sqlc.slice('amount')) OR sqlc.slice('amount') IS NULL) AND
    ("amount" > sqlc.narg('amount_from') OR sqlc.narg('amount_from') IS NULL) AND
    ("amount" < sqlc.narg('amount_to') OR sqlc.narg('amount_to') IS NULL) AND
    ("data" = ANY(sqlc.slice('data')) OR sqlc.slice('data') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_paid" = ANY(sqlc.slice('date_paid')) OR sqlc.slice('date_paid') IS NULL) AND
    ("date_expired" = ANY(sqlc.slice('date_expired')) OR sqlc.slice('date_expired') IS NULL) AND
    ("date_expired" > sqlc.narg('date_expired_from') OR sqlc.narg('date_expired_from') IS NULL) AND
    ("date_expired" < sqlc.narg('date_expired_to') OR sqlc.narg('date_expired_to') IS NULL)
);