-- Review tags (2026-11-23): quick chips on a review about the subject's
-- conduct, as_described, quick_reply or well_packed about a seller and
-- quick_reply or on_time about a buyer. A deleted review has none.
ALTER TABLE reviews ADD COLUMN tags text[] NOT NULL DEFAULT '{}';
