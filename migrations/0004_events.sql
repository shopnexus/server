-- Events: the outbox. A repository writes a row in the transaction of the
-- change it announces, and events.Bus hands it to the Restate handlers
-- subscribed to its kind. A row is thin; a subscriber rereads its subject.

CREATE TABLE events (
    id            uuid PRIMARY KEY,
    kind          text NOT NULL,
    subject_id    uuid NOT NULL,
    data          jsonb NOT NULL,
    at            timestamptz NOT NULL,
    -- Set when every subscriber has the event; deleted 7 days later.
    dispatched_at timestamptz
);

CREATE INDEX events_pending ON events (id) WHERE dispatched_at IS NULL;

CREATE INDEX events_dispatched ON events (dispatched_at) WHERE dispatched_at IS NOT NULL;
