-- name: GetFlagshipProduct :many
SELECT s.*
FROM unnest(sqlc.slice('spu_id')::bigint[]) AS u(spu_id)
JOIN LATERAL (
    SELECT sku.*, sku.id as sku_id, st.sold
    FROM "catalog"."product_sku" sku
    INNER JOIN "inventory"."stock" st ON sku.id = st.ref_id AND st.ref_type = 'ProductSku'
    WHERE sku.spu_id = u.spu_id
    ORDER BY st.sold DESC, sku.price ASC LIMIT 1
) s ON true;

-- name: ListRating :many
SELECT ref_id, AVG(score) as score, COUNT(*) as count
FROM "catalog"."comment"
WHERE (
    ref_type = sqlc.arg('ref_type') AND
    ref_id = ANY(sqlc.slice('ref_id'))
)
GROUP BY ref_id;

-- name: DetailRating :one
SELECT ref_id, AVG(score) as score, COUNT(*) as count,
    COUNT(CASE WHEN score = 10 THEN 1 END) as five_count,
    COUNT(CASE WHEN score BETWEEN 8 AND 9 THEN 1 END) as four_count,
    COUNT(CASE WHEN score BETWEEN 6 AND 7 THEN 1 END) as three_count,
    COUNT(CASE WHEN score BETWEEN 4 AND 5 THEN 1 END) as two_count,
    COUNT(CASE WHEN score BETWEEN 1 AND 3 THEN 1 END) as one_count
FROM "catalog"."comment"
WHERE (
    ref_type = sqlc.arg('ref_type') AND
    ref_id = sqlc.arg('ref_id')
)
GROUP BY ref_id;

-- name: GetAvailableProducts :many
SELECT s.id, s.sku_id, s.serial_number
FROM unnest(sqlc.slice('sku_id')::bigint[]) AS u(sku_id)
JOIN LATERAL (
    SELECT id, sku_id, serial_number
    FROM "inventory"."sku_serial"
    WHERE sku_id = u.sku_id AND "status" = 'Active'
    ORDER BY date_created DESC LIMIT 5
) s ON true;