-- name: ListCountVendorOrder :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() as total_count
FROM "order"."order" embed_order
INNER JOIN "order"."payment" payment ON embed_order."payment_id" = payment."id"
WHERE payment.status = 'Success' AND (embed_order."vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) 
ORDER BY embed_order."date_created" ASC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;