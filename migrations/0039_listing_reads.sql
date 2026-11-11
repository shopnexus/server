-- Module assist: every read of a seller's video, photo or listing on
-- another site into a draft, one row each. A video or photo read names its
-- upload (media_id); a link read its link and, after the read step, how
-- many photos the old listing has (link_photos). photos, draft and
-- estimate fill in as the flow's steps finish; a step not reached yet is
-- NULL. photos are a video's frames (with at_seconds), a link's copies,
-- or the photo read. The row is deleted 24 hours after it starts. Like
-- other modules' tables, it references no table of module media.
--
-- transcript is what the seller's phone heard during a video, held only
-- while the read runs: the end of the read sets it to NULL. It is never
-- logged or returned.
CREATE TABLE listing_reads (
    id              uuid PRIMARY KEY,
    account_id      uuid NOT NULL REFERENCES accounts (id),
    source          text NOT NULL CHECK (source IN ('video', 'photo', 'link')),
    media_id        uuid,
    link_site       text CHECK (link_site IN ('chotot.com')),
    link_list_id    bigint CHECK (link_list_id > 0),
    link_url        text,
    link_photos     integer CHECK (link_photos > 0),
    transcript      text,
    status          text NOT NULL CHECK (status IN ('reading', 'done', 'failed')),
    failure         text CHECK (failure IN ('unreadable', 'video_too_short', 'video_too_long',
                                            'listing_not_found', 'no_item', 'unavailable')),
    photos          jsonb,
    draft           jsonb,
    contact_removed boolean,
    estimate        jsonb,
    created_at      timestamptz NOT NULL,
    expires_at      timestamptz NOT NULL,
    write_id        uuid NOT NULL,
    CHECK ((status = 'failed') = (failure IS NOT NULL)),
    CHECK ((media_id IS NOT NULL) = (source IN ('video', 'photo'))),
    CHECK ((link_site IS NOT NULL AND link_list_id IS NOT NULL AND link_url IS NOT NULL) = (source = 'link')),
    CHECK (link_photos IS NULL OR source = 'link'),
    CHECK (transcript IS NULL OR (source = 'video' AND status = 'reading')),
    CHECK ((contact_removed IS NOT NULL) = (source = 'link' AND draft IS NOT NULL))
);
-- Each cap counts an account's reads of one source in the last 24 hours.
CREATE INDEX listing_reads_account_source_created ON listing_reads (account_id, source, created_at);

-- The phone's transcript between the request and the read's insert, so it
-- never enters a Restate journal. The insert moves it into listing_reads
-- and deletes it; a row older than 10 minutes is deleted by the next insert.
CREATE TABLE listing_read_transcripts (
    account_id  uuid NOT NULL REFERENCES accounts (id),
    request_key text NOT NULL,
    text        text NOT NULL CHECK (length(text) BETWEEN 1 AND 1000),
    created_at  timestamptz NOT NULL,
    PRIMARY KEY (account_id, request_key)
);

-- The reads of the release before. video_reads, link_reads and the two
-- columns of account_updates stay: that release still writes them while
-- this runs. A later release drops them.
INSERT INTO listing_reads (id, account_id, source, media_id, status, failure, photos, draft, estimate,
    created_at, expires_at, write_id)
SELECT id, account_id, 'video', video_id, status,
       CASE failure WHEN 'unreadable_video' THEN 'unreadable' ELSE failure END,
       frames, draft, estimate, created_at, expires_at, write_id
FROM video_reads;

INSERT INTO listing_reads (id, account_id, source, link_site, link_list_id, link_url, link_photos, status, failure,
    photos, draft, contact_removed, estimate, created_at, expires_at, write_id)
SELECT id, account_id, 'link', site, list_id, url, photo_count, status, failure,
       (SELECT jsonb_agg(jsonb_build_object('upload_id', p.upload) ORDER BY p.n)
        FROM jsonb_array_elements_text(draft -> 'photos') WITH ORDINALITY AS p (upload, n)),
       draft, contact_removed, estimate, created_at, expires_at, write_id
FROM link_reads;

-- The update a read's account gets after each step.
ALTER TABLE account_updates ADD COLUMN listing_read_id uuid;
UPDATE account_updates SET kind = 'listing_read', listing_read_id = coalesce(video_read_id, link_read_id)
WHERE kind IN ('video_read', 'link_read');
