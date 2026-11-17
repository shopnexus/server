-- Home's Dành cho bạn reads a person's recent searches and listing views.
--
-- A search history entry keeps the code of its first category chip beside
-- the chip's label, as the feed weighs searches by subcategory and a label
-- names no single one: "Khác" labels a subcategory under every root, and
-- "Đồng hồ" labels two. Entries written before this file have no code and
-- weigh nothing. The code is plain text with no foreign key, as
-- search_watches.category is: a code no category has any more boosts no
-- listing, and nothing reads the column by code, so it has no index.
--
-- The feed reads a viewer's newest 100 marks. The index on the viewer, the
-- day and the listing gives them in that order, so the read stops at 100
-- however many listings the person opened; it replaces the index on the
-- viewer alone, which erasure's delete by viewer now uses.

ALTER TABLE search_history ADD COLUMN category_code text;

CREATE INDEX listing_view_marks_viewer_day ON listing_view_marks (viewer_id, viewed_on, listing_id);
DROP INDEX listing_view_marks_viewer;
