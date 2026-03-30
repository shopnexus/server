-- name: ListPopularProductPerCategory :many
SELECT
    c.id as category_id,
    p.id as spu_id
FROM "catalog"."category" c
CROSS JOIN LATERAL (
    SELECT ps.id
    FROM "catalog"."product_spu" ps
    WHERE ps.category_id = c.id
      AND ps.is_active = true
      AND ps.date_deleted IS NULL
    ORDER BY ps.date_created DESC
    LIMIT sqlc.arg('product_limit')::int
) p
WHERE c.id = ANY(sqlc.slice('category_id'));
