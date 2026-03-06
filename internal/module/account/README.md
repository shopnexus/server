# Account Module

## Overview

The Account module is the identity and user-management core of the ShopNexus e-commerce platform. It is responsible for:

- **Authentication** -- registration (basic / OAuth-ready), login, JWT access and refresh token issuance.
- **User identity** -- a single `account` entity that can be either a **Customer** or a **Vendor**, differentiated by an enum type.
- **Profile management** -- name, gender, date of birth, avatar, email/phone verification status.
- **Contact / address book** -- multiple shipping addresses per account with a configurable default.
- **Favorites** -- users can favorite SPU (Standard Product Unit) items.
- **Payment methods** -- CRUD with JSONB-stored provider data and exactly-one-default enforcement.
- **Notifications** -- per-account notification records with type, channel, scheduling and read-tracking.
- **Vendor income history** -- an append-only ledger of vendor earnings and running balance.
## Database Schema

All tables reside in the PostgreSQL schema `account`.

### Enum Types

```sql
CREATE TYPE "account"."type"         AS ENUM ('Customer', 'Vendor');
CREATE TYPE "account"."status"       AS ENUM ('Active', 'Suspended');
CREATE TYPE "account"."gender"       AS ENUM ('Male', 'Female', 'Other');
CREATE TYPE "account"."address_type" AS ENUM ('Home', 'Work');
```

### Tables

#### account.account

The root identity table. Every user in the system has exactly one row here.

```sql
CREATE TABLE "account"."account" (
    "id"           UUID         NOT NULL DEFAULT gen_random_uuid(),
    "type"         account.type NOT NULL,
    "status"       account.status NOT NULL DEFAULT 'Active',
    "phone"        VARCHAR(50),
    "email"        VARCHAR(255),
    "username"     VARCHAR(100),
    "password"     VARCHAR(255),
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "number"       BIGINT NOT NULL GENERATED ALWAYS AS IDENTITY,
    CONSTRAINT "account_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `account_phone_key` | `phone` | UNIQUE |
| `account_email_key` | `email` | UNIQUE |
| `account_username_key` | `username` | UNIQUE |

The `number` column (added in migration 0002) is an auto-incrementing identity used as a human-readable account number.

#### account.profile

One-to-one extension of `account` for personal details.

```sql
CREATE TABLE "account"."profile" (
    "id"                 UUID          NOT NULL,  -- FK -> account.id
    "gender"             account.gender,
    "name"               VARCHAR(100),
    "date_of_birth"      TIMESTAMP(3),
    "avatar_rs_id"       UUID,                    -- resource storage reference
    "email_verified"     BOOLEAN       NOT NULL DEFAULT false,
    "phone_verified"     BOOLEAN       NOT NULL DEFAULT false,
    "default_contact_id" UUID,                    -- FK -> contact.id
    "date_created"       TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated"       TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "profile_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `profile_avatar_rs_id_key` | `avatar_rs_id` | UNIQUE |
| `profile_default_contact_id_key` | `default_contact_id` | UNIQUE |

**Foreign keys:**
- `profile.id` -> `account.account.id` (ON DELETE CASCADE)
- `profile.default_contact_id` -> `account.contact.id` (ON DELETE SET NULL)

#### account.customer

Subtype table for Customer accounts. Currently holds only timestamps; exists to enable future customer-specific fields.

```sql
CREATE TABLE "account"."customer" (
    "id"           UUID          NOT NULL,  -- FK -> account.id
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "customer_pkey" PRIMARY KEY ("id")
);
```

**Foreign keys:**
- `customer.id` -> `account.account.id` (ON DELETE CASCADE)

#### account.vendor

Subtype table for Vendor accounts.

```sql
CREATE TABLE "account"."vendor" (
    "id"          UUID NOT NULL,  -- FK -> account.id
    "description" TEXT NOT NULL DEFAULT '',
    CONSTRAINT "vendor_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `vendor_id_idx` | `id` | B-tree |

**Foreign keys:**
- `vendor.id` -> `account.account.id` (ON DELETE CASCADE)

#### account.income_history

Append-only ledger tracking vendor income events. Each entry records the income delta and the resulting running balance.

```sql
CREATE TABLE "account"."income_history" (
    "id"              BIGSERIAL      NOT NULL,
    "account_id"      UUID           NOT NULL,  -- FK -> vendor.id
    "type"            VARCHAR(50)    NOT NULL,
    "income"          BIGINT         NOT NULL,
    "current_balance" BIGINT         NOT NULL,
    "note"            VARCHAR(100),
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "income_history_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `income_history_account_id_idx` | `account_id` | B-tree |
| `income_history_type_idx` | `type` | B-tree |
| `income_history_date_created_idx` | `date_created` | B-tree |

**Foreign keys:**
- `income_history.account_id` -> `account.vendor.id` (ON DELETE CASCADE)

#### account.notification

Per-account notifications with optional scheduling and send-tracking.

```sql
CREATE TABLE "account"."notification" (
    "id"             BIGSERIAL      NOT NULL,
    "account_id"     UUID           NOT NULL,  -- FK -> account.id
    "type"           VARCHAR(50)    NOT NULL,
    "channel"        VARCHAR(50)    NOT NULL,
    "is_read"        BOOLEAN        NOT NULL DEFAULT false,
    "content"        TEXT           NOT NULL,
    "date_created"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated"   TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_sent"      TIMESTAMPTZ(3),
    "date_scheduled" TIMESTAMPTZ(3),
    CONSTRAINT "notification_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `notification_account_id_idx` | `account_id` | B-tree |
| `notification_type_idx` | `type` | B-tree |
| `notification_channel_idx` | `channel` | B-tree |
| `notification_date_created_idx` | `date_created` | B-tree |

**Foreign keys:**
- `notification.account_id` -> `account.account.id` (ON DELETE CASCADE)

#### account.contact

Address book entries. Each account may have multiple contacts (shipping addresses).

```sql
CREATE TABLE "account"."contact" (
    "id"             UUID               NOT NULL DEFAULT gen_random_uuid(),
    "account_id"     UUID               NOT NULL,  -- FK -> account.id
    "full_name"      VARCHAR(100)       NOT NULL,
    "phone"          VARCHAR(30)        NOT NULL,
    "phone_verified" BOOLEAN            NOT NULL DEFAULT false,
    "address"        VARCHAR(255)       NOT NULL,
    "address_type"   account.address_type NOT NULL,
    "date_created"   TIMESTAMPTZ(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated"   TIMESTAMPTZ(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "contact_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `contact_account_id_idx` | `account_id` | B-tree |

**Foreign keys:**
- `contact.account_id` -> `account.account.id` (ON DELETE CASCADE)

#### account.favorite

Tracks which SPUs (Standard Product Units) a user has favorited. Each (account, SPU) pair is unique.

```sql
CREATE TABLE "account"."favorite" (
    "id"           BIGSERIAL      NOT NULL,
    "account_id"   UUID           NOT NULL,  -- FK -> account.id
    "spu_id"       UUID           NOT NULL,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "favorite_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type |
|---|---|---|
| `favorite_account_id_spu_id_key` | `(account_id, spu_id)` | UNIQUE |
| `favorite_spu_id_idx` | `spu_id` | B-tree |

**Foreign keys:**
- `favorite.account_id` -> `account.account.id` (ON DELETE CASCADE)

#### account.payment_method

Stores payment methods with flexible JSONB data. A partial unique index enforces that at most one method per account can be the default.

```sql
CREATE TABLE "account"."payment_method" (
    "id"           UUID           NOT NULL DEFAULT gen_random_uuid(),
    "account_id"   UUID           NOT NULL,  -- FK -> account.id
    "type"         VARCHAR(50)    NOT NULL,
    "label"        VARCHAR(100)   NOT NULL,
    "data"         JSONB          NOT NULL,
    "is_default"   BOOLEAN        NOT NULL DEFAULT false,
    "date_created" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    "date_updated" TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "payment_method_pkey" PRIMARY KEY ("id")
);
```

| Index | Columns | Type | Note |
|---|---|---|---|
| `payment_method_account_id_idx` | `account_id` | B-tree | |
| `payment_method_account_default_key` | `account_id` | UNIQUE | Partial: `WHERE is_default = true` |

**Foreign keys:**
- `payment_method.account_id` -> `account.account.id` (ON DELETE CASCADE)

### Entity Relationship Summary

```
account.account (root)
  |-- 1:1  account.profile
  |-- 1:1  account.customer   (when type = 'Customer')
  |-- 1:1  account.vendor     (when type = 'Vendor')
  |-- 1:N  account.contact
  |-- 1:N  account.favorite
  |-- 1:N  account.notification
  |-- 1:N  account.payment_method

account.vendor
  |-- 1:N  account.income_history

account.profile
  |-- N:1  account.contact    (default_contact_id)
```

---

## API Endpoints

All routes are prefixed with `/api/v1/account`.

### Authentication

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `POST` | `/auth/login/basic` | `LoginBasic` | No | Login with username/email/phone + password. Returns access and refresh tokens. |
| `POST` | `/auth/register/basic` | `RegisterBasic` | No | Register a new Customer or Vendor account. Returns access and refresh tokens. |
| `POST` | `/auth/refresh` | `Refresh` | No | Exchange a valid refresh token for a new access + refresh token pair. |

### Account

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `GET` | `/` | `GetAccount` | Yes | Retrieve another account's profile by `account_id` query parameter. |

### Me (Current User Profile)

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `GET` | `/me` | `GetMe` | Yes | Get the authenticated user's full profile (account + profile + vendor details if applicable). |
| `PATCH` | `/me` | `UpdateMe` | Yes | Update the authenticated user's profile fields (username, phone, email, name, gender, DOB, avatar, default contact, vendor description). |

### Contacts

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `GET` | `/contact` | `ListContact` | Yes | List all contacts for the authenticated user. |
| `GET` | `/contact/:contact_id` | `GetContact` | Yes | Get a specific contact by ID. |
| `POST` | `/contact` | `CreateContact` | Yes | Create a new contact. If it is the first contact, it is automatically set as the default. |
| `PATCH` | `/contact` | `UpdateContact` | Yes | Update an existing contact's fields. |
| `DELETE` | `/contact` | `DeleteContact` | Yes | Delete a contact by ID (passed in request body). |

### Favorites

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `POST` | `/favorite/:spu_id` | `AddFavorite` | Yes | Add an SPU to favorites. Idempotent -- returns existing if already favorited. |
| `DELETE` | `/favorite/:spu_id` | `RemoveFavorite` | Yes | Remove an SPU from favorites. |
| `GET` | `/favorite` | `ListFavorite` | Yes | List the authenticated user's favorites (paginated). |
| `GET` | `/favorite/:spu_id/check` | `CheckFavorite` | Yes | Check whether a specific SPU is favorited. Returns `{ "is_favorited": bool }`. |

### Payment Methods

| Method | Path | Handler | Auth | Description |
|---|---|---|---|---|
| `POST` | `/payment-method` | `CreatePaymentMethod` | Yes | Create a new payment method. If `is_default` is true, the previous default is unset within a transaction. |
| `GET` | `/payment-method` | `ListPaymentMethod` | Yes | List the authenticated user's payment methods (paginated, default first). |
| `PATCH` | `/payment-method` | `UpdatePaymentMethod` | Yes | Update type, label, or data of a payment method. |
| `DELETE` | `/payment-method` | `DeletePaymentMethod` | Yes | Delete a payment method by ID (passed in request body). |
| `PUT` | `/payment-method/:id/default` | `SetDefaultPaymentMethod` | Yes | Set a specific payment method as the default (unsets previous default in a transaction). |

---

## Business Logic Layer

The business logic lives in `internal/module/account/biz/` and is encapsulated in the `AccountBiz` struct.

### AccountBiz Struct

```go
type AccountBiz struct {
    tokenDuration        time.Duration
    jwtSecret            []byte
    refreshTokenDuration time.Duration
    refreshSecret        []byte
    storage              AccountStorage
    pubsub               pubsub.Client
    common               *commonbiz.CommonBiz
}
```

The constructor `NewAccountBiz` accepts a `*config.Config`, an `AccountStorage`, a `pubsub.Client`, and a `*commonbiz.CommonBiz`. JWT durations and secrets are read from configuration.

### Authentication (`biz/auth.go`)

- **Login** -- Accepts username, email, or phone (at least one required) plus a password. Looks up the account, verifies bcrypt hash, then issues both an access token and a refresh token.
- **Register** -- Creates the account, profile, and subtype row (customer or vendor) inside a **single database transaction**. Hashes the password with bcrypt (cost 10). Returns tokens immediately. Supports passwordless registration (OAuth placeholder) when an email is provided.
- **Refresh** -- Validates the refresh token against `RefreshSecret`, loads the account from the database, then issues a fresh access + refresh token pair.
- **Token generation** -- Access tokens use HS512, include standard JWT registered claims (`iss`, `sub`, `aud`, `iat`, `exp`), and carry a custom `Account` claim containing `Type`, `ID`, and `Number`. Refresh tokens follow the same structure with a longer expiry and a separate signing secret.

### Profile (`biz/profile.go`)

- **GetProfile** -- Fetches the account, profile, and (if vendor) vendor record, then assembles them into a unified `Profile` model. Resolves the avatar resource ID into a URL via `CommonBiz`.
- **ListProfile** -- Paginated listing of profiles for a set of account IDs. Joins account and profile data and maps to the `Profile` model.
- **UpdateProfile** -- Updates account-level fields (status, username, phone, email), profile-level fields (gender, name, DOB, avatar, default contact), and subtype-level fields (vendor description) within a **single transaction**.

### Contact (`biz/contact.go`)

- **ListContact / GetContact** -- Scoped to the authenticated user's account ID.
- **CreateContact** -- Creates a contact within a transaction. If the new contact is the user's first contact, it is automatically set as the profile's `default_contact_id`.
- **UpdateContact** -- Partial update using COALESCE semantics (only non-nil fields are overwritten).
- **DeleteContact** -- Deletes by contact ID, scoped to the authenticated user's account.
- **GetDefaultContact** -- Batch-loads default contacts for a list of account IDs via a join on `profile.default_contact_id`.

### Favorite (`biz/favorite.go`)

- **AddFavorite** -- Idempotent: checks for an existing favorite first and returns it without error if found.
- **RemoveFavorite** -- Deletes the favorite by `(account_id, spu_id)`.
- **ListFavorite** -- Paginated, ordered by `date_created DESC`.
- **CheckFavorite** -- Returns a boolean indicating whether the SPU is favorited.

### Payment Method (`biz/payment_method.go`)

- **CreatePaymentMethod** -- If `is_default` is true, the existing default is unset first, all within a transaction to satisfy the partial unique index.
- **ListPaymentMethod** -- Paginated, ordered by `is_default DESC, date_created DESC` (default method appears first).
- **UpdatePaymentMethod** -- Partial update of type, label, and/or data.
- **DeletePaymentMethod** -- Scoped to the authenticated user.
- **SetDefaultPaymentMethod** -- Unsets the current default and sets the new one, wrapped in a transaction.

### Account (`biz/account.go`)

- **DeleteAccount** -- Performs a soft delete by setting the account status to `Suspended`.

---

## Models and Types

### Domain Models (`model/`)

#### `Profile` (`model/account.go`)

A composite view combining data from `account`, `profile`, and optionally `vendor`:

```go
type Profile struct {
    ID          uuid.UUID
    DateCreated time.Time
    DateUpdated time.Time
    Type        AccountType
    Status      AccountStatus
    Phone       null.String
    Email       null.String
    Username    null.String
    Gender      null.Value[AccountGender]
    Name        null.String
    DateOfBirth null.Time
    EmailVerified    bool
    PhoneVerified    bool
    DefaultContactID uuid.NullUUID
    AvatarURL        null.String
    Description      null.String   // Vendor only
}
```

#### `Claims` (`model/claims.go`)

JWT claims embedded in every access and refresh token:

```go
type Claims struct {
    jwt.RegisteredClaims
    Account AuthenticatedAccount
}

type AuthenticatedAccount struct {
    Type   AccountType
    ID     uuid.UUID
    Number int64
}
```

#### Sentinel Errors (`model/error.go`)

```go
ErrInvalidCredentials  // "Invalid credentials provided"
ErrAccountNotFound     // "Account not found"
ErrMissingIdentifier   // "At least one of username, email, or phone must be provided"
```

### SQLC-Generated Types (`db/sqlc/models.go`)

The following Go enum types are generated from PostgreSQL enums:

| Go Type | Values |
|---|---|
| `AccountType` | `Customer`, `Vendor` |
| `AccountStatus` | `Active`, `Suspended` |
| `AccountGender` | `Male`, `Female`, `Other` |
| `AccountAddressType` | `Home`, `Work` |

Each enum type has a `Valid() bool` method and a corresponding `Null*` wrapper type for nullable columns.

The following struct types are generated from database tables:

| Go Type | Source Table |
|---|---|
| `AccountAccount` | `account.account` |
| `AccountProfile` | `account.profile` |
| `AccountCustomer` | `account.customer` |
| `AccountVendor` | `account.vendor` |
| `AccountIncomeHistory` | `account.income_history` |
| `AccountNotification` | `account.notification` |
| `AccountContact` | `account.contact` |
| `AccountFavorite` | `account.favorite` |
| `AccountPaymentMethod` | `account.payment_method` |

---
## Key Patterns
### Authentication Flow

1. **Registration** creates account + profile + subtype rows in a single transaction, then generates JWT tokens.
2. **Login** looks up the account by any identifier (username, email, or phone), verifies the bcrypt password hash, and returns tokens.
3. **Refresh** validates the refresh token (signed with a separate secret), reloads the account, and issues new tokens.
4. All authenticated endpoints extract claims from the request via `authclaims.GetClaims(r)` which parses and validates the JWT from the Authorization header.

### Soft Delete

Account deletion does not remove the row. Instead, `DeleteAccount` updates the account status to `Suspended`.
### Idempotent Favorites

`AddFavorite` checks for an existing favorite before inserting and returns the existing record without error, making the operation safely idempotent.

### Payment Method Default Enforcement

A partial unique index (`WHERE is_default = true`) ensures at most one default payment method per account at the database level. The business logic wraps the default swap (unset old + set new) in a transaction to maintain consistency.
