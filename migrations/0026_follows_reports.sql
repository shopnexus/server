-- Follows: a person follows an account to hear of its new listings. A
-- block, either way, deletes the follows between the two. At most 1,000
-- per follower.
CREATE TABLE account_follows (
    id          uuid PRIMARY KEY,
    follower_id uuid NOT NULL REFERENCES accounts (id),
    followed_id uuid NOT NULL REFERENCES accounts (id),
    created_at  timestamptz NOT NULL,
    UNIQUE (follower_id, followed_id),
    CHECK (follower_id <> followed_id)
);

CREATE INDEX account_follows_follower ON account_follows (follower_id, id);
CREATE INDEX account_follows_followed ON account_follows (followed_id, id);

-- Reports: a person tells ShopNexus a listing or an account breaks the
-- rules. account_id is the account reported, the seller for a listing. A
-- reporter has one open report per target; a withdrawn one keeps its row.
CREATE TABLE reports (
    id           uuid PRIMARY KEY,
    reporter_id  uuid NOT NULL REFERENCES accounts (id),
    account_id   uuid NOT NULL REFERENCES accounts (id),
    listing_id   uuid REFERENCES listings (id),
    reason       text NOT NULL CHECK (reason IN ('banned', 'fake', 'scam', 'other')),
    note         text NOT NULL CHECK (char_length(note) <= 500),
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL,
    withdrawn_at timestamptz,
    CHECK (reporter_id <> account_id)
);

CREATE UNIQUE INDEX reports_open_listing ON reports (reporter_id, listing_id)
    WHERE withdrawn_at IS NULL AND listing_id IS NOT NULL;
CREATE UNIQUE INDEX reports_open_account ON reports (reporter_id, account_id)
    WHERE withdrawn_at IS NULL AND listing_id IS NULL;
CREATE INDEX reports_account ON reports (account_id, created_at);
