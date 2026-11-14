-- An address says what the place is, so the shipper knows when to deliver:
-- home, work (an office takes parcels in office hours) or other. A COD order
-- copies it with the rest of the address. Addresses and orders from before
-- this file become home. The default only fills those rows; the code sets
-- the kind on every insert.

ALTER TABLE addresses
    ADD COLUMN kind text NOT NULL DEFAULT 'home' CHECK (kind IN ('home', 'work', 'other'));
ALTER TABLE addresses ALTER COLUMN kind DROP DEFAULT;

ALTER TABLE orders
    ADD COLUMN recipient_kind text NOT NULL DEFAULT 'home' CHECK (recipient_kind IN ('home', 'work', 'other'));
ALTER TABLE orders ALTER COLUMN recipient_kind DROP DEFAULT;
