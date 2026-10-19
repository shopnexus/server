-- Work for the outside world (SMS, email, events), written in the same
-- transaction as the business change. The relay publishes rows to JetStream
-- in id order and stamps published_at.
CREATE TABLE outbox (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    subject      text NOT NULL,
    payload      jsonb NOT NULL,
    headers      jsonb NOT NULL DEFAULT '{}',   -- W3C traceparent, so traces continue into consumers
    deadline     timestamptz,                   -- consumers drop the message after this
    ordering_key text,                          -- reserved for the event layer
    created_at   timestamptz NOT NULL DEFAULT now(),
    published_at timestamptz
);

CREATE INDEX outbox_pending ON outbox (id) WHERE published_at IS NULL;

-- Wake the relay at commit instead of waiting for its next poll.
CREATE FUNCTION outbox_notify() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_notify('outbox', '');
    RETURN NULL;
END $$;

CREATE TRIGGER outbox_notify AFTER INSERT ON outbox
    FOR EACH STATEMENT EXECUTE FUNCTION outbox_notify();
