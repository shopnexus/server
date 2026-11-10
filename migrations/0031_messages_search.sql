-- Inbox search: ParadeDB's BM25 index over message bodies, folded to
-- lowercase ASCII, with the conversation and status to filter inside the
-- index and the ID to page by.
CREATE INDEX messages_search ON messages USING paradedb (
    id, (body::pdb.simple('ascii_folding=true')), conversation_id, (status::pdb.literal)
);
