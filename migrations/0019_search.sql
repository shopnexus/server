-- Search: ParadeDB's BM25 index over listings, the listing's point for
-- distance, and the cache of the model's understanding of a query.
-- pg_search is preloaded by the paradedb image and needs vector.
CREATE EXTENSION IF NOT EXISTS pg_search CASCADE;

-- The point distance is measured from: blurred from the seller's GPS, or the
-- commune's point. It never leaves the server.
ALTER TABLE listings ADD COLUMN lat double precision;
ALTER TABLE listings ADD COLUMN lng double precision;
UPDATE listings AS l SET lat = c.lat, lng = c.lng
FROM communes AS c
WHERE c.code = l.commune_code;
ALTER TABLE listings ALTER COLUMN lat SET NOT NULL, ALTER COLUMN lng SET NOT NULL;

ALTER TABLE listings ADD COLUMN category_root text
GENERATED ALWAYS AS (split_part(category_code, '.', 1)) STORED;
-- The specs as text, so one field matches labels and values.
ALTER TABLE listings ADD COLUMN specs_text text GENERATED ALWAYS AS (specs::text) STORED;

CREATE INDEX listings_search ON listings USING paradedb (
    id, title, description, specs_text,
    (category_code::pdb.literal), (category_root::pdb.literal), price, (condition::pdb.literal),
    allow_offers, (status::pdb.literal), published_at, lat, lng
);

-- What the model understood of a normalized query. No account is stored.
CREATE TABLE search_understandings (
    query_key      text NOT NULL,
    prompt_version integer NOT NULL,
    answer         jsonb NOT NULL,
    created_at     timestamptz NOT NULL,
    PRIMARY KEY (query_key, prompt_version)
);
CREATE INDEX search_understandings_created ON search_understandings (created_at);
