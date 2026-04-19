-- Drops all account schema objects in reverse dependency order.
-- Indexes are dropped before tables; types after tables; schema last.

-- Indexes
DROP INDEX IF EXISTS "account"."idx_wallet_tx_account";
DROP INDEX IF EXISTS "account"."payment_method_account_default_key";
DROP INDEX IF EXISTS "account"."payment_method_account_id_idx";
DROP INDEX IF EXISTS "account"."favorite_spu_id_idx";
DROP INDEX IF EXISTS "account"."favorite_account_id_spu_id_key";
DROP INDEX IF EXISTS "account"."contact_account_id_idx";
DROP INDEX IF EXISTS "account"."notification_date_created_idx";
DROP INDEX IF EXISTS "account"."notification_channel_idx";
DROP INDEX IF EXISTS "account"."notification_type_idx";
DROP INDEX IF EXISTS "account"."notification_account_id_idx";
DROP INDEX IF EXISTS "account"."income_history_date_created_idx";
DROP INDEX IF EXISTS "account"."income_history_type_idx";
DROP INDEX IF EXISTS "account"."income_history_account_id_idx";
DROP INDEX IF EXISTS "account"."profile_default_contact_id_key";
DROP INDEX IF EXISTS "account"."profile_avatar_rs_id_key";
DROP INDEX IF EXISTS "account"."account_username_key";
DROP INDEX IF EXISTS "account"."account_email_key";
DROP INDEX IF EXISTS "account"."account_phone_key";

-- Tables (dependent tables first)
DROP TABLE IF EXISTS "account"."wallet_transaction";
DROP TABLE IF EXISTS "account"."wallet";
DROP TABLE IF EXISTS "account"."payment_method";
DROP TABLE IF EXISTS "account"."favorite";
DROP TABLE IF EXISTS "account"."contact";
DROP TABLE IF EXISTS "account"."notification";
DROP TABLE IF EXISTS "account"."income_history";
DROP TABLE IF EXISTS "account"."profile";
DROP TABLE IF EXISTS "account"."account";

-- Enums
DROP TYPE IF EXISTS "account"."wallet_transaction_type";
DROP TYPE IF EXISTS "account"."address_type";
DROP TYPE IF EXISTS "account"."gender";
DROP TYPE IF EXISTS "account"."status";

DROP SCHEMA IF EXISTS "account";
