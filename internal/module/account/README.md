# Account Module

Handles user identity, authentication, and account-related data. Accounts are **unified** -- any account can act as both buyer and seller. There are no separate customer/vendor account types.

**Struct**: `AccountHandler` | **Interface**: `AccountBiz` | **Restate service**: `Account`

---

## Features

### Authentication
- Register and login with email, phone, or username + password (bcrypt).
- JWT access tokens (HS512) with refresh token rotation using a separate signing secret.

### Profile
- Name, gender, date of birth, avatar (linked to resource management), description.
- Email and phone verification status tracking.

### Contacts
- Multiple shipping addresses per account (full name, phone, address, address type: Home/Work).
- First contact auto-set as default. Configurable default via profile.

### Favorites
- Add/remove SPUs to a wishlist. Paginated listing. Batch check if SPUs are favorited.
- Idempotent add -- returns existing record if already favorited.

### Payment Methods
- CRUD with JSONB data for flexible provider metadata.
- Exactly-one-default enforcement via partial unique index (`WHERE is_default = true`).
- Default swap wrapped in a transaction.

### Notifications
- Per-account notifications with type, channel, content, read tracking.
- Scheduled delivery support. Unread count endpoint.
- Mark individual or all notifications as read.

### Account Management
- `SuspendAccount` sets status to `Suspended` (soft delete, no row removal).

---

## Database Tables

All tables in the `account` schema.

| Table | Key Columns | Notes |
|-------|-------------|-------|
| `account` | id (UUID), number (identity), status, phone, email, username, password | Unique on phone, email, username |
| `profile` | id (FK to account), gender, name, description, date_of_birth, avatar_rs_id, default_contact_id | 1:1 with account |
| `contact` | id, account_id, full_name, phone, address, address_type | Multiple per account |
| `favorite` | id, account_id, spu_id | Unique on (account_id, spu_id) |
| `payment_method` | id, account_id, type, label, data (JSONB), is_default | Partial unique index for default |
| `notification` | id, account_id, type, channel, is_read, content, date_scheduled | Indexed on account, type, channel |
| `income_history` | id, account_id, type, income, current_balance, note | Append-only earnings ledger |

---

## Endpoints

All routes prefixed with `/api/v1/account`.

### Auth (no auth required)

| Method | Path | Description |
|--------|------|-------------|
| POST | `/auth/login/basic` | Login with identifier + password, returns tokens |
| POST | `/auth/register/basic` | Register new account, returns tokens |
| POST | `/auth/refresh` | Exchange refresh token for new token pair |

### Profile

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Get another account's profile by `account_id` query param |
| GET | `/me` | Get authenticated user's full profile |
| PATCH | `/me` | Update profile fields |

### Contacts

| Method | Path | Description |
|--------|------|-------------|
| GET | `/contact` | List all contacts |
| GET | `/contact/:contact_id` | Get specific contact |
| POST | `/contact` | Create contact (auto-default if first) |
| PATCH | `/contact` | Update contact |
| DELETE | `/contact` | Delete contact |

### Favorites

| Method | Path | Description |
|--------|------|-------------|
| POST | `/favorite/:spu_id` | Add SPU to favorites (idempotent) |
| DELETE | `/favorite/:spu_id` | Remove SPU from favorites |
| GET | `/favorite` | List favorites (paginated) |

### Payment Methods

| Method | Path | Description |
|--------|------|-------------|
| POST | `/payment-method` | Create payment method |
| GET | `/payment-method` | List payment methods (default first) |
| PATCH | `/payment-method` | Update payment method |
| DELETE | `/payment-method` | Delete payment method |
| PUT | `/payment-method/:id/default` | Set as default |

### Notifications

| Method | Path | Description |
|--------|------|-------------|
| GET | `/notification` | List notifications (paginated) |
| GET | `/notification/unread-count` | Get unread notification count |
| POST | `/notification/read` | Mark specific notifications as read |
| POST | `/notification/read-all` | Mark all notifications as read |

## ER Diagram

```mermaid
erDiagram
"account.profile" |o--|| "account.account" : "id"
"account.profile" }o--o| "account.contact" : "default_contact_id"
"account.notification" }o--|| "account.account" : "account_id"
"account.contact" }o--|| "account.account" : "account_id"
"account.favorite" }o--|| "account.account" : "account_id"
"account.payment_method" }o--|| "account.account" : "account_id"
"chat.conversation" }o--|| "account.account" : "customer_id"
"chat.message" }o--|| "account.account" : "sender_id"

"account.account" {
  uuid id
  status status
  varchar(50) phone
  varchar(255) email
  varchar(100) username
  varchar(255) password
  timestamptz date_created
  timestamptz date_updated
  bigint number
}
"account.contact" {
  uuid id
  uuid account_id FK
  varchar(100) full_name
  varchar(30) phone
  boolean phone_verified
  varchar(255) address
  address_type address_type
  timestamptz date_created
  timestamptz date_updated
  float8 latitude
  float8 longitude
}
"account.favorite" {
  bigint id
  uuid account_id FK
  uuid spu_id
  timestamptz date_created
}
"account.income_history" {
  bigint id
  uuid account_id
  varchar(50) type
  bigint income
  bigint current_balance
  varchar(100) note
  timestamptz date_created
}
"account.notification" {
  bigint id
  uuid account_id FK
  varchar(50) type
  varchar(50) channel
  boolean is_read
  text content
  timestamptz date_created
  timestamptz date_updated
  timestamptz date_sent
  timestamptz date_scheduled
  varchar(200) title
  jsonb metadata
}
"account.payment_method" {
  uuid id
  uuid account_id FK
  varchar(50) type
  varchar(100) label
  jsonb data
  boolean is_default
  timestamptz date_created
  timestamptz date_updated
}
"account.profile" {
  uuid id FK
  gender gender
  varchar(100) name
  timestamp date_of_birth
  uuid avatar_rs_id
  boolean email_verified
  boolean phone_verified
  uuid default_contact_id FK
  timestamptz date_created
  timestamptz date_updated
  text description
}
```
