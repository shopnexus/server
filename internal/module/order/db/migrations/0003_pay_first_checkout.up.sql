-- Pay-first checkout: buyer pays at checkout, item status inferred from payment/order/transport

-- New columns on item
ALTER TABLE "order".item ADD COLUMN transport_option TEXT;
ALTER TABLE "order".item ADD COLUMN transport_cost_estimate BIGINT NOT NULL DEFAULT 0;
ALTER TABLE "order".item ADD COLUMN payment_id BIGINT REFERENCES "order".payment(id);
ALTER TABLE "order".item ADD COLUMN date_cancelled TIMESTAMPTZ(3);

-- Drop item status column (status is now inferred)
ALTER TABLE "order".item DROP COLUMN status;

-- Drop order.payment_id (payment now lives on items, not orders)
ALTER TABLE "order"."order" DROP COLUMN payment_id;

-- Drop the item_status enum type
DROP TYPE IF EXISTS "order".item_status;

-- Index: find items by payment_id (for webhook confirmation)
CREATE INDEX idx_item_payment_id ON "order".item(payment_id) WHERE payment_id IS NOT NULL;

-- Index: seller's pending inbox (paid items awaiting confirmation)
CREATE INDEX idx_item_seller_pending ON "order".item(seller_id, transport_option)
    WHERE order_id IS NULL AND date_cancelled IS NULL;
