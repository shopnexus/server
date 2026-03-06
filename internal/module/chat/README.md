# Chat Module

## Overview

The chat module provides real-time messaging between customers and vendors in the ShopNexus e-commerce platform. It combines a traditional REST API for resource management and history retrieval with a persistent WebSocket connection for live message delivery and read receipts.

Key capabilities:

- One-to-one conversations between a customer and a vendor.
- Real-time message delivery over WebSocket (gorilla/websocket).
- Three message types: Text, Image, and System.
- Message status tracking with a read receipt system (Sent -> Delivered -> Read).
- Paginated REST endpoints for listing conversations and message history.
- JWT-based authentication on both HTTP and WebSocket transports.
- Dependency injection via Uber fx.
- Type-safe database access via SQLC with pgx v5.

---
## Database Schema

All tables reside in the `chat` PostgreSQL schema.

### Enums

```sql
CREATE TYPE "chat"."message_type" AS ENUM ('Text', 'Image', 'System');
CREATE TYPE "chat"."message_status" AS ENUM ('Sent', 'Delivered', 'Read');
```

| Enum             | Values                    | Description                               |
|------------------|---------------------------|-------------------------------------------|
| `message_type`   | `Text`, `Image`, `System` | The content format of a message.          |
| `message_status` | `Sent`, `Delivered`, `Read` | Delivery lifecycle stage of a message.  |

### Table: `chat.conversation`

Represents a one-to-one channel between a customer and a vendor.

```sql
CREATE TABLE IF NOT EXISTS "chat"."conversation" (
    "id"              UUID NOT NULL DEFAULT gen_random_uuid(),
    "customer_id"     UUID NOT NULL,
    "vendor_id"       UUID NOT NULL,
    "last_message_at" TIMESTAMPTZ(3),
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "conversation_pkey" PRIMARY KEY ("id")
);
```

| Column           | Type           | Nullable | Default             | Description                          |
|------------------|----------------|----------|---------------------|--------------------------------------|
| `id`             | `UUID`         | NOT NULL | `gen_random_uuid()` | Primary key.                         |
| `customer_id`    | `UUID`         | NOT NULL | --                  | FK to `account.account(id)`.         |
| `vendor_id`      | `UUID`         | NOT NULL | --                  | FK to `account.vendor(id)`.          |
| `last_message_at`| `TIMESTAMPTZ(3)` | NULL   | --                  | Timestamp of the most recent message.|
| `date_created`   | `TIMESTAMPTZ(3)` | NOT NULL | `CURRENT_TIMESTAMP` | Row creation timestamp.           |

**Indexes:**

| Index Name                          | Columns                      | Unique | Notes                                   |
|-------------------------------------|------------------------------|--------|-----------------------------------------|
| `conversation_pkey`                 | `id`                         | Yes    | Primary key.                            |
| `conversation_customer_vendor_key`  | `(customer_id, vendor_id)`   | Yes    | Ensures one conversation per pair.      |
| `conversation_customer_id_idx`      | `customer_id`                | No     | Fast lookup by customer.                |
| `conversation_vendor_id_idx`        | `vendor_id`                  | No     | Fast lookup by vendor.                  |
| `conversation_last_message_at_idx`  | `last_message_at DESC`       | No     | Ordering conversations by recency.      |

**Foreign Keys:**

| Constraint                        | Column        | References              | On Delete | On Update |
|-----------------------------------|---------------|-------------------------|-----------|-----------|
| `conversation_customer_id_fkey`   | `customer_id` | `account.account(id)`   | CASCADE   | CASCADE   |
| `conversation_vendor_id_fkey`     | `vendor_id`   | `account.vendor(id)`    | CASCADE   | CASCADE   |

### Table: `chat.message`

Stores individual messages within a conversation.

```sql
CREATE TABLE IF NOT EXISTS "chat"."message" (
    "id"              BIGSERIAL NOT NULL,
    "conversation_id" UUID NOT NULL,
    "sender_id"       UUID NOT NULL,
    "type"            "chat"."message_type" NOT NULL DEFAULT 'Text',
    "content"         TEXT NOT NULL,
    "status"          "chat"."message_status" NOT NULL DEFAULT 'Sent',
    "metadata"        JSONB,
    "date_created"    TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "message_pkey" PRIMARY KEY ("id")
);
```

| Column            | Type                   | Nullable | Default             | Description                              |
|-------------------|------------------------|----------|---------------------|------------------------------------------|
| `id`              | `BIGSERIAL`            | NOT NULL | auto-increment      | Primary key.                             |
| `conversation_id` | `UUID`                 | NOT NULL | --                  | FK to `chat.conversation(id)`.           |
| `sender_id`       | `UUID`                 | NOT NULL | --                  | FK to `account.account(id)`.             |
| `type`            | `chat.message_type`    | NOT NULL | `'Text'`            | Content type of the message.             |
| `content`         | `TEXT`                 | NOT NULL | --                  | Message body (text content or image URL).|
| `status`          | `chat.message_status`  | NOT NULL | `'Sent'`            | Current delivery status.                 |
| `metadata`        | `JSONB`                | NULL     | --                  | Arbitrary JSON metadata.                 |
| `date_created`    | `TIMESTAMPTZ(3)`       | NOT NULL | `CURRENT_TIMESTAMP` | Message creation timestamp.              |

**Indexes:**

| Index Name                    | Columns                              | Notes                                          |
|-------------------------------|--------------------------------------|-------------------------------------------------|
| `message_pkey`                | `id`                                 | Primary key.                                    |
| `message_conversation_id_idx` | `(conversation_id, date_created DESC)` | Efficient paginated message listing per conversation. |
| `message_sender_id_idx`       | `sender_id`                          | Lookup by sender.                               |

**Foreign Keys:**

| Constraint                    | Column            | References                | On Delete | On Update |
|-------------------------------|-------------------|---------------------------|-----------|-----------|
| `message_conversation_id_fkey`| `conversation_id` | `chat.conversation(id)`   | CASCADE   | CASCADE   |
| `message_sender_id_fkey`      | `sender_id`       | `account.account(id)`     | CASCADE   | CASCADE   |

---

## API Endpoints (HTTP)

All REST endpoints are mounted under `/api/v1/chat`. Every request requires a valid JWT in the authorization header.

| Method | Path                                 | Handler              | Description                                          |
|--------|--------------------------------------|----------------------|------------------------------------------------------|
| POST   | `/api/v1/chat/conversation`          | `CreateConversation` | Create a new conversation (or return existing one).   |
| GET    | `/api/v1/chat/conversation`          | `ListConversation`   | List conversations for the authenticated account.     |
| GET    | `/api/v1/chat/conversation/:id/messages` | `ListMessage`    | List messages in a conversation (paginated, DESC).    |

### POST `/api/v1/chat/conversation`

Creates a conversation between the authenticated customer and a vendor. If a conversation between the same pair already exists, it returns the existing one (idempotent).

**Request Body:**

```json
{
  "vendor_id": "uuid"
}
```

**Response:** `200 OK` with the `ChatConversation` object.

### GET `/api/v1/chat/conversation`

Returns a paginated list of all conversations the authenticated account participates in (as either customer or vendor), ordered by `last_message_at DESC NULLS LAST`.

**Query Parameters:**

| Parameter | Type  | Required | Default | Description                |
|-----------|-------|----------|---------|----------------------------|
| `page`    | int32 | No       | 1       | Page number (1-indexed).   |
| `limit`   | int32 | No       | 10      | Items per page (max 100).  |

**Response:** Paginated result containing `ChatConversation[]`, total count, and page metadata.

### GET `/api/v1/chat/conversation/:id/messages`

Returns a paginated list of messages for the given conversation, ordered by `date_created DESC` (newest first). The caller must be a participant of the conversation.

**Path Parameters:**

| Parameter | Type | Description       |
|-----------|------|-------------------|
| `id`      | UUID | Conversation ID.  |

**Query Parameters:**

| Parameter | Type  | Required | Default | Description                |
|-----------|-------|----------|---------|----------------------------|
| `page`    | int32 | No       | 1       | Page number (1-indexed).   |
| `limit`   | int32 | No       | 10      | Items per page (max 100).  |

**Response:** Paginated result containing `ChatMessage[]`, total count, and page metadata.

---

## WebSocket Protocol

### Connection

```
GET /ws/chat
```

The WebSocket endpoint is mounted at `/ws/chat` on the root Echo instance (not under the `/api/v1/chat` group). Authentication is performed via the same JWT mechanism used by HTTP endpoints -- the token is extracted from the request headers during the upgrade handshake via `authclaims.GetClaims(r)`.

On successful connection, the server registers the client in an in-memory map keyed by `account_id` (UUID). When the connection closes, the client is removed from the map.

The `websocket.Upgrader` is configured with `CheckOrigin: func(r *http.Request) bool { return true }`, allowing connections from any origin.

### Message Envelope

All WebSocket messages (both client-to-server and server-to-client) share a common JSON envelope:

```json
{
  "type": "<message_type>",
  "data": { ... }
}
```

This maps to the `WSMessage` struct:

```go
type WSMessage struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}
```

### Client-to-Server Message Types

#### `send_message`

Send a new chat message within a conversation.

```json
{
  "type": "send_message",
  "data": {
    "conversation_id": "uuid",
    "type": "Text",
    "content": "Hello!",
    "metadata": {}
  }
}
```

| Field              | Type   | Required | Description                                      |
|--------------------|--------|----------|--------------------------------------------------|
| `conversation_id`  | UUID   | Yes      | Target conversation.                             |
| `type`             | string | Yes      | One of `Text`, `Image`, `System`.                |
| `content`          | string | Yes      | Message body.                                    |
| `metadata`         | object | No       | Arbitrary key-value metadata (stored as JSONB).  |

Maps to the `WSSendMessage` DTO:

```go
type WSSendMessage struct {
    ConversationID uuid.UUID              `json:"conversation_id"`
    Type           chatdb.ChatMessageType `json:"type"`
    Content        string                 `json:"content"`
    Metadata       map[string]any         `json:"metadata,omitempty"`
}
```

#### `mark_read`

Mark all unread messages from the other participant in a conversation as read.

```json
{
  "type": "mark_read",
  "data": {
    "conversation_id": "uuid"
  }
}
```

Maps to the `WSMarkRead` DTO:

```go
type WSMarkRead struct {
    ConversationID uuid.UUID `json:"conversation_id"`
}
```

### Server-to-Client Message Types

#### `new_message`

Broadcasted to both participants when a message is sent. The `data` payload is the full `ChatMessage` database row.

```json
{
  "type": "new_message",
  "data": {
    "id": 42,
    "conversation_id": "uuid",
    "sender_id": "uuid",
    "type": "Text",
    "content": "Hello!",
    "status": "Sent",
    "metadata": null,
    "date_created": "2025-01-15T10:30:00.000Z"
  }
}
```

#### `read_receipt`

Sent to the other participant when a user marks messages as read.

```json
{
  "type": "read_receipt",
  "data": {
    "conversation_id": "uuid",
    "reader_id": "uuid"
  }
}
```

#### `error`

Sent to the client when a request fails.

```json
{
  "type": "error",
  "data": {
    "message": "error description"
  }
}
```

### WebSocket Message Type Constants

Defined in `chatmodel`:

```go
const (
    WSTypeSendMessage = "send_message"  // Client -> Server: send a message
    WSTypeNewMessage  = "new_message"   // Server -> Client: new message notification
    WSTypeMarkRead    = "mark_read"     // Client -> Server: mark conversation as read
    WSTypeReadReceipt = "read_receipt"  // Server -> Client: read receipt notification
    WSTypeError       = "error"         // Server -> Client: error notification
)
```

---

## Business Logic

The `ChatBiz` struct in `biz/chat.go` encapsulates all business rules and orchestrates database operations via the `ChatStorage` interface (a generic `pgsqlc.Storage` wrapping the SQLC `*Queries`).

### CreateConversation

1. Checks if a conversation between the authenticated customer and the target vendor already exists (via `GetConversationByParticipants`).
2. If it exists, returns the existing conversation (idempotent).
3. Otherwise, creates and returns a new conversation.

The uniqueness constraint `conversation_customer_vendor_key` on `(customer_id, vendor_id)` provides an additional database-level guarantee against duplicates.

### SendMessage

1. Fetches the conversation by ID.
2. Verifies the authenticated account is a participant (either `customer_id` or `vendor_id`).
3. Within a database transaction:
   - Inserts the new message row.
   - Updates the conversation's `last_message_at` timestamp to `CURRENT_TIMESTAMP`.
4. Returns the created message.

### ListConversation

1. Constrains pagination parameters (default limit 10, max 100).
2. Fetches conversations where the account is either `customer_id` or `vendor_id`, ordered by `last_message_at DESC NULLS LAST`.
3. Fetches the total count for pagination metadata.

### ListMessage

1. Constrains pagination parameters.
2. Verifies the authenticated account is a participant of the conversation.
3. Fetches messages ordered by `date_created DESC` (newest first).
4. Fetches the total count for pagination metadata.

### MarkRead

Bulk-updates all messages in a conversation where:
- The sender is NOT the current user (you only mark the other person's messages as read).
- The status is not already `'Read'`.

Sets their status to `'Read'`.

---

## Real-Time Messaging Architecture

### Connection Lifecycle

```
Client                          Server
  |                               |
  |--- GET /ws/chat (Upgrade) --->|  JWT extracted from headers
  |<-- 101 Switching Protocols ---|  Client registered in h.clients[accountID]
  |                               |
  |--- { send_message } -------->|  Message persisted to DB
  |<-- { new_message } ----------|  Echo back to sender
  |         +-------------------->|  Forward to recipient (if online)
  |                               |
  |--- { mark_read } ----------->|  Bulk update in DB
  |         +-------------------->|  read_receipt sent to other participant
  |                               |
  |--- connection close -------->|  Client removed from h.clients
```

### Client Registry

The `Handler` struct maintains a concurrent-safe in-memory map of connected clients:

```go
type Handler struct {
    biz       *chatbiz.ChatBiz
    upgrader  websocket.Upgrader
    clients   map[uuid.UUID]*websocket.Conn
    clientsMu sync.RWMutex
}
```

- **Write lock** (`clientsMu.Lock()`) is acquired when adding or removing clients.
- **Read lock** (`clientsMu.RLock()`) is acquired when sending messages to a specific client via `sendToClient`.

### Message Delivery Flow

When a client sends a `send_message`:

1. The payload is deserialized into `WSSendMessage`.
2. `biz.SendMessage` persists the message and updates `last_message_at` in a transaction.
3. The conversation is fetched to determine the recipient ID.
4. A `new_message` event containing the full `ChatMessage` is sent to both:
   - The sender (confirmation / echo).
   - The recipient (if currently connected).

If the recipient is not connected, the message is still persisted in the database and will be available via the REST `ListMessage` endpoint when they come online.

### Offline Handling

There is no explicit offline queuing mechanism. Messages are always persisted to PostgreSQL. Clients that reconnect can retrieve missed messages via the paginated `GET /api/v1/chat/conversation/:id/messages` endpoint.

---

## Read Receipt System

The read receipt system tracks whether the other participant has seen messages in a conversation.

### Database-Level

Each message has a `status` column with the enum type `chat.message_status`:

| Status      | Description                                      |
|-------------|--------------------------------------------------|
| `Sent`      | Default. The message has been written to the DB.  |
| `Delivered` | Reserved for future use (not currently set).      |
| `Read`      | The recipient has marked the message as read.     |

The `MarkMessagesRead` query performs a bulk update:

```sql
UPDATE "chat"."message"
SET "status" = 'Read'
WHERE "conversation_id" = $1
    AND "sender_id" != $2        -- Only mark the OTHER person's messages
    AND "status" != 'Read';      -- Skip already-read messages
```

The `CountUnreadMessages` query returns the number of unread messages for a given reader in a conversation:

```sql
SELECT COUNT(*)
FROM "chat"."message"
WHERE "conversation_id" = $1
    AND "sender_id" != $2
    AND "status" != 'Read';
```

### Real-Time Notification

When a user sends a `mark_read` WebSocket message:

1. The server calls `biz.MarkRead`, which bulk-updates message statuses in the database.
2. The server determines the other participant in the conversation.
3. A `read_receipt` event is sent to the other participant (if online) containing `conversation_id` and `reader_id`.

This allows the other participant's client to update its UI in real time (e.g., changing message status indicators from "sent" to "read").

---

## Models and Types

### SQLC-Generated Models (`db/sqlc/models.go`)

```go
type ChatMessageStatus string

const (
    ChatMessageStatusSent      ChatMessageStatus = "Sent"
    ChatMessageStatusDelivered ChatMessageStatus = "Delivered"
    ChatMessageStatusRead      ChatMessageStatus = "Read"
)

type ChatMessageType string

const (
    ChatMessageTypeText   ChatMessageType = "Text"
    ChatMessageTypeImage  ChatMessageType = "Image"
    ChatMessageTypeSystem ChatMessageType = "System"
)

type ChatConversation struct {
    ID            uuid.UUID `json:"id"`
    CustomerID    uuid.UUID `json:"customer_id"`
    VendorID      uuid.UUID `json:"vendor_id"`
    LastMessageAt null.Time `json:"last_message_at"`
    DateCreated   time.Time `json:"date_created"`
}

type ChatMessage struct {
    ID             int64             `json:"id"`
    ConversationID uuid.UUID         `json:"conversation_id"`
    SenderID       uuid.UUID         `json:"sender_id"`
    Type           ChatMessageType   `json:"type"`
    Content        string            `json:"content"`
    Status         ChatMessageStatus `json:"status"`
    Metadata       json.RawMessage   `json:"metadata"`
    DateCreated    time.Time         `json:"date_created"`
}
```

Both enum types implement `database/sql/driver.Scanner` and have `Valid()` methods for validation. Nullable variants (`NullChatMessageStatus`, `NullChatMessageType`) are also generated.

### WebSocket DTOs (`model/chat.go`)

```go
// Envelope for all WS messages
type WSMessage struct {
    Type string      `json:"type"`
    Data interface{} `json:"data"`
}

// Client -> Server: send a message
type WSSendMessage struct {
    ConversationID uuid.UUID              `json:"conversation_id"`
    Type           chatdb.ChatMessageType `json:"type"`
    Content        string                 `json:"content"`
    Metadata       map[string]any         `json:"metadata,omitempty"`
}

// Client -> Server: mark messages as read
type WSMarkRead struct {
    ConversationID uuid.UUID `json:"conversation_id"`
}
```

### HTTP Request DTOs (`transport/echo/chat.go`)

```go
type CreateConversationRequest struct {
    VendorID uuid.UUID `json:"vendor_id" validate:"required"`
}

type ListConversationRequest struct {
    sharedmodel.PaginationParams
}

type ListMessageRequest struct {
    ConversationID uuid.UUID `param:"id" validate:"required"`
    sharedmodel.PaginationParams
}
```

### Business Logic Parameter Types (`biz/chat.go`)

```go
type CreateConversationParams struct {
    Account  accountmodel.AuthenticatedAccount
    VendorID uuid.UUID `validate:"required"`
}

type ListConversationParams struct {
    Account accountmodel.AuthenticatedAccount
    sharedmodel.PaginationParams
}

type SendMessageParams struct {
    Account        accountmodel.AuthenticatedAccount
    ConversationID uuid.UUID              `validate:"required"`
    Type           chatdb.ChatMessageType `validate:"required,validateFn=Valid"`
    Content        string                 `validate:"required"`
    Metadata       json.RawMessage
}

type ListMessageParams struct {
    Account        accountmodel.AuthenticatedAccount
    ConversationID uuid.UUID `validate:"required"`
    sharedmodel.PaginationParams
}

type MarkReadParams struct {
    Account        accountmodel.AuthenticatedAccount
    ConversationID uuid.UUID `validate:"required"`
}
```

---
## Key Patterns
### Idempotent Conversation Creation

`CreateConversation` first attempts to look up an existing conversation by the `(customer_id, vendor_id)` pair. If found, it returns the existing record. This is backed by the `conversation_customer_vendor_key` unique index at the database level.

### Participant Authorization

Both `SendMessage` and `ListMessage` verify that the authenticated user is a participant of the target conversation (either `customer_id` or `vendor_id`) before proceeding. This prevents unauthorized access to conversations.

### Concurrent Client Map

The WebSocket client registry uses `sync.RWMutex` for safe concurrent access. Reads (message delivery) use `RLock` to allow parallel sends, while writes (connect/disconnect) use the exclusive `Lock`.