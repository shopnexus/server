-- name: GetIncomeHistory :one
SELECT *
FROM "account"."income_history"
WHERE ("id" = sqlc.narg('id'));

-- name: CountIncomeHistory :one
SELECT COUNT(*)
FROM "account"."income_history"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("income" = ANY(sqlc.slice('income')) OR sqlc.slice('income') IS NULL) AND
    ("income" > sqlc.narg('income_from') OR sqlc.narg('income_from') IS NULL) AND
    ("income" < sqlc.narg('income_to') OR sqlc.narg('income_to') IS NULL) AND
    ("current_balance" = ANY(sqlc.slice('current_balance')) OR sqlc.slice('current_balance') IS NULL) AND
    ("current_balance" > sqlc.narg('current_balance_from') OR sqlc.narg('current_balance_from') IS NULL) AND
    ("current_balance" < sqlc.narg('current_balance_to') OR sqlc.narg('current_balance_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);

-- name: ListIncomeHistory :many
SELECT *
FROM "account"."income_history"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("income" = ANY(sqlc.slice('income')) OR sqlc.slice('income') IS NULL) AND
    ("income" > sqlc.narg('income_from') OR sqlc.narg('income_from') IS NULL) AND
    ("income" < sqlc.narg('income_to') OR sqlc.narg('income_to') IS NULL) AND
    ("current_balance" = ANY(sqlc.slice('current_balance')) OR sqlc.slice('current_balance') IS NULL) AND
    ("current_balance" > sqlc.narg('current_balance_from') OR sqlc.narg('current_balance_from') IS NULL) AND
    ("current_balance" < sqlc.narg('current_balance_to') OR sqlc.narg('current_balance_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListCountIncomeHistory :many
SELECT sqlc.embed(embed_income_history), COUNT(*) OVER() as total_count
FROM "account"."income_history" embed_income_history
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("income" = ANY(sqlc.slice('income')) OR sqlc.slice('income') IS NULL) AND
    ("income" > sqlc.narg('income_from') OR sqlc.narg('income_from') IS NULL) AND
    ("income" < sqlc.narg('income_to') OR sqlc.narg('income_to') IS NULL) AND
    ("current_balance" = ANY(sqlc.slice('current_balance')) OR sqlc.slice('current_balance') IS NULL) AND
    ("current_balance" > sqlc.narg('current_balance_from') OR sqlc.narg('current_balance_from') IS NULL) AND
    ("current_balance" < sqlc.narg('current_balance_to') OR sqlc.narg('current_balance_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateIncomeHistory :one
INSERT INTO "account"."income_history" ("account_id", "type", "income", "current_balance", "note", "date_created")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateBatchIncomeHistory :batchone
INSERT INTO "account"."income_history" ("account_id", "type", "income", "current_balance", "note", "date_created")
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: CreateCopyIncomeHistory :copyfrom
INSERT INTO "account"."income_history" ("account_id", "type", "income", "current_balance", "note", "date_created")
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CreateDefaultIncomeHistory :one
INSERT INTO "account"."income_history" ("account_id", "type", "income", "current_balance", "note")
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: CreateCopyDefaultIncomeHistory :copyfrom
INSERT INTO "account"."income_history" ("account_id", "type", "income", "current_balance", "note")
VALUES ($1, $2, $3, $4, $5);

-- name: UpdateIncomeHistory :one
UPDATE "account"."income_history"
SET "account_id" = COALESCE(sqlc.narg('account_id'), "account_id"),
    "type" = COALESCE(sqlc.narg('type'), "type"),
    "income" = COALESCE(sqlc.narg('income'), "income"),
    "current_balance" = COALESCE(sqlc.narg('current_balance'), "current_balance"),
    "note" = CASE WHEN sqlc.arg('null_note')::bool = TRUE THEN NULL ELSE COALESCE(sqlc.narg('note'), "note") END,
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteIncomeHistory :exec
DELETE FROM "account"."income_history"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("type" = ANY(sqlc.slice('type')) OR sqlc.slice('type') IS NULL) AND
    ("income" = ANY(sqlc.slice('income')) OR sqlc.slice('income') IS NULL) AND
    ("income" > sqlc.narg('income_from') OR sqlc.narg('income_from') IS NULL) AND
    ("income" < sqlc.narg('income_to') OR sqlc.narg('income_to') IS NULL) AND
    ("current_balance" = ANY(sqlc.slice('current_balance')) OR sqlc.slice('current_balance') IS NULL) AND
    ("current_balance" > sqlc.narg('current_balance_from') OR sqlc.narg('current_balance_from') IS NULL) AND
    ("current_balance" < sqlc.narg('current_balance_to') OR sqlc.narg('current_balance_to') IS NULL) AND
    ("note" = ANY(sqlc.slice('note')) OR sqlc.slice('note') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL)
);