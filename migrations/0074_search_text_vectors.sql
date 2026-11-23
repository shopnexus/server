-- Discovery: the vector of a text a search embedded, by the embedding
-- model's version and the SHA-256 of the normalized text. A vector never
-- changes for its key, so rows leave by use: hits counts the days a search
-- read the row, last_used_at is the last of them. The table is unlogged: a
-- crash empties it and searches embed again.
CREATE UNLOGGED TABLE search_text_vectors (
    model_version text NOT NULL,
    text_hash     bytea NOT NULL,
    embedding     halfvec NOT NULL,
    created_at    timestamptz NOT NULL,
    last_used_at  timestamptz NOT NULL,
    hits          integer NOT NULL DEFAULT 0,
    PRIMARY KEY (model_version, text_hash)
);

INSERT INTO search_text_vectors (model_version, text_hash, embedding, created_at, last_used_at)
SELECT DISTINCT ON (model_version, query_key)
    model_version, sha256(convert_to(query_key, 'UTF8')), embedding, created_at, created_at
FROM search_query_vectors
ORDER BY model_version, query_key, created_at DESC;

DROP TABLE search_query_vectors;
