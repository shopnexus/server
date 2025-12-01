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
    COUNT(CASE WHEN score = 100 THEN 1 END) as five_count,
    COUNT(CASE WHEN score BETWEEN 80 AND 100 THEN 1 END) as four_count,
    COUNT(CASE WHEN score BETWEEN 60 AND 80 THEN 1 END) as three_count,
    COUNT(CASE WHEN score BETWEEN 40 AND 60 THEN 1 END) as two_count,
    COUNT(CASE WHEN score BETWEEN 0 AND 40 THEN 1 END) as one_count
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
    SELECT *
    FROM catalog.product_spu p
    WHERE p.id = ANY(sqlc.slice('spu_id')::bigint[])
),
spu_data AS (
    SELECT
        fs.*,
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
    spu.*,
    COALESCE(r.rating_total, 0) as rating_total,
    COALESCE(r.rating_score, 0) as rating_score
FROM spu_data spu
LEFT JOIN rating_data r ON spu.id = r.spu_id;


-- name: SearchCountProductSpu :many
SELECT sqlc.embed(embed_product_spu), COUNT(*) OVER() as total_count
FROM "catalog"."product_spu" embed_product_spu
WHERE (
    ("id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
    ("account_id" = ANY(sqlc.slice('account_id')) OR sqlc.slice('account_id') IS NULL) AND
    ("category_id" = ANY(sqlc.slice('category_id')) OR sqlc.slice('category_id') IS NULL) AND
    ("brand_id" = ANY(sqlc.slice('brand_id')) OR sqlc.slice('brand_id') IS NULL) AND
    ("featured_sku_id" = ANY(sqlc.slice('featured_sku_id')) OR sqlc.slice('featured_sku_id') IS NULL) AND
    ("is_active" = ANY(sqlc.slice('is_active')) OR sqlc.slice('is_active') IS NULL) AND
    ("date_created" = ANY(sqlc.slice('date_created')) OR sqlc.slice('date_created') IS NULL) AND
    ("date_created" > sqlc.narg('date_created_from') OR sqlc.narg('date_created_from') IS NULL) AND
    ("date_created" < sqlc.narg('date_created_to') OR sqlc.narg('date_created_to') IS NULL) AND
    ("date_updated" = ANY(sqlc.slice('date_updated')) OR sqlc.slice('date_updated') IS NULL) AND
    ("date_updated" > sqlc.narg('date_updated_from') OR sqlc.narg('date_updated_from') IS NULL) AND
    ("date_updated" < sqlc.narg('date_updated_to') OR sqlc.narg('date_updated_to') IS NULL) AND
    ("date_deleted" = NULL) AND (
      ("slug" ILIKE '%' || sqlc.narg('slug') || '%') OR
      ("name" ILIKE '%' || sqlc.narg('name') || '%') OR
      ("description" ILIKE '%' || sqlc.narg('description') || '%')
    )
)
ORDER BY "id"
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;