CREATE TABLE shipping_settings (
    account_id uuid PRIMARY KEY REFERENCES accounts (id),
    address_id uuid NOT NULL REFERENCES addresses (id) ON DELETE CASCADE,
    phone      text NOT NULL
);
CREATE INDEX shipping_settings_address ON shipping_settings (address_id);

CREATE TABLE ticket_message_receipts (
    message_id uuid PRIMARY KEY,
    ticket_id  uuid NOT NULL REFERENCES tickets (id) ON DELETE CASCADE
);
CREATE INDEX ticket_message_receipts_ticket ON ticket_message_receipts (ticket_id);
