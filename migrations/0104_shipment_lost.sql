-- A parcel the carrier lost after the pickup ends its order failed with the
-- shipment lost: the carrier's fault, which counts against neither side.
-- NOT VALID: new and updated rows are checked, old rows stay in place.
ALTER TABLE orders DROP CONSTRAINT orders_shipment_status_check;
ALTER TABLE orders ADD CONSTRAINT orders_shipment_status_check
CHECK (shipment_status IN ('pending_pickup', 'in_transit', 'delivered', 'failed', 'lost')) NOT VALID;

-- Only a failed order keeps a lost shipment.
ALTER TABLE orders ADD CONSTRAINT orders_shipment_lost_check
CHECK (shipment_status IS DISTINCT FROM 'lost' OR status = 'failed') NOT VALID;
