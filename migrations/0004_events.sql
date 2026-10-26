-- Events: the outbox. A repository writes a row in the transaction of the
-- change it announces, and events.Bus sends it to the Restate service
-- Events, which hands it to every subscriber of its kind. A row is thin; a
-- subscriber rereads its subject.

CREATE TABLE events (
    id              uuid PRIMARY KEY,
    kind            text NOT NULL,
    subject_id      uuid NOT NULL,
    payload         jsonb NOT NULL,
    occurred_at     timestamptz NOT NULL,
    -- Sends the ingress refused, and when the bus may try again.
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz,
    -- Set when Restate has the event; deleted 7 days later.
    dispatched_at   timestamptz
);

CREATE INDEX events_pending ON events (id) WHERE dispatched_at IS NULL;

CREATE INDEX events_dispatched ON events (dispatched_at) WHERE dispatched_at IS NOT NULL;
