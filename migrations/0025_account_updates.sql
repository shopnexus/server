-- Realtime: each account's update log. stream.Notify appends a row per
-- account in the transaction of the change. seq runs 1, 2, 3 per account
-- with no hole: the counter row stays locked until commit and rolls back
-- with the transaction, so seqs also become visible in order. No foreign
-- key, as for events: the log is platform data, pruned after 30 days.
CREATE TABLE account_update_seqs (
    account_id uuid PRIMARY KEY,
    seq        bigint NOT NULL
);

CREATE TABLE account_updates (
    account_id      uuid NOT NULL,
    seq             bigint NOT NULL,
    kind            text NOT NULL,
    conversation_id uuid,
    message_id      uuid,
    deal_id         uuid,
    notification_id uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, seq)
);
CREATE INDEX account_updates_created_at ON account_updates (created_at);

-- When the person last had the app open; the WebSocket writes it.
ALTER TABLE accounts ADD COLUMN last_active_at timestamptz;
