-- Custom dashboard aggregation queries for seller analytics

-- name: GetSellerOrderStats :one
-- Aggregates revenue, order count, and items sold for a seller within a date range.
-- Only counts orders with status 'Success'.
SELECT
    COALESCE(SUM(o."total"), 0)::bigint AS total_revenue,
    COUNT(o."id")::bigint AS total_orders,
    COALESCE(SUM(i.item_count), 0)::bigint AS items_sold
FROM "order"."order" o
LEFT JOIN LATERAL (
    SELECT SUM(it."quantity") AS item_count
    FROM "order"."item" it
    WHERE it."order_id" = o."id" AND it."status" = 'Confirmed'
) i ON true
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date;

-- name: GetSellerOrderTimeSeries :many
-- Returns time-bucketed revenue and order counts for chart data.
-- @granularity must be 'day', 'week', or 'month'.
SELECT
    date_trunc(@granularity::text, o."date_created")::timestamptz AS bucket,
    COALESCE(SUM(o."total"), 0)::bigint AS revenue,
    COUNT(o."id")::bigint AS order_count
FROM "order"."order" o
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date
GROUP BY bucket
ORDER BY bucket ASC;

-- name: GetSellerPendingActions :one
-- Counts unconfirmed incoming items and pending refunds for a seller.
SELECT
    (SELECT COUNT(*)::bigint FROM "order"."item"
     WHERE "item"."seller_id" = @seller_id AND "item"."status" = 'Pending' AND "item"."order_id" IS NULL) AS pending_items,
    (SELECT COUNT(*)::bigint FROM "order"."refund" r
     JOIN "order"."order" o ON r."order_id" = o."id"
     WHERE o."seller_id" = @seller_id AND r."status" = 'Pending') AS pending_refunds;

-- name: GetSellerTopProducts :many
-- Returns top products by sold quantity within a date range, with revenue.
SELECT
    i."sku_id",
    i."sku_name",
    SUM(i."quantity")::bigint AS sold_count,
    SUM(i."paid_amount")::bigint AS revenue
FROM "order"."item" i
JOIN "order"."order" o ON i."order_id" = o."id"
WHERE o."seller_id" = @seller_id
    AND o."status" = 'Success'
    AND o."date_created" >= @start_date
    AND o."date_created" < @end_date
GROUP BY i."sku_id", i."sku_name"
ORDER BY sold_count DESC
LIMIT @top_limit::int;
