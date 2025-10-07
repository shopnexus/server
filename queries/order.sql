-- name: ExistsCartItems :one
SELECT EXISTS(
    SELECT 1 FROM account.cart_item WHERE sku_id = ANY(sqlc.arg('sku_ids')::bigint[])
) AS "exists";

-- name: GetVendorAddressBySkuIDs :many
SELECT DISTINCT
    v.id as vendor_id,
    c.address,
    sku.id as sku_id
FROM catalog.product_sku AS sku
JOIN catalog.product_spu AS spu ON spu.id = sku.spu_id
JOIN account.vendor AS v ON v.account_id = spu.account_id
JOIN account.contact AS c ON c.id = v.default_contact_id
WHERE sku.id = ANY(sqlc.slice('sku_ids'));