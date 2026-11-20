-- When the carrier first reported an order's parcel past pending_pickup,
-- so the order shows that step with its time.

ALTER TABLE orders ADD COLUMN picked_up_at timestamptz;

-- A parcel on its way or delivered took the time of the carrier's first
-- update after the shipment, or of the shipment when it went out already
-- on its way.
UPDATE orders AS o
SET picked_up_at = coalesce((
    SELECT min(m.moved_at) FROM deal_moves AS m
    WHERE m.order_id = o.id AND m.kind = 'shipment_updated' AND m.by_party = 'system'
), o.shipped_at)
WHERE o.shipment_status IN ('in_transit', 'delivered');
