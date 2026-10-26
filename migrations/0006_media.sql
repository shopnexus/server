-- Media: uploads of photos. The app PUTs a photo to uploads/<id> in the
-- object store through a presigned URL; attached to a listing, it is
-- processed into media/<id>.jpg and media/<id>_thumb.jpg. An upload still
-- waiting after 24 hours is deleted.

CREATE TABLE uploads (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL REFERENCES accounts (id),
    -- What the upload is attached to, such as a listing; NULL while waiting.
    holder_id    uuid,
    content_type text NOT NULL CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp')),
    size         bigint NOT NULL CHECK (size BETWEEN 1 AND 15728640),
    status       text NOT NULL CHECK (status IN ('waiting', 'attached', 'ready', 'failed', 'expired')),
    photo_key    text,
    thumb_key    text,
    failure      text,
    write_id     uuid NOT NULL,
    created_at   timestamptz NOT NULL,
    updated_at   timestamptz NOT NULL
);

CREATE INDEX uploads_owner ON uploads (owner_id, id);
