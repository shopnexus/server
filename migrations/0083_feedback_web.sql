-- Feedback from the website (2026-11-24): where it was sent from, the page
-- it was sent on, and a browser has no app version.
ALTER TABLE feedback ADD COLUMN source text NOT NULL DEFAULT 'app' CHECK (source IN ('app', 'web'));
ALTER TABLE feedback ADD COLUMN page_url text CHECK (char_length(page_url) BETWEEN 1 AND 500);
ALTER TABLE feedback DROP CONSTRAINT feedback_check1;
ALTER TABLE feedback ADD CONSTRAINT feedback_device_check
CHECK ((device_model IS NULL) = (device_os IS NULL) AND (app_version IS NULL OR device_model IS NOT NULL));
