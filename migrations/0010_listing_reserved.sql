-- A listing is reserved while an order's parcel travels (decided
-- 2026-10-26); reserved_for names the order.

ALTER TABLE listings DROP CONSTRAINT listings_status_check;
ALTER TABLE listings ADD CONSTRAINT listings_status_check
CHECK (status IN ('processing', 'active', 'hidden', 'rejected', 'reserved', 'sold', 'deleted'));
ALTER TABLE listings ADD COLUMN reserved_for uuid;
