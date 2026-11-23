-- The language an account's mail is written in (2026-11-23), from the
-- Accept-Language of its latest sign-in. Existing accounts read Vietnamese.
ALTER TABLE accounts ADD COLUMN locale text NOT NULL DEFAULT 'vi';
