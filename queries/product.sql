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

-- name: ListMostSoldProducts :many
WITH ranked_spus AS (
    SELECT
        spu.id AS spu_id,
        SUM(s.sold) AS total_sold
    FROM catalog.product_spu AS spu
    JOIN catalog.product_sku AS sku
        ON sku.spu_id = spu.id
    JOIN inventory.stock AS s
        ON s.ref_id = sku.id AND s.ref_type = 'ProductSku'
    GROUP BY spu.id
    ORDER BY total_sold DESC
    LIMIT sqlc.arg('top_n')
)
SELECT *
FROM ranked_spus
ORDER BY RANDOM()  -- randomize within the top set
LIMIT sqlc.arg('limit');

-- name: ListProductDetail :many
WITH filtered_spu AS (
    SELECT 
        p.id,
        p.account_id,
        p.category_id,
        p.brand_id,
        p.name,
        p.description,
        p.is_active,
        p.date_manufactured,
        p.date_created,
        p.date_updated,
        p.date_deleted
    FROM catalog.product_spu p
    WHERE p.id = ANY(sqlc.slice('spu_id')::bigint[])
),
spu_data AS (
    SELECT 
        fs.id,
        fs.name,
        fs.description,
        fs.is_active,
        fs.date_manufactured,
        fs.date_created,
        fs.date_updated,
        fs.date_deleted,
        COALESCE(vp.name, '') as vendor_name,
        c.name as category_name,
        b.name as brand_name
    FROM filtered_spu fs
    JOIN account.vendor v ON fs.account_id = v.id
    LEFT JOIN account.profile vp ON v.id = vp.id
    JOIN catalog.category c ON fs.category_id = c.id
    JOIN catalog.brand b ON fs.brand_id = b.id
),
rating_data AS (
    SELECT 
        ref_id as spu_id,
        COUNT(*) as rating_total,
        AVG(score) as rating_score
    FROM catalog.comment 
    WHERE ref_type = 'ProductSpu' 
    AND ref_id = ANY(sqlc.slice('spu_id')::bigint[])
    GROUP BY ref_id
)
SELECT 
    spu.id as id,
    spu.vendor_name as vendor,
    spu.category_name as category,
    spu.brand_name as brand,
    spu.name,
    spu.description,
    spu.is_active,
    spu.date_manufactured,
    spu.date_created,
    spu.date_updated,
    spu.date_deleted,
    COALESCE(r.rating_total, 0) as rating_total,
    COALESCE(r.rating_score, 0) as rating_score
FROM spu_data spu
LEFT JOIN rating_data r ON spu.id = r.spu_id;
