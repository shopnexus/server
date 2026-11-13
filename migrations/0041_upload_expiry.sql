-- Module media: when a waiting upload is deleted. It starts a day after
-- the upload, and a listing draft that names the photo moves it to 30 days
-- after its last PUT; nothing moves it back. The default serves the
-- inserts of the release before while this rolls out: their uploads
-- expire a day on, as that release's MediaExpire does.
ALTER TABLE uploads ADD COLUMN expires_at timestamptz NOT NULL DEFAULT (now() + interval '24 hours');
UPDATE uploads SET expires_at = created_at + interval '24 hours';
