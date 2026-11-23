-- Order emails (2026-11-23): whether order notices are mailed to the
-- account's email address. Existing rows keep mailing, as accounts
-- without a row do.
ALTER TABLE notification_settings ADD COLUMN order_emails boolean NOT NULL DEFAULT true;
ALTER TABLE notification_settings ALTER COLUMN order_emails DROP DEFAULT;
