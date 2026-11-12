-- The order flow of 2026-11-12: an order is pending, shipped, completed,
-- failed or cancelled, and a shipment replaces the waybill. A shipment is
-- linked (the server made it on the seller's GHTK account) or manual (the
-- seller typed the code of a shipment made on a carrier's own app), and
-- keeps the status its carrier reports. Spec:
-- ~/shopnexus/docs/superpowers/specs/2026-11-12-order-flow-sync-design.md.
--
-- This migration renames values the release before writes and drops two
-- columns it reads, so that release's order reads and writes fail while a
-- deploy rolls out. Part 1 of the spec does not wire the app to the
-- server, so no one calls them then.

ALTER TABLE orders DROP CONSTRAINT orders_status_check;
ALTER TABLE orders DROP CONSTRAINT orders_carrier_check;
ALTER TABLE orders DROP CONSTRAINT orders_cancel_reason_check;
DROP INDEX orders_open_per_deal;

ALTER TABLE orders
    ADD COLUMN shipment_mode text,
    ADD COLUMN shipment_status text,
    ADD COLUMN delivered_at timestamptz;

-- A waybill of a carrier ShopNexus cannot read keeps no shipment. Only
-- development data has one, and the buyer's confirmation still closes it.
UPDATE orders SET carrier = NULL, tracking_code = NULL, fee = NULL WHERE carrier = 'other';
UPDATE orders SET status = CASE status
    WHEN 'requested' THEN 'pending' WHEN 'delivered' THEN 'completed' WHEN 'returned' THEN 'failed' ELSE status END;
-- Every other waybill was typed by its seller: a manual shipment, with the
-- carrier already holding the parcel, or the way the order ended.
UPDATE orders SET shipment_mode = 'manual', shipment_status = CASE status
    WHEN 'completed' THEN 'delivered' WHEN 'failed' THEN 'failed' ELSE 'in_transit' END
WHERE carrier IS NOT NULL;
UPDATE orders SET delivered_at = closed_at WHERE shipment_status = 'delivered';
UPDATE orders SET cancel_reason = 'shipped_to_another' WHERE cancel_reason = 'sent_to_another';

ALTER TABLE orders DROP COLUMN waybill_source, DROP COLUMN carrier_name;
ALTER TABLE orders
    ADD CONSTRAINT orders_status_check CHECK (status IN ('pending', 'shipped', 'completed', 'failed', 'cancelled')),
    ADD CONSTRAINT orders_carrier_check CHECK (carrier IN ('ghtk', 'ghn', 'spx', 'jt', 'viettel_post')),
    ADD CONSTRAINT orders_shipment_mode_check CHECK (shipment_mode IN ('linked', 'manual')),
    ADD CONSTRAINT orders_shipment_status_check
        CHECK (shipment_status IN ('pending_pickup', 'in_transit', 'delivered', 'failed')),
    ADD CONSTRAINT orders_cancel_reason_check CHECK (cancel_reason IN (
        'buyer', 'seller', 'seller_timeout', 'pickup_timeout', 'shipped_to_another', 'listing_closed'
    )),
    -- A shipment has its mode, carrier, code and status together; only a
    -- delivered one has a delivery time.
    ADD CONSTRAINT orders_shipment_check CHECK (
        (shipment_mode IS NULL) = (carrier IS NULL)
        AND (shipment_mode IS NULL) = (tracking_code IS NULL)
        AND (shipment_mode IS NULL) = (shipment_status IS NULL)
        AND (delivered_at IS NULL OR shipment_status = 'delivered')
    );

CREATE UNIQUE INDEX orders_open_per_deal ON orders (deal_id) WHERE status IN ('pending', 'shipped');
-- A carrier's report names a tracking code; ShipmentUpdates finds its orders here.
CREATE INDEX orders_tracking_code ON orders (tracking_code) WHERE tracking_code IS NOT NULL;

ALTER TABLE deal_moves DROP CONSTRAINT deal_moves_kind_check;
UPDATE deal_moves SET kind = CASE kind WHEN 'delivered' THEN 'completed' ELSE 'failed' END
WHERE kind IN ('delivered', 'returned');
ALTER TABLE deal_moves ADD CONSTRAINT deal_moves_kind_check CHECK (kind IN (
    'offer', 'counter', 'accept', 'decline', 'withdraw', 'cancel', 'price_changed', 'offers_closed',
    'listing_closed', 'cod_requested', 'shipped', 'shipment_updated', 'completed', 'failed', 'order_cancelled'
));

-- Stored notices take the new types, whose texts read the same params.
UPDATE notifications SET notice_type = CASE notice_type WHEN 'delivered' THEN 'completed' ELSE 'failed' END
WHERE notice_type IN ('delivered', 'returned');
UPDATE notifications SET params = jsonb_set(params, '{reason}', '"shipped_to_another"')
WHERE params ->> 'reason' = 'sent_to_another';
