-- Custom product queries

-- name: SearchCountProductSpuByTags :many
-- Returns SPU IDs that have ALL of the specified tags (AND logic).
SELECT DISTINCT p.id, COUNT(*) OVER() as total_count
FROM "catalog"."product_spu" p
INNER JOIN "catalog"."product_spu_tag" t ON t.spu_id = p.id
WHERE p.date_deleted IS NULL
  AND t.tag = ANY(sqlc.slice('tags'))
GROUP BY p.id
HAVING COUNT(DISTINCT t.tag) = sqlc.arg('tag_count')::int
ORDER BY p.id
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;
