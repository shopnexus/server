-- name: GetFavorite :one
SELECT *
FROM "account"."favorite"
WHERE "account_id" = sqlc.arg('account_id') AND "spu_id" = sqlc.arg('spu_id');

-- name: ListCountFavorite :many
SELECT sqlc.embed(embed_favorite), COUNT(*) OVER() as total_count
FROM "account"."favorite" embed_favorite
WHERE (
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("spu_id" = ANY(sqlc.slice('spu_id')) OR sqlc.slice('spu_id') IS NULL)
)
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: CreateDefaultFavorite :one
INSERT INTO "account"."favorite" ("account_id", "spu_id")
VALUES ($1, $2)
RETURNING *;

-- name: DeleteFavorite :exec
DELETE FROM "account"."favorite"
WHERE "account_id" = sqlc.arg('account_id') AND "spu_id" = sqlc.arg('spu_id');

-- name: CountFavoriteBySpuID :many
SELECT "spu_id", COUNT(*) as count
FROM "account"."favorite"
WHERE "spu_id" = ANY(sqlc.slice('spu_id'))
GROUP BY "spu_id";
