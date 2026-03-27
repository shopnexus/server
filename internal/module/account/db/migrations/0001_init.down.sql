-- Drops all account schema objects in reverse dependency order.
-- Indexes are dropped before tables; types after tables; schema last.

-- Indexes
DROP INDEX IF EXISTS "payment_method_account_default_key";
DROP INDEX IF EXISTS "payment_method_account_id_idx";
DROP INDEX IF EXISTS "favorite_spu_id_idx";
DROP INDEX IF EXISTS "favorite_account_id_spu_id_key";
DROP INDEX IF EXISTS "contact_account_id_idx";
DROP INDEX IF EXISTS "notification_date_created_idx";
DROP INDEX IF EXISTS "notification_channel_idx";
DROP INDEX IF EXISTS "notification_type_idx";
DROP INDEX IF EXISTS "notification_account_id_idx";
DROP INDEX IF EXISTS "income_history_date_created_idx";
DROP INDEX IF EXISTS "income_history_type_idx";
DROP INDEX IF EXISTS "income_history_account_id_idx";
DROP INDEX IF EXISTS "profile_default_contact_id_key";
DROP INDEX IF EXISTS "profile_avatar_rs_id_key";
DROP INDEX IF EXISTS "account_username_key";
DROP INDEX IF EXISTS "account_email_key";
DROP INDEX IF EXISTS "account_phone_key";

-- Tables (dependent tables first)
DROP TABLE IF EXISTS "account"."payment_method";
DROP TABLE IF EXISTS "account"."favorite";
DROP TABLE IF EXISTS "account"."contact";
DROP TABLE IF EXISTS "account"."notification";
DROP TABLE IF EXISTS "account"."income_history";
DROP TABLE IF EXISTS "account"."profile";
DROP TABLE IF EXISTS "account"."account";

-- Enums
DROP TYPE IF EXISTS "account"."address_type";
DROP TYPE IF EXISTS "account"."gender";
DROP TYPE IF EXISTS "account"."status";

DROP SCHEMA IF EXISTS "account";
