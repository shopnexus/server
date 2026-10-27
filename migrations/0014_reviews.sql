-- Reviews: the buyer of a shipped order rates its seller, 1 to 5 stars with
-- an optional text, and may change, delete or post it again for 7 days
-- after the first submission. A delete keeps the row, without its text,
-- so the 7 days stay counted from the first submission.

CREATE TABLE reviews (
    id         uuid PRIMARY KEY,
    order_id   uuid NOT NULL UNIQUE REFERENCES orders (id),
    seller_id  uuid NOT NULL REFERENCES accounts (id),
    buyer_id   uuid NOT NULL REFERENCES accounts (id),
    listing_id uuid NOT NULL REFERENCES listings (id),
    stars      integer NOT NULL CHECK (stars BETWEEN 1 AND 5),
    body       text NOT NULL CHECK (char_length(body) <= 500),
    deleted_at timestamptz,
    write_id   uuid NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

CREATE INDEX reviews_seller ON reviews (seller_id, id);
