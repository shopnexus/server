-- A carrier token is one sealed value, its nonce first (platform/secret),
-- as the console's TOTP secrets are.
ALTER TABLE carrier_accounts DROP CONSTRAINT carrier_accounts_token_check;
UPDATE carrier_accounts SET token = nonce || token WHERE token IS NOT NULL;
ALTER TABLE carrier_accounts
    DROP COLUMN nonce,
    ADD CONSTRAINT carrier_accounts_token_check CHECK (token IS NOT NULL OR carrier = 'ghn');
