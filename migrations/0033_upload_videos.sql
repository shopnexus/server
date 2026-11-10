-- A seller's product video is an upload too (decided 2026-11-10, spec
-- 2026-11-10-video-read-design.md): MP4 or QuickTime, at most 50 MB while a
-- photo stays at 15 MB. A video is held by the read that watches it, in shape
-- video, and consumed when the read ends: its object is deleted and the row
-- stays.

ALTER TABLE uploads DROP CONSTRAINT uploads_content_type_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_content_type_check
CHECK (content_type IN ('image/jpeg', 'image/png', 'image/webp', 'video/mp4', 'video/quicktime'));

ALTER TABLE uploads DROP CONSTRAINT uploads_size_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_size_check
CHECK (size >= 1 AND size <= CASE WHEN content_type IN ('video/mp4', 'video/quicktime') THEN 52428800 ELSE 15728640 END);

ALTER TABLE uploads DROP CONSTRAINT uploads_status_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_status_check
CHECK (status IN ('waiting', 'attached', 'ready', 'failed', 'expired', 'consumed'));

ALTER TABLE uploads DROP CONSTRAINT uploads_shape_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_shape_check CHECK (shape IN ('photo', 'square', 'video'));
