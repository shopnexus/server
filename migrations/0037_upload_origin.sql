-- Where an upload came from: put by the app (app), cut by the server from
-- the seller's video (frame), or copied by the server from the seller's
-- listing on another site (link). A listing shows buyers which of its
-- photos were copied. Rows from before are the app's; a frame among them
-- waits a day at most and was shown as the app's anyway.
--
-- The default stays: migrations run while the replicas of the release
-- before still serve, and their inserts name no origin. A migration of a
-- later release may drop it.
ALTER TABLE uploads ADD COLUMN origin text NOT NULL DEFAULT 'app' CHECK (origin IN ('app', 'frame', 'link'));
