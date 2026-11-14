-- Notification settings (2026-11-14): which notices push, and quiet hours
-- in Vietnam time as HH:MM. An account without a row reads every switch on
-- and quiet hours on from 22:00 to 07:00. They replace discovery's
-- saved_settings, whose one switch also stopped the price drop notice
-- itself; an account that had it off keeps price_cuts off.
CREATE TABLE notification_settings (
    account_id uuid PRIMARY KEY REFERENCES accounts (id),
    messages   boolean NOT NULL,
    offers     boolean NOT NULL,
    orders     boolean NOT NULL,
    price_cuts boolean NOT NULL,
    followed   boolean NOT NULL,
    reviews    boolean NOT NULL,
    quiet_on   boolean NOT NULL,
    quiet_from text NOT NULL CHECK (quiet_from ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    quiet_to   text NOT NULL CHECK (quiet_to ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
    CHECK (quiet_from <> quiet_to)
);

INSERT INTO notification_settings (
    account_id, messages, offers, orders, price_cuts, followed, reviews, quiet_on, quiet_from, quiet_to
)
SELECT
    account_id,
    true AS messages,
    true AS offers,
    true AS orders,
    false AS price_cuts,
    true AS followed,
    true AS reviews,
    true AS quiet_on,
    '22:00' AS quiet_from,
    '07:00' AS quiet_to
FROM saved_settings
WHERE NOT price_cut_alerts;

DROP TABLE saved_settings;
