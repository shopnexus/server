-- A listing's code ends its web address (/listings/<title slug>-<code>).
-- It is 8 Crockford base32 characters hashed from the ID and an attempt,
-- so a seed pack's listings get the same codes on every load; a code
-- another listing holds moves to the next attempt.
CREATE FUNCTION listing_code(id uuid, attempt int) RETURNS text
LANGUAGE sql IMMUTABLE PARALLEL SAFE
RETURN (
    SELECT string_agg(substr('0123456789abcdefghjkmnpqrstvwxyz', ((h.n >> (35 - 5 * i)) & 31)::int + 1, 1), '' ORDER BY i)
    FROM (SELECT ('x' || encode(substr(sha256(uuid_send(id) || int4send(attempt)), 1, 5), 'hex'))::bit(40)::bigint AS n) h,
         generate_series(0, 7) i
);

ALTER TABLE listings ADD COLUMN code text UNIQUE CHECK (code ~ '^[0-9a-hjkmnp-tv-z]{8}$');

UPDATE listings l SET code = listing_code(l.id, 0)
FROM (SELECT id, row_number() OVER (PARTITION BY listing_code(id, 0) ORDER BY created_at, id) AS n FROM listings) f
WHERE f.id = l.id AND f.n = 1;

DO $$
DECLARE r record;
BEGIN
    FOR r IN SELECT id FROM listings WHERE code IS NULL ORDER BY created_at, id LOOP
        UPDATE listings SET code = (
            SELECT c FROM generate_series(1, 15) a, listing_code(r.id, a) c
            WHERE NOT EXISTS (SELECT 1 FROM listings WHERE code = c) ORDER BY a LIMIT 1)
        WHERE id = r.id;
    END LOOP;
END $$;

ALTER TABLE listings ALTER COLUMN code SET NOT NULL;
