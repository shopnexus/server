-- Rules module deal holds in Go, kept by the table too. NOT VALID: new and
-- updated rows are checked, and an old row never fails the deploy.

-- An order's price is the listed price, a counter or the agreed price.
ALTER TABLE orders ADD CONSTRAINT orders_price_check CHECK (price > 0) NOT VALID;

-- Only a cancelled order names who cancelled it and why.
ALTER TABLE orders ADD CONSTRAINT orders_cancel_check CHECK (
    (status = 'cancelled') = (cancelled_by IS NOT NULL)
    AND (status = 'cancelled') = (cancel_reason IS NOT NULL)
) NOT VALID;

-- An order is closed exactly when it ended: completed, cancelled or failed.
ALTER TABLE orders ADD CONSTRAINT orders_closed_check CHECK (
    (status IN ('completed', 'cancelled', 'failed')) = (closed_at IS NOT NULL)
) NOT VALID;

-- shipped_at is the shipment's time, set with its mode.
ALTER TABLE orders ADD CONSTRAINT orders_shipped_check CHECK (
    (shipment_mode IS NULL) = (shipped_at IS NULL)
) NOT VALID;

-- An open price is a positive offer or counter.
ALTER TABLE deals ADD CONSTRAINT deals_open_amount_check CHECK (open_amount > 0) NOT VALID;

-- Agreeing clears the open price, so only a negotiating deal holds one.
ALTER TABLE deals ADD CONSTRAINT deals_open_status_check CHECK (
    open_by IS NULL OR status = 'negotiating'
) NOT VALID;
