# Common Module

## Overview

The **common** module provides shared infrastructure services consumed by other modules throughout the ShopNexus e-commerce backend. It encompasses three core responsibilities:

1. **Resource Management** -- Tracking file attachments (images, documents) and linking them to domain entities such as products, brands, refunds, and comments via a polymorphic reference table.
2. **Object Storage** -- Abstracting file upload and URL retrieval across multiple storage backends (local filesystem, AWS S3/MinIO with CloudFront, and remote/external URLs).
3. **Service Options Registry** -- A generic registry for configurable service providers used by other modules (payment providers, shipment carriers, object store backends), persisted in the database and auto-synced on startup.

The module follows the standard project layering: database migrations and SQLC-generated queries at the bottom, a business logic (`biz`) layer in the middle, and Echo HTTP transport handlers at the top, all wired together via Uber fx dependency injection.

---

## Database Schema

All tables live under the `common` PostgreSQL schema.

### Enum: `common.resource_ref_type`

Defines the types of domain entities that a resource can be attached to:

```sql
CREATE TYPE "common"."resource_ref_type" AS ENUM (
    'ProductSpu',
    'ProductSku',
    'Brand',
    'Refund',
    'ReturnDispute',
    'Comment'
);
```

### Table: `common.resource`

Stores metadata for every uploaded or referenced file. Each resource maps to exactly one object in a storage backend.

```sql
CREATE TABLE IF NOT EXISTS "common"."resource" (
    "id"          UUID           NOT NULL DEFAULT gen_random_uuid(),
    "uploaded_by" UUID,
    "provider"    TEXT           NOT NULL,   -- e.g. "local", "s3", "remote"
    "object_key"  VARCHAR(2048)  NOT NULL,   -- path/key within the provider
    "mime"        VARCHAR(100)   NOT NULL,   -- MIME type (image/png, etc.)
    "size"        BIGINT         NOT NULL,   -- file size in bytes
    "metadata"    JSONB          NOT NULL,   -- arbitrary extra metadata
    "checksum"    TEXT,                      -- optional integrity hash
    "created_at"  TIMESTAMPTZ(3) NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT "resource_pkey" PRIMARY KEY ("id")
);

-- Ensures no duplicate objects within the same provider
CREATE UNIQUE INDEX IF NOT EXISTS "resource_provider_object_key_key"
    ON "common"."resource" ("provider", "object_key");
```

### Table: `common.resource_reference`

A join/link table that associates resources with domain entities. The `ref_type` + `ref_id` pair identifies the owning entity, while `order` controls display ordering.

```sql
CREATE TABLE IF NOT EXISTS "common"."resource_reference" (
    "id"       BIGSERIAL NOT NULL,
    "rs_id"    UUID      NOT NULL,                          -- FK -> resource.id
    "ref_type" "common"."resource_ref_type" NOT NULL,       -- which entity type
    "ref_id"   UUID      NOT NULL,                          -- entity primary key
    "order"    INTEGER   NOT NULL,                          -- display order
    CONSTRAINT "resource_reference_pkey" PRIMARY KEY ("id")
);

ALTER TABLE "common"."resource_reference"
    ADD CONSTRAINT "resource_reference_rs_id_fkey"
    FOREIGN KEY ("rs_id") REFERENCES "common"."resource" ("id")
    ON DELETE CASCADE ON UPDATE CASCADE;
```

### Table: `common.service_option`

A registry of available service providers. Each row represents one option within a category (e.g. a specific shipment carrier method or a payment gateway method).

```sql
CREATE TABLE IF NOT EXISTS "common"."service_option" (
    "id"          VARCHAR(100) NOT NULL,   -- e.g. "ghn-standard", "vnpay-qr"
    "category"    TEXT         NOT NULL,   -- "objectstore", "payment", or "shipment"
    "name"        TEXT         NOT NULL,   -- human-readable name
    "description" TEXT         NOT NULL,   -- human-readable description
    "provider"    TEXT         NOT NULL,   -- provider identifier
    "method"      TEXT         NOT NULL,   -- method within that provider
    "is_active"   BOOLEAN      NOT NULL DEFAULT true,
    "order"       INTEGER      NOT NULL,   -- display order
    CONSTRAINT "service_option_pkey" PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "service_option_category_provider_idx"
    ON "common"."service_option" ("category", "provider");
```

---
## API Endpoints

The module registers routes under `/api/v1/common`:

| Method | Path | Handler | Description |
|--------|------|---------|-------------|
| `POST` | `/api/v1/common/files` | `UploadFile` | Upload a file via multipart/form-data. Requires authentication. Returns the created resource object with a resolved URL. |
| `GET` | `/api/v1/common/option` | `ListServiceOption` | List active service options filtered by `category` query parameter. Returns an array of `OptionConfig` objects. |

### POST /api/v1/common/files

**Request:** `multipart/form-data`
- `file` (required) -- the file to upload.
- `private` (optional) -- string `"true"` to mark the upload as private.

**Response:** `200 OK` with the `Resource` JSON object.

**Flow:**
1. Extracts the file from the multipart form.
2. Reads authentication claims from the request context.
3. Delegates to `CommonBiz.UploadFile()` which stores the file via the configured object store provider.
4. Returns the full resource details including a resolved public/presigned URL.

### GET /api/v1/common/option

**Request:** Query parameters
- `category` (required) -- one of `"objectstore"`, `"payment"`, `"shipment"`.

**Response:** `200 OK` with an array of `OptionConfig` JSON objects.

---

## Business Logic Layer (`biz`)

### CommonBiz Struct

The central service object. Holds a reference to the database storage layer and a map of initialized object store clients.

```go
type CommonBiz struct {
    storage        CommonStorage
    objectstoreMap map[string]objectstore.Client
}
```

### File Operations

#### SetupObjectStore

Called during initialization (`NewcommonBiz`). Configures three object store backends:

1. **Local** -- Stores files on the local filesystem under `./tmp/uploads`.
2. **S3** -- Connects to AWS S3 (or MinIO) using credentials from the application config, with optional CloudFront CDN URL.
3. **Remote** -- A passthrough client for externally-hosted URLs.

After initializing each client, it registers them as service options in the `common.service_option` table under the `"objectstore"` category via `UpdateServiceOptions`.

#### UploadFile

Validates input parameters, generates a unique object key (`{uuid}_{filename}`), uploads the file to the configured default provider (from `config.Filestore.Type`), inserts a `resource` record, and returns the resource ID with its resolved URL.

#### GetFileURL / MustGetFileURL

Resolves the public URL for a given `(provider, objectKey)` pair. `MustGetFileURL` falls back to a configured 404 placeholder image URL on error instead of returning an error.

### Resource Management

#### UpdateResources

Transactional operation that replaces all resource attachments for a given `(refType, refID)` entity. It:
1. Deletes existing resource references (and optionally the resource records themselves).
2. Verifies the new resource IDs exist.
3. Creates new `resource_reference` rows with correct ordering via batch insert (`CreateCopyDefaultResourceReference`).

#### DeleteResources

Transactional operation that removes resource references for specified entities. Supports a `SkipDeleteResources` list to preserve certain resource records while still removing their references. Optionally deletes the underlying `resource` records as well.

#### GetResources

Retrieves all resources attached to one or more entities of a given type. Returns a map from `refID` to a slice of `Resource` model objects, each with a resolved URL. Uses `ListSortedResources` to fetch results ordered by the reference `order`.

#### GetResourcesByIDs

Fetches resources by their UUID primary keys. Returns a map from resource ID to `Resource` model. Falls back to a placeholder URL for any resource that cannot be resolved.

#### GetResourceURLByID

Looks up a single resource by ID and returns its resolved URL as a nullable string.

### Service Option Management

#### UpdateServiceOptions

Upserts service option configurations into the database. For each `OptionConfig` in the input, it checks if a record with that ID exists. If not, it creates one; if so, it updates the existing record. Runs within a transaction.

The `Category` field is validated against: `"objectstore"`, `"payment"`, `"shipment"`.

#### ListServiceOption

Retrieves active service options by category, used by the HTTP handler for the public-facing option listing endpoint.
## Models and Types

### commonmodel.Resource (in `model/resource.go`)

The domain-level representation of a file resource returned by the API:

```go
type Resource struct {
    ID       uuid.UUID   `json:"id"`
    Url      string      `json:"url"`
    Mime     string      `json:"mime"`
    Size     int64       `json:"size"`
    Checksum null.String `json:"checksum"`
}
```

### commonmodel.ErrResourceNotFound (in `model/error.go`)

A typed error for when a referenced resource does not exist:

```go
var ErrResourceNotFound = sharedmodel.NewError("resource.not_found", "Resource not found")
```

### sharedmodel.OptionConfig (in `shared/model/option.go`)

The domain model for a service option, shared across modules:

```go
type OptionMethod string

type OptionConfig struct {
    ID          string       `json:"id"`          // e.g. "ghn-standard", "vnpay-qr"
    Provider    string       `json:"provider"`    // "ghn", "vnpay", "momo"
    Method      OptionMethod `json:"method"`      // "standard", "express", "qr", "cod"
    Name        string       `json:"name"`        // human-readable name
    Description string       `json:"description"` // human-readable description
}
```

### SQLC-Generated Models (in `db/sqlc/models.go`)

| Type | Description |
|------|-------------|
| `CommonResourceRefType` | Go string type for the `resource_ref_type` enum with `Valid()` and `Scan()` methods. Values: `ProductSpu`, `ProductSku`, `Brand`, `Refund`, `ReturnDispute`, `Comment`. |
| `CommonResource` | Direct mapping of the `common.resource` table row. |
| `CommonResourceReference` | Direct mapping of the `common.resource_reference` table row. |
| `CommonServiceOption` | Direct mapping of the `common.service_option` table row. |

---

## Key Patterns

### Polymorphic Resource References

Instead of adding image columns to every entity table, the module uses a generic `resource_reference` join table with a `ref_type` enum discriminator. This allows any entity (product SPU, SKU, brand, refund, dispute, comment) to have an ordered list of file attachments without schema changes.

### Multi-Provider Object Storage

The `objectstoreMap` abstraction allows the system to simultaneously support multiple storage backends. Each resource record tracks which `provider` it belongs to, so URLs are always resolved against the correct backend. The fallback chain ensures graceful degradation: if a provider is not found, the local provider is used; if URL resolution fails, a configurable placeholder URL is returned.

### Transactional Resource Updates (Delete-and-Reattach)

`UpdateResources` implements a replace-all strategy within a database transaction: it deletes all existing references, then re-creates them in the desired order. This avoids complex diffing logic while maintaining data integrity.
### Service Option Auto-Sync

On module startup, `SetupObjectStore` automatically registers or updates the available object store providers as service options. This pattern is designed to be reused: payment and shipment modules can call `UpdateServiceOptions` with their own configs to keep the registry in sync with the running system.

---