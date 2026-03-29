-- name: ListCountProductSpuRecent :many
-- Same as ListCountProductSpu but ordered by date_created DESC (newest first)
SELECT sqlc.embed(embed_product_spu), COUNT(*) OVER() as total_count
FROM "catalog"."product_spu" embed_product_spu
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("slug" = ANY(sqlc.slice('slug')) OR sqlc.slice('slug') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("category_id" = ANY(sqlc.slice('category_id')) OR sqlc.slice('category_id') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("date_deleted" IS NULL)
)
ORDER BY "date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;
