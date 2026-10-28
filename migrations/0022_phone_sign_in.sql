-- Sign-in goes through Firebase: server2 sends no codes and knows phone
-- numbers only.
DROP TABLE otp_challenges, otp_quotas;

DELETE FROM identities WHERE kind <> 'phone';
DELETE FROM credentials c
WHERE NOT EXISTS (SELECT 1 FROM identities i WHERE i.account_id = c.account_id);
-- Sessions and push tokens belong to sign-in and go with the account; an
-- account left without credentials that owns other rows stops the migration
-- on that foreign key.
DELETE FROM push_tokens p
WHERE NOT EXISTS (SELECT 1 FROM credentials c WHERE c.account_id = p.account_id);
DELETE FROM sessions s
WHERE NOT EXISTS (SELECT 1 FROM credentials c WHERE c.account_id = s.account_id);
DELETE FROM accounts a
WHERE NOT EXISTS (SELECT 1 FROM credentials c WHERE c.account_id = a.id);

ALTER TABLE identities
    DROP CONSTRAINT identities_kind_check,
    ADD CONSTRAINT identities_kind_check CHECK (kind = 'phone');
