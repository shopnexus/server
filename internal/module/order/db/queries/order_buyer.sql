-- Buyer-side order list queries. Each returns paginated rows with embedded
-- COUNT(*) OVER() so the FE paginator gets total in one round trip. The three
-- queries partition orders into Cancelled > Completed > Pending mutual-exclusion
-- buckets via order.is_cancelled(). Joins payment_session twice (confirm + payout)
-- and transport once.

-- name: ListBuyerPendingOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND NOT "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
  AND ps_payout."status" <> 'Success'
  AND t."status" IS DISTINCT FROM 'Success'
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListBuyerCompletedOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND NOT "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;

-- name: ListBuyerCancelledOrders :many
SELECT sqlc.embed(embed_order), COUNT(*) OVER() AS total_count
FROM "order"."order" embed_order
LEFT JOIN "order"."payment_session" ps_confirm
       ON ps_confirm."id" = embed_order."confirm_session_id"
LEFT JOIN "order"."payment_session" ps_payout
       ON ps_payout."id" = embed_order."id" AND ps_payout."kind" = 'seller-payout'
LEFT JOIN "order"."transport" t ON t."id" = embed_order."transport_id"
WHERE embed_order."buyer_id" = @buyer_id
  AND "order".is_cancelled(ps_confirm."status", t."status", ps_payout."status")
ORDER BY embed_order."date_created" DESC
LIMIT sqlc.narg('limit')::int
OFFSET sqlc.narg('offset')::int;
