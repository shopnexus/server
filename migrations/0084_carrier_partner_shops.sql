-- A GHN shop a seller linked by a texted code has ShopNexus as staff and
-- no token of its own: token and nonce are NULL, and only at GHN.
ALTER TABLE carrier_accounts
    ALTER COLUMN token DROP NOT NULL,
    ALTER COLUMN nonce DROP NOT NULL,
    ADD CONSTRAINT carrier_accounts_token_check CHECK (
        (token IS NULL) = (nonce IS NULL) AND (token IS NOT NULL OR carrier = 'ghn')
    );
