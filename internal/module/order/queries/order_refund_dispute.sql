-- name: GetRefundDispute :one
SELECT *
FROM "order"."refund_dispute"
WHERE ("id" = sqlc.narg('id'));

-- name: CountRefundDispute :one
SELECT COUNT(*)
FROM "order"."refund_dispute"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("refund_id" = ANY(sqlc.slice('refund_id')) OR sqlc.slice('refund_id') IS NULL) AND
    ("issued_by_id" = ANY(sqlc.slice('issued_by_id')) OR sqlc.slice('issued_by_id') IS NULL) AND
    ("reason" = ANY(sqlc.slice('reason')) OR sqlc.slice('reason') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);

-- name: ListRefundDispute :many
SELECT *
FROM "order"."refund_dispute"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("refund_id" = ANY(sqlc.slice('refund_id')) OR sqlc.slice('refund_id') IS NULL) AND
    ("issued_by_id" = ANY(sqlc.slice('issued_by_id')) OR sqlc.slice('issued_by_id') IS NULL) AND
    ("reason" = ANY(sqlc.slice('reason')) OR sqlc.slice('reason') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
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

-- name: ListCountRefundDispute :many
SELECT sqlc.embed(embed_refund_dispute), COUNT(*) OVER() as total_count
FROM "order"."refund_dispute" embed_refund_dispute
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("refund_id" = ANY(sqlc.slice('refund_id')) OR sqlc.slice('refund_id') IS NULL) AND
    ("issued_by_id" = ANY(sqlc.slice('issued_by_id')) OR sqlc.slice('issued_by_id') IS NULL) AND
    ("reason" = ANY(sqlc.slice('reason')) OR sqlc.slice('reason') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
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

-- name: CreateRefundDispute :one
INSERT INTO "order"."refund_dispute" ("id", "refund_id", "issued_by_id", "reason", "status", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateBatchRefundDispute :batchone
INSERT INTO "order"."refund_dispute" ("id", "refund_id", "issued_by_id", "reason", "status", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: CreateCopyRefundDispute :copyfrom
INSERT INTO "order"."refund_dispute" ("id", "refund_id", "issued_by_id", "reason", "status", "date_created", "date_updated")
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: CreateDefaultRefundDispute :one
INSERT INTO "order"."refund_dispute" ("refund_id", "issued_by_id", "reason")
VALUES ($1, $2, $3)
RETURNING *;

-- name: CreateCopyDefaultRefundDispute :copyfrom
INSERT INTO "order"."refund_dispute" ("refund_id", "issued_by_id", "reason")
VALUES ($1, $2, $3);

-- name: UpdateRefundDispute :one
UPDATE "order"."refund_dispute"
SET "refund_id" = COALESCE(sqlc.narg('refund_id'), "refund_id"),
    "issued_by_id" = COALESCE(sqlc.narg('issued_by_id'), "issued_by_id"),
    "reason" = COALESCE(sqlc.narg('reason'), "reason"),
    "status" = COALESCE(sqlc.narg('status'), "status"),
    "date_created" = COALESCE(sqlc.narg('date_created'), "date_created"),
    "date_updated" = COALESCE(sqlc.narg('date_updated'), "date_updated")
WHERE id = sqlc.arg('id')
RETURNING *;

-- name: DeleteRefundDispute :exec
DELETE FROM "order"."refund_dispute"
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("refund_id" = ANY(sqlc.slice('refund_id')) OR sqlc.slice('refund_id') IS NULL) AND
    ("issued_by_id" = ANY(sqlc.slice('issued_by_id')) OR sqlc.slice('issued_by_id') IS NULL) AND
    ("reason" = ANY(sqlc.slice('reason')) OR sqlc.slice('reason') IS NULL) AND
    ("status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL)
);