-- Search: the listing text tokenized a second time with diacritics folded
-- away (title_folded, description_folded, specs_folded), for searches on a
-- person's raw words, which are often typed without them. A query the model
-- rewrote matches the fields as written.
DROP INDEX listings_search;
CREATE INDEX listings_search ON listings USING paradedb (
    id, title, description, specs_text,
    (title::pdb.unicode_words('alias=title_folded', 'ascii_folding=true')),
    (description::pdb.unicode_words('alias=description_folded', 'ascii_folding=true')),
    (specs_text::pdb.unicode_words('alias=specs_folded', 'ascii_folding=true')),
    (category_code::pdb.literal), (category_root::pdb.literal), price, (condition::pdb.literal),
    allow_offers, (status::pdb.literal), published_at, lat, lng
);
