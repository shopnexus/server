-- name: GetVendorProductStats :one
SELECT
  (SELECT COUNT(*) FROM catalog.product_spu p WHERE p.account_id = @account_id AND p.date_deleted IS NULL)::bigint AS product_count,
  COALESCE((SELECT AVG(c.score) FROM catalog.comment c
   JOIN catalog.product_spu p ON c.ref_type = 'ProductSpu' AND c.ref_id = p.id
   WHERE p.account_id = @account_id AND p.date_deleted IS NULL), 0)::float8 AS average_rating;
