-- Two-way reviews (2026-11-19). Each side of an order reviews the other:
-- role 'seller' rates the person who sold, 'buyer' the person who bought.
-- The subject may reply once, which closes the review. A review stays
-- hidden from its subject and the public until visible_at.
ALTER TABLE reviews
    ADD COLUMN role        text,
    ADD COLUMN reviewer_id uuid REFERENCES accounts (id),
    ADD COLUMN subject_id  uuid REFERENCES accounts (id),
    ADD COLUMN reply       text CHECK (char_length(reply) <= 500),
    ADD COLUMN replied_at  timestamptz,
    ADD COLUMN visible_at  timestamptz;

UPDATE reviews SET role = 'seller', reviewer_id = buyer_id, subject_id = seller_id, visible_at = created_at;

ALTER TABLE reviews
    ALTER COLUMN role SET NOT NULL,
    ALTER COLUMN reviewer_id SET NOT NULL,
    ALTER COLUMN subject_id SET NOT NULL,
    ALTER COLUMN visible_at SET NOT NULL,
    ADD CONSTRAINT reviews_role CHECK (role IN ('seller', 'buyer')),
    ADD CONSTRAINT reviews_reply CHECK ((reply IS NULL) = (replied_at IS NULL)),
    DROP CONSTRAINT reviews_order_id_key,
    ADD CONSTRAINT reviews_order_role UNIQUE (order_id, role),
    DROP COLUMN seller_id,
    DROP COLUMN buyer_id;

CREATE INDEX reviews_subject ON reviews (subject_id, role, id);
CREATE INDEX reviews_reviewer ON reviews (reviewer_id, order_id);
