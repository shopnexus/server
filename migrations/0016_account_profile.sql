-- A person's public profile: a bio of up to 200 characters and an avatar,
-- the object key of a 400 px square that media made from an upload.

ALTER TABLE accounts ADD COLUMN bio text CHECK (char_length(bio) BETWEEN 1 AND 200);
ALTER TABLE accounts ADD COLUMN avatar_key text;

-- What processing makes of an upload: a photo with its thumbnail, or a
-- square. Set when the upload is attached, so NULL while it waits.
ALTER TABLE uploads ADD COLUMN shape text CHECK (shape IN ('photo', 'square'));
UPDATE uploads SET shape = 'photo'
WHERE holder_id IS NOT NULL;
ALTER TABLE uploads ADD CONSTRAINT uploads_shape_held CHECK ((holder_id IS NULL) = (shape IS NULL));
