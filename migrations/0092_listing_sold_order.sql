-- The order a listing was sold through; NULL for a sale off the app, which
-- the seller may undo. A deleted listing keeps it.
ALTER TABLE listings
    ADD COLUMN sold_order_id uuid,
    ADD CONSTRAINT listings_sold_order_check CHECK (sold_order_id IS NULL OR status IN ('sold', 'deleted'));
-- Sold listings with a completed order were sold through the latest one.
UPDATE listings l SET sold_order_id = (
    SELECT o.id FROM orders o WHERE o.listing_id = l.id AND o.status = 'completed' ORDER BY o.id DESC LIMIT 1)
WHERE l.status = 'sold';
