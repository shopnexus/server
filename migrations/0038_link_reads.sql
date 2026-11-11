-- Module assist: a seller's listing on another site read into a draft
-- listing. photo_count, draft and estimate fill in as the flow's steps
-- finish; a step not reached yet is NULL. The draft's photos are the
-- copies, waiting uploads of the seller's. The row is deleted 24 hours
-- after it starts. Nothing about the listing's seller is stored, and its
-- text only as the draft restates it.
CREATE TABLE link_reads (
    id              uuid PRIMARY KEY,
    account_id      uuid NOT NULL REFERENCES accounts (id),
    site            text NOT NULL CHECK (site IN ('chotot.com')),
    list_id         bigint NOT NULL CHECK (list_id > 0),
    url             text NOT NULL,
    status          text NOT NULL CHECK (status IN ('reading', 'done', 'failed')),
    failure         text CHECK (failure IN ('listing_not_found', 'no_item', 'unavailable')),
    photo_count     integer CHECK (photo_count > 0),
    draft           jsonb,
    contact_removed boolean,
    estimate        jsonb,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    write_id        uuid NOT NULL,
    CHECK ((status = 'failed') = (failure IS NOT NULL)),
    CHECK ((draft IS NULL) = (contact_removed IS NULL))
);
-- The cap counts an account's link reads of the last 24 hours.
CREATE INDEX link_reads_account_created ON link_reads (account_id, created_at);

-- The update a read's account gets after each step.
ALTER TABLE account_updates ADD COLUMN link_read_id uuid;
