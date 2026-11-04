# ShopNexus Server

The backend of ShopNexus, a C2C marketplace for used goods in Vietnam. It is one Go binary on Postgres, with Restate for the few flows that must survive a crash.

Two product rules shape the code:

1. The platform holds nobody's money. The only deal path is cash on delivery with inspection at the door: the seller ships from their own carrier account and types the waybill into the app, and the buyer pays the shipper after looking at the item. There is no wallet, payment or finance module.
2. The platform judges no case. There is no dispute desk; trust is meant to come from statistics, such as how often a buyer refuses parcels at the door. Those counts are not built yet.

The earlier design, where the buyer paid first and the platform held the money in escrow, lives on the `escrow` branch.

Design decisions are in [ARCHITECTURE.md](ARCHITECTURE.md). The developer docs are in the `docs` repository.

## Why

### Modular monolith

The server is a modular monolith. Each module owns its tables and exposes a service, and other modules call that service and never touch the tables. A module announces a change with an event and does not know who listens. The linter enforces the layer order and these rules, so a module could later move to its own process behind the same contract.

### Durable flows on Restate

Most use cases are plain calls from an HTTP handler. The ones that write to several places in a row run as Restate services, so a crash halfway resumes instead of leaving half a change: sign-in, posting and publishing a listing, processing uploads, shipping and finishing an order, sending a chat message. While Restate is down, reads and every other use case keep working.

### Events through an outbox

A change and its events commit in one transaction to an `events` table. A bus drains the table into Restate, which delivers each event to each subscriber and retries it on its own. Events are thin: a subscriber reads the current state through the owning module's service, so order and duplicates do not matter.

### Spec-first API

A feature starts as its OpenAPI spec in `api/`. Middleware checks every request against it, and the end-to-end tests check every response.

## Modules

| Module | Holds |
|---|---|
| `account` | Profiles, suspension, the address book, blocks |
| `auth` | Phone sign-in through Firebase and per-device sessions |
| `staff` | Moderator and admin accounts for the web console |
| `catalog` | Listings, categories, provinces and communes |
| `deal` | Price negotiation and COD orders |
| `chat` | One conversation per buyer and seller |
| `trust` | Reviews and the seller's public profile |
| `discovery` | Search, the nearby feed, saved listings |
| `media` | Photo uploads and processing |
| `notification` | In-app notices and push |
| `screen` | One read per app screen, composed from the others |

`platform` holds the shared plumbing (config, Postgres, Restate helpers, the event bus, HTTP, the live stream, errors). `provider` wraps outside services: Firebase, FCM and the language model that reads search queries.

## Stack

- Go, with hand-written SQL on pgx and no ORM.
- Postgres 18 as ParadeDB, whose `pg_search` index powers search.
- Restate for durable flows and event delivery.
- RustFS as the S3-compatible photo store. The app uploads straight to it with a presigned URL.
- Firebase for phone verification and push.

## Run

```bash
make run    # start the dev stack and the server; the first run writes .env
make test   # every test against the test stack, then golangci-lint
make down   # stop both stacks
```

Configuration has no defaults: a missing variable stops the process with its name. Staff admins are created from the command line with `go run ./cmd/staff create-admin`; the API cannot create one.
