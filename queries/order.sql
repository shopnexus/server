-- name: ExistsCartItems :one
SELECT EXISTS(
    SELECT 1 FROM account.cart_item WHERE sku_id = ANY(sqlc.arg('sku_ids')::bigint[])
) AS "exists";

-- name: GetVendorAddressBySkuIDs :many
SELECT DISTINCT
    p.id as vendor_id,
    c.address,
    sku.id as sku_id
FROM catalog.product_sku AS sku
JOIN catalog.product_spu AS spu ON spu.id = sku.spu_id
JOIN account.profile AS p ON p.id = spu.account_id
JOIN account.contact AS c ON c.id = p.default_contact_id
WHERE sku.id = ANY(sqlc.slice('sku_ids'));

-- name: ListVendorOrderItem :many
WITH vendor_orders AS (
    SELECT oi.*
    FROM "order"."item" oi
    WHERE (
        (oi."id" = ANY(sqlc.slice('id')) OR sqlc.slice('id') IS NULL) AND
        (oi."order_id" = ANY(sqlc.slice('order_id')) OR sqlc.slice('order_id') IS NULL) AND
        (oi."sku_id" = ANY(sqlc.slice('sku_id')) OR sqlc.slice('sku_id') IS NULL) AND
        (oi."vendor_id" = ANY(sqlc.slice('vendor_id')) OR sqlc.slice('vendor_id') IS NULL) AND
        (oi."confirmed_by_id" = ANY(sqlc.slice('confirmed_by_id')) OR sqlc.slice('confirmed_by_id') IS NULL) AND
        (oi."shipment_id" = ANY(sqlc.slice('shipment_id')) OR sqlc.slice('shipment_id') IS NULL) AND
        (oi."status" = ANY(sqlc.slice('status')) OR sqlc.slice('status') IS NULL) AND
        (oi."quantity" = ANY(sqlc.slice('quantity')) OR sqlc.slice('quantity') IS NULL) AND
        (oi."quantity" > sqlc.narg('quantity_from') OR sqlc.narg('quantity_from') IS NULL) AND
        (oi."quantity" < sqlc.narg('quantity_to') OR sqlc.narg('quantity_to') IS NULL)
    )
)
SELECT orders.*
FROM vendor_orders orders
INNER JOIN "order"."base" base ON base.id = orders.order_id
WHERE base.payment_status = 'Success'
ORDER BY orders."id"
LIMIT sqlc.narg('limit')
OFFSET sqlc.narg('offset');