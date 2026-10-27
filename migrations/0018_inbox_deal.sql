-- The deals of a pair of people, either side, as the inbox and a
-- conversation read them.

CREATE INDEX deals_buyer_seller ON deals (buyer_id, seller_id);
CREATE INDEX deals_seller_buyer ON deals (seller_id, buyer_id);
