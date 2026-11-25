-- Chợ Tốt links (2026-11-26): a seller proves a Chợ Tốt profile is theirs
-- with a code in its bio, then brings its listings over. ShopNexus only
-- reads Chợ Tốt.

-- One live code per account; asking again before expires_at answers it.
CREATE TABLE chotot_codes (
    account_id uuid PRIMARY KEY REFERENCES accounts (id),
    code       text NOT NULL CHECK (code ~ '^SN-[2-9A-HJKMNP-Z]{6}$'),
    expires_at timestamptz NOT NULL
);

-- A profile maps to one account and an account to one profile. seen_through
-- is the newest listing seen, so auto_import takes only newer ones; waiting
-- counts the listings that could still be brought over at the last check.
CREATE TABLE chotot_links (
    id             uuid PRIMARY KEY,
    account_id     uuid NOT NULL UNIQUE REFERENCES accounts (id),
    oid            text NOT NULL UNIQUE CHECK (oid ~ '^[0-9a-f]{32}$'),
    chotot_account bigint NOT NULL CHECK (chotot_account > 0),
    display_name   text NOT NULL,
    avatar_url     text NOT NULL,
    auto_import    boolean NOT NULL,
    follow_updates boolean NOT NULL,
    remind_hide    boolean NOT NULL,
    seen_through   bigint,
    waiting        integer CHECK (waiting >= 0),
    linked_at      timestamptz NOT NULL,
    checked_at     timestamptz
);

-- One import per account and Chợ Tốt listing. oid is the profile it came
-- from: a copy follows Chợ Tốt only while the account links that profile.
-- ad_price is the listing's price on Chợ Tốt as last seen; ad_gone marks a
-- copy hidden because its listing left Chợ Tốt, so checks skip it.
CREATE TABLE chotot_imports (
    id                uuid PRIMARY KEY,
    account_id        uuid NOT NULL REFERENCES accounts (id),
    list_id           bigint NOT NULL CHECK (list_id > 0),
    oid               text NOT NULL,
    import_state      text NOT NULL CHECK (import_state IN ('importing', 'listed', 'draft', 'skipped', 'unsupported')),
    listing_id        uuid REFERENCES listings (id),
    review            text[] NOT NULL,
    missing_spec_keys text[] NOT NULL,
    reason            text,
    draft             jsonb,
    ad_price          bigint NOT NULL CHECK (ad_price >= 0),
    ad_gone           boolean NOT NULL,
    updated_at        timestamptz NOT NULL,
    UNIQUE (account_id, list_id),
    CHECK ((import_state = 'listed') = (listing_id IS NOT NULL)),
    CHECK ((import_state = 'draft') = (draft IS NOT NULL))
);

CREATE UNIQUE INDEX chotot_imports_listing ON chotot_imports (listing_id) WHERE listing_id IS NOT NULL;
