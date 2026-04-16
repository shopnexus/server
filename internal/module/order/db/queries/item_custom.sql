-- Custom item queries

-- name: ListPaidPendingItemsBySeller :many
SELECT i.* FROM "order".item i
JOIN "order".payment p ON p.id = i.payment_id
WHERE i.seller_id = @seller_id
  AND i.order_id IS NULL
  AND i.date_cancelled IS NULL
  AND p.status = 'Success'
ORDER BY i.date_created DESC
LIMIT @lim OFFSET @off;

-- name: CountPaidPendingItemsBySeller :one
SELECT COUNT(*) FROM "order".item i
JOIN "order".payment p ON p.id = i.payment_id
WHERE i.seller_id = @seller_id
  AND i.order_id IS NULL
  AND i.date_cancelled IS NULL
  AND p.status = 'Success';

-- name: ListPaidPendingItemsByBuyer :many
SELECT i.* FROM "order".item i
JOIN "order".payment p ON p.id = i.payment_id
WHERE i.account_id = @account_id
  AND i.order_id IS NULL
  AND i.date_cancelled IS NULL
  AND p.status = 'Success'
ORDER BY i.date_created DESC
LIMIT @lim OFFSET @off;

-- name: CountPaidPendingItemsByBuyer :one
SELECT COUNT(*) FROM "order".item i
JOIN "order".payment p ON p.id = i.payment_id
WHERE i.account_id = @account_id
  AND i.order_id IS NULL
  AND i.date_cancelled IS NULL
  AND p.status = 'Success';

-- name: ListPendingPaymentItemsByPaymentID :many
SELECT i.* FROM "order".item i
WHERE i.payment_id = @payment_id
  AND i.order_id IS NULL
  AND i.date_cancelled IS NULL;

-- name: CancelItemsByIDs :execrows
UPDATE "order".item
SET date_cancelled = now()
WHERE id = ANY(@item_ids::BIGINT[])
  AND order_id IS NULL
  AND date_cancelled IS NULL;

-- name: SetItemsOrderID :execrows
UPDATE "order".item
SET order_id = @order_id
WHERE id = ANY(@item_ids::BIGINT[])
  AND order_id IS NULL
  AND date_cancelled IS NULL;
