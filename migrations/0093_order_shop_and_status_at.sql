-- The carrier shop a linked shipment was made at, so a save can check the
-- seller still links that shop; NULL for shipments made before.
ALTER TABLE orders ADD COLUMN shop_code text;

-- When the carrier's step behind shipment_status happened, so an older
-- report never lands last; NULL until the carrier reports one.
ALTER TABLE orders ADD COLUMN shipment_status_at timestamptz;
