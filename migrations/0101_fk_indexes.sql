-- An index under every foreign key into a table that rows are deleted
-- from, so deleting a parent row does not scan the child table.
CREATE INDEX deal_moves_order ON deal_moves (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX reviews_listing ON reviews (listing_id);
CREATE INDEX orders_listing ON orders (listing_id);
CREATE INDEX conversations_account_a ON conversations (account_a);
CREATE INDEX conversations_account_b ON conversations (account_b);
CREATE INDEX hidden_listings_listing ON hidden_listings (listing_id);
CREATE INDEX review_helpful_account ON review_helpful (account_id);
CREATE INDEX scam_warnings_account ON scam_warnings (account_id);
CREATE INDEX scam_warnings_suspect ON scam_warnings (suspect_id);
CREATE INDEX reports_listing ON reports (listing_id) WHERE listing_id IS NOT NULL;
CREATE INDEX tickets_order ON tickets (order_id) WHERE order_id IS NOT NULL;
CREATE INDEX ticket_replies_ticket ON ticket_replies (ticket_id);
CREATE INDEX otp_challenges_account ON otp_challenges (account_id) WHERE account_id IS NOT NULL;
CREATE INDEX report_resolutions_account_id ON report_resolutions (account_id);
