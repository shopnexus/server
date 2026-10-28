-- Sign-in goes through Firebase: server2 sends no codes and knows phone
-- numbers only.
DROP TABLE otp_challenges, otp_quotas;

DELETE FROM identities WHERE kind <> 'phone';
DELETE FROM credentials c
WHERE NOT EXISTS (SELECT 1 FROM identities i WHERE i.account_id = c.account_id);
-- An account left without credentials that owns rows elsewhere stops the
-- migration on that foreign key; no other row is deleted with it.
DELETE FROM accounts a
WHERE NOT EXISTS (SELECT 1 FROM credentials c WHERE c.account_id = a.id);

ALTER TABLE identities
    DROP CONSTRAINT identities_kind_check,
    ADD CONSTRAINT identities_kind_check CHECK (kind = 'phone');
