-- Module assist: a seller's video read into a draft listing. frames, draft
-- and estimate fill in as the flow's steps finish; a step not reached yet is
-- NULL. The row is deleted 24 hours after it starts. video_id names the
-- upload the read holds; like other modules' tables, it references no table
-- of module media. The transcript is never stored.
CREATE TABLE video_reads (
    id         uuid PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts (id),
    video_id   uuid NOT NULL,
    status     text NOT NULL CHECK (status IN ('reading', 'done', 'failed')),
    failure    text CHECK (failure IN ('unreadable_video', 'video_too_short', 'video_too_long', 'no_item', 'unavailable')),
    frames     jsonb,
    draft      jsonb,
    estimate   jsonb,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    write_id   uuid NOT NULL,
    CHECK ((status = 'failed') = (failure IS NOT NULL))
);
-- The cap counts an account's reads of the last 24 hours.
CREATE INDEX video_reads_account_created ON video_reads (account_id, created_at);

-- The update a read's account gets after each step.
ALTER TABLE account_updates ADD COLUMN video_read_id uuid;
