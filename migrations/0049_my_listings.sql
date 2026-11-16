-- Tin của bạn: views of listings, the people who wrote about a listing, and
-- photos copied from a seller's own listing.
--
-- A signed-in person other than the seller who opens a listing counts once
-- per UTC day. listing_view_marks keeps the last day each person opened each
-- listing, so a second open that day changes nothing; discovery deletes a
-- person's marks when the account is deleted. listing_view_days holds the
-- count per listing and day, so a total and the last seven days are sums
-- over a few rows.

CREATE TABLE listing_view_marks (
    listing_id uuid NOT NULL REFERENCES listings (id),
    viewer_id  uuid NOT NULL REFERENCES accounts (id),
    viewed_on  date NOT NULL,
    PRIMARY KEY (listing_id, viewer_id)
);

CREATE INDEX listing_view_marks_viewer ON listing_view_marks (viewer_id);

CREATE TABLE listing_view_days (
    listing_id uuid NOT NULL REFERENCES listings (id),
    viewed_on  date NOT NULL,
    view_count integer NOT NULL CHECK (view_count > 0),
    PRIMARY KEY (listing_id, viewed_on)
);

-- The people who wrote to a seller about a listing, read by listing.
CREATE INDEX messages_listing ON messages (listing_id) WHERE listing_id IS NOT NULL;

-- An upload copied by the server from a photo of the seller's own listing,
-- for a new listing. The replicas of the release before never insert it.
ALTER TABLE uploads DROP CONSTRAINT uploads_origin_check;
ALTER TABLE uploads ADD CONSTRAINT uploads_origin_check CHECK (origin IN ('app', 'frame', 'link', 'listing'));
