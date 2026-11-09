-- Search vectors: one text vector and one vector per processed photo of each
-- listing, and the caches of the query side. Vectors are halfvec, half the
-- size of vector; the HNSW indexes use cosine distance. model_version holds the
-- version string the server is configured with (EMBED_*_VERSION): a row of
-- another version counts as missing and is embedded again.

-- Catalog: title, category name, condition, specs and description.
CREATE TABLE listing_text_vectors (
    listing_id    uuid PRIMARY KEY REFERENCES listings (id) ON DELETE CASCADE,
    model_version text NOT NULL,
    text_hash     bytea NOT NULL,
    embedding     halfvec(1024) NOT NULL,
    embedded_at   timestamptz NOT NULL
);

-- Catalog: one row per processed photo, keyed by its object key.
CREATE TABLE listing_photo_vectors (
    listing_id    uuid NOT NULL REFERENCES listings (id) ON DELETE CASCADE,
    photo_key     text NOT NULL,
    model_version text NOT NULL,
    embedding     halfvec(1152) NOT NULL,
    embedded_at   timestamptz NOT NULL,
    PRIMARY KEY (listing_id, photo_key)
);

CREATE INDEX listing_text_vectors_embedding
ON listing_text_vectors USING hnsw (embedding halfvec_cosine_ops);
CREATE INDEX listing_photo_vectors_embedding
ON listing_photo_vectors USING hnsw (embedding halfvec_cosine_ops);

-- Discovery: a query photo's vector, until its upload expires. It holds the
-- upload ID and no account; the upload row ties it to one for that long.
CREATE TABLE search_photo_vectors (
    upload_id     uuid PRIMARY KEY,
    model_version text NOT NULL,
    embedding     halfvec(1152) NOT NULL,
    expires_at    timestamptz NOT NULL
);
CREATE INDEX search_photo_vectors_expires ON search_photo_vectors (expires_at);

-- Discovery: the vector of a text the search embedded (the rewritten query,
-- or its visual description), 7 days like the understanding.
CREATE TABLE search_query_vectors (
    query_key      text NOT NULL,
    prompt_version integer NOT NULL,
    model_version  text NOT NULL,
    embedding      halfvec NOT NULL,
    created_at     timestamptz NOT NULL,
    PRIMARY KEY (query_key, prompt_version, model_version)
);
CREATE INDEX search_query_vectors_created ON search_query_vectors (created_at);
