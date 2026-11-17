-- Report cases: the reports about one account that share one resolution.
-- The reports not withdrawn and not resolved are the account's open case; a
-- console member resolves it, which stamps those reports with a
-- resolution, and the next report opens a new case. Reopening a case sets
-- reopened_at and clears its reports' resolution_id, except a report whose
-- reporter has a newer open report of the same target, which stays with
-- the reopened resolution.
CREATE TABLE report_resolutions (
    id          uuid PRIMARY KEY,
    account_id  uuid NOT NULL REFERENCES accounts (id),
    outcome     text NOT NULL CHECK (outcome IN ('dismissed', 'actioned')),
    member_id   uuid NOT NULL REFERENCES console_members (id),
    resolved_at timestamptz NOT NULL,
    reopened_at timestamptz
);

CREATE INDEX report_resolutions_account ON report_resolutions (account_id, resolved_at)
WHERE reopened_at IS NULL;
CREATE INDEX report_resolutions_resolved ON report_resolutions (resolved_at, id)
WHERE reopened_at IS NULL;

ALTER TABLE reports ADD COLUMN resolution_id uuid REFERENCES report_resolutions (id);

-- A reporter has one open report per target; a resolved one no longer
-- counts, so reporting again opens a new row in the next case.
DROP INDEX reports_open_listing;
DROP INDEX reports_open_account;
CREATE UNIQUE INDEX reports_open_listing ON reports (reporter_id, listing_id)
WHERE withdrawn_at IS NULL AND resolution_id IS NULL AND listing_id IS NOT NULL;
CREATE UNIQUE INDEX reports_open_account ON reports (reporter_id, account_id)
WHERE withdrawn_at IS NULL AND resolution_id IS NULL AND listing_id IS NULL;
CREATE INDEX reports_open_case ON reports (account_id, created_at)
WHERE withdrawn_at IS NULL AND resolution_id IS NULL;
CREATE INDEX reports_resolution ON reports (resolution_id) WHERE resolution_id IS NOT NULL;

-- A listing a console member hid: held_at is set, and only a member puts
-- it back on sale.
ALTER TABLE listings ADD COLUMN held_at timestamptz;

ALTER TABLE console_actions DROP CONSTRAINT console_actions_target_kind_check;
ALTER TABLE console_actions ADD CONSTRAINT console_actions_target_kind_check
CHECK (target_kind IN ('account', 'member', 'ticket', 'listing'));
