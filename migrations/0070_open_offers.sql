-- The buyers' open offers on a seller's listings, newest offer first, for
-- the seller's offers screen and its count.
CREATE INDEX deals_open_offers ON deals (seller_id, open_at DESC, id DESC) WHERE status = 'negotiating' AND open_by = 'buyer';
