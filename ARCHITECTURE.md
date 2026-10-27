# server2 architecture

`~/shopnexus/specs/2026-10-19-server2-foundation-design.md` holds scope,
endpoints, auth flows, security rules and the testing matrix, and points here
for code shape, transactions and durability. Where the two disagree, this file
wins. Plan 2 (`~/shopnexus/plans/2026-10-20-server2-plan2-redesign-otp.md`)
built the OTP sign-in and sessions on this design.

The code takes from two branches of the old `server` repository.
`origin/microservice-ready` reads top to bottom with few layers.
`origin/cleancode` has hand-written SQL repositories, idempotent inserts and
Restate workflows split by phase. server2 adds rich entities and
OpenAPI-first contracts.

## Decided

Every section below is decided. New proposals go in a section at the end
until the user approves them.

### Infrastructure

Postgres, Restate (server 1.8.0, Go SDK v1.1.0) and RustFS for photos (see
Object store). Restate replaced the relay, JetStream and NATS. The outbox is
a table, `events`, that a bus drains into Restate (see Events). `/readyz`
checks Postgres and the Restate admin health endpoint.

### Use cases

A use case is a method of the module's service, in its own file:
`func (s *Account) Update(ctx context.Context, id uuid.UUID, in UpdateInput) (AccountView, error)`
in `account/service/account_update.go`. `service.Account` holds the module's
repositories and clock, `service.New` builds it, and it implements
`service.Service`, the module's contract. The module's handlers and every
other module call `Service`. A method reads the clock once, `now := s.now()`.
A repository is an interface with a Postgres implementation; a test wraps it
to inject a fault. Business code takes no functions as arguments: no update
callbacks, no transaction closures.

A flow that runs as a Restate service is written in the service handler
itself (see Durable flows). The `Service` method in front of it calls it
through `restatex.Caller`.

### HTTP handlers

A handler is a method of the module's `handler.Handler` and runs in one
order: `httpx.Caller` on a signed-in route, `httpx.Bind` for the request, one
`Service` method, then `httpx.Respond` or `httpx.WriteError`. Its request
struct is unexported and lives in its file; the response is the view the
service returns.
`httpx.Bind(r, &in)` fills a request struct. A field tagged `path`, `query` or
`header` takes that request value and also carries `json:"-"`; the other
fields come from the JSON body. The spec middleware has already checked every
value and filled in query defaults, so `Bind` converts strings to Go types
(`string`, `int`, `bool` and any `encoding.TextUnmarshaler`) and a handler
repeats no shape rule. `Bind` reads the tags of a type once and keeps the
result in a `sync.Map`.
`httpx.ClientIP(r)` is the caller's address, which the `ResolveClientIP`
middleware works out once per request from `TRUSTED_PROXIES`.

### Pagination

A list endpoint pages by keyset over UUIDv7 IDs, newest first. Its handler
binds `page.Request` (the spec's shared `Cursor` and `Limit` parameters),
alone or embedded in its request struct, and passes it to the service and
the repository. The repository lists rows below `req.Before()`, which is
`uuid.Max()` on the first page, and reads `req.Fetch()` rows, one more than
the page holds. The service returns `page.Of(rows, req, id)`, which drops the extra
row and sets `next_cursor` to the ID of the last item kept.

### Transactions and concurrency

A transaction lives inside a repository method and covers one aggregate.
Concurrent writes are caught optimistically: `Save` fails with `ErrConflict`
when the aggregate changed since it was read, and `write_id` (below) is the
column that detects it. A use case that loses the race answers
`apperr.ErrTryAgain` (409 `try_again`) in every module. A conflict reply to an
OTP guess must not reveal whether the guess was right.

### Aggregates in auth and account

- `Account` (module account): display name, bio, avatar, status,
  suspension.
- `Address` (module account): one address of an account's address book.
- `Credentials` (module auth): every identity of one account, later its
  password. It holds the rule that an account keeps at least one identity.
- `Session` (module auth): one signed-in device. It is separate because sessions
  are unbounded and every refresh writes one.
- `Challenge` (module auth): one OTP code.

`UNIQUE (kind, value)` on identities spans accounts, so the database
constraint holds it.

### Durable flows

Only flows that need durability run as Restate services. Other use cases are
called directly from HTTP handlers with `context.Context`, so reads and
refresh keep working when Restate is down.

- The client's `Idempotency-Key` is the idempotency key of the invocation. A
  repeated key returns the first result and runs no step again.
- OTP send is service `OTP`. It admits the send against the quotas, stores
  the challenge and sends the SMS or email.
- A `restate.Run` step only reads or writes a store, so any error it returns
  is a fault that Restate retries. Every business decision is taken in the
  handler between steps, on values a step returned and the journal kept.
  Code outside a step uses only those values: a step that saves an entity
  saves a copy, so a field `Save` sets on the first run, such as the write
  ID, never differs from a replay. A replay takes the same decisions, and a
  retried `Save` lands the same row because the row already carries its
  write ID. Entities that a step returns have `MarshalJSON` for the journal.
- A business refusal leaves the service as data, `Refused *apperr.Error` in
  the result. Restate retries an error, and its ingress keeps only the code
  and message of a terminal one, which would lose `code` and `details`. The
  result type has `Refusal()`, and `restatex.Caller` returns that refusal as
  the error, so the handler gets the same `*apperr.Error` a plain method
  returns. Each service names its own result type, because Restate derives
  a JSON schema ID from the type's name and refuses a generic one.
- A step that can lose a race or find an identity taken returns that as
  data: `restatex.Outcome(err, repository.ErrConflict)` gives `(true, nil)` for the
  conflict, and the service decides between steps. `restatex.Ignore` drops an
  outcome that changes nothing, such as a lost race on `last_used_at`.
- A service takes `Now func() time.Time`, read once per invocation through a
  journaled step (`restatex.Now`), so tests move time by hand.
- Sign-up touches two aggregates, `Account` and then `Credentials`, so
  service `SignIn` runs it as a saga. When `Credentials.Create` returns
  `ErrTaken`, because another sign-up took the identity between the lookup
  and the insert, a step deletes the account and the client gets
  `try_again`. Restate retries every other failure, so no step ends the
  saga with a terminal error.
- Every step inside `restate.Run` is idempotent. IDs are minted before the
  step by `restatex.NewID(ctx, at)`, a UUIDv7 built from the journaled clock
  and the invocation's deterministic random generator (`restate.UUID` gives
  version 4, which would break the spec's time-ordered cursors). Inserts use
  `ON CONFLICT (id) DO NOTHING`. The same ID is the idempotency key sent to a
  payment provider.
- The two Restate services live in `auth/service`: `OTP` in
  `otp_send_durable.go` and `SignIn` in `sign_in_durable.go`. They are the
  only code that passes closures, to `restate.Run`. `Auth.SendOTP` and
  `Auth.SignIn` call them through `restatex.Caller`. `Auth.Durable` returns
  their definitions for `auth.Mount`.

### Events

A module announces a change with an event, and modules that react
subscribe to its kind; the module that changed data never lists who
reacts. Every subscriber is a module of server2, so there is no broker.

- `events.Event` is thin: a `Kind` constant in the form
  `aggregate.past_tense` (`account.updated`, `account.suspended`,
  `account.unsuspended`), the `SubjectID` of the aggregate, and a `Payload`
  map that only a business event fills. A subscriber reads the subject's
  current state through its module's `Service`, so order, delay and
  duplicates do not change the result.
- A repository writes the events of a change with `events.Insert` in the
  change's transaction, as it writes an audit entry: `Save(ctx, a, writeID,
  evs...)`. A conflicting write leaves no event. An event with an ID that
  exists already is not written again, so a durable step that retries its
  save with the event IDs from its journal writes one event. `Insert` also
  runs `pg_notify('events', '')`, which Postgres delivers on commit.
- A module that reacts to events has a `worker` package next to
  `handler`: a Restate service with one handler per event kind, of the
  signature `func(restate.Context, events.Event) error`, that calls the
  module's `Service`. The module's `Mount` adds the worker to its Restate
  services and calls `d.Events.Subscribe(kind, service, handler)`.
  `Subscribe` after the bus has started panics, and `app.New` refuses to
  start when a subscription names a service or handler this process does
  not serve.
- `events.Bus.Run`, started by `app.New` and stopped by `App.Close`,
  repeats a round: in one transaction it takes up to 100 pending rows
  `FOR UPDATE SKIP LOCKED`, sends each once to the bus's own Restate
  service, `Events.Deliver`, with the event ID as idempotency key, and sets
  `dispatched_at` on what Restate took. An event whose kind has no
  subscriber is marked without a send. Each send gives up after 5 seconds.
  When the ingress refuses an event (4xx), the bus logs it at ERROR and the
  event waits 1 s, 2 s, 4 s and so on, up to an hour (`attempts`,
  `next_attempt_at`); any other failure ends the round, and the next round
  sends the rest again. Restate drops a repeated key for 24 hours. Once an
  hour, and at start, the bus deletes rows dispatched more than 7 days
  ago, 10,000 at a time.
- `Events.Deliver` sends the event to every subscriber with
  `restate.ServiceSend`. Each send is journaled, so a retried `Deliver`
  sends nothing twice. A send to a service Restate does not know yet, as
  during a rolling deploy, fails `Deliver` with RT0018, and Restate retries
  it with growing backoff until the service is registered.
- The bus listens on channel `events` on a connection of its own, outside
  the pool, and also runs a round every second. A pooled connection would
  hold a slot for good, and `Pool.Close` would wait for it.
- Each event to each subscriber is its own Restate invocation, which
  Restate retries alone until the handler succeeds; one failing subscriber
  blocks no other subscriber and no other event. While Restate is down, events wait in the table and the API keeps
  working.
- Nothing replays history. A subscriber that needs its data rebuilt reads
  the current tables with a job of its own.

### Module layout

```
internal/account/
  domain/            entity and its rules, business errors
    account.go
  repository/        one interface per aggregate, its Postgres version and tests
    account.go         interface Accounts, ErrNotFound, ErrConflict
    account_pg.go      hand-written SQL, one transaction per method
    account_pg_test.go
  service/           the module's contract and use cases
    service.go         interface Service, type Account, New
    views.go           response views and inputs
    account_update.go  one use case per file, a method of Account
  handler/           HTTP: request structs, one method per endpoint
    handler.go         type Handler, Register
    account_update.go
  worker/            Restate handlers for the events the module reacts to
                     (account has none yet)
  account.go         Mount: builds the layers, returns service.Service
```

Modules auth, staff, media, catalog, deal, chat, trust and discovery have
the same four packages; a module that reacts to events adds `worker`, and
deal is the first that does. Module `screen` has no `repository` or
`domain`: it only reads the other modules' services (see Screen).

The modules follow the product model in `app2/mohinh.md`, not the old
server's split (decided 2026-10-26). There is no finance module: ShopNexus
holds no money, so there is no wallet and no payment session. Planned
next: `notification` (the in-app list and push, subscribing to the events
of deal, chat and catalog) and `billing` (boosts and seller plans). There
is no `kyc` module: posting needs a verified phone alone, since a carrier
checks a seller's identity when it opens their account (decided
2026-10-27). Reports, incident
records and reputation will join `trust`; search, the feed, followed
searches and price alerts will join `discovery`. Imports run one way, `handler` and `worker` →
`service` → `repository` → `domain`, and depguard rules in `.golangci.yml`
refuse an import against it.

An entity keeps its fields unexported, so a repository in another package
rebuilds one through `Restore…` (`RestoreAccount`, `RestoreChallenge`; a
member through `domain.MemberState`) and records a write with
`Saved(writeID)`; only repositories call either.

One binary serves the HTTP API on `:8080` and the Restate endpoint on `:9080`.
A module's `Mount` takes `module.Deps` (router, pool, Restate ingress and
service prefix, clock, logger), the module's own settings and the services
of the modules it calls. It builds the repositories, the service and the
handler, registers the routes, and returns what app and other modules need:
`account.Mount` returns `accountsvc.Service`, `auth.Mount` returns its
`Service` and its Restate services. `app.mountModules` calls each `Mount` in
dependency order and imports no package inside a module. `auth.Protect`, the
sign-in middleware, needs only the auth keys, so app builds it first and
hands it to every module with signed-in routes. `staff.NewService` builds the
staff service on a pool; `staff.Mount`, `cmd/staff` and the e2e tests share
it, and `cmd/staff` passes no account or auth service.

To run a module in its own process, a client implements that module's
`Service` over the network and app passes it where it passes the module's
`Mount` result. The data split, its tables out of the shared database, is
separate work.

### Tables and aggregates

| Aggregate | Tables | Notes |
|---|---|---|
| `Account` | `accounts` | adds `write_id` |
| `Credentials` | `credentials` (root: `account_id` PK, `write_id`, later `password_hash`), `identities` (children) | `Save` changes the root's `write_id` and rewrites the account's identities in one transaction; the planned `passwords` table folds into the root |
| `Session` | `sessions` | adds `write_id` and `auth_time` |
| `Challenge` | `otp_challenges` | adds `write_id` |
| `Upload` | `uploads` | module media; status, content type, size, the processed keys |
| `Listing` | `listings` | module catalog; photos, marks and specs are `jsonb` columns, read and written with the row |
| `Address` | `addresses` | module account; one row per address, `write_id`; `recipient_name` keeps the keyword `name` out of the columns |

No column is named after an SQL keyword (sqlfluff RF04, run with
`sqlfluff lint migrations/`): `events.occurred_at` and `payload`,
`staff_actions.action_kind` and `acted_at`, `identities.identifier`,
`otp_quotas.subject`, `staff_login_locks.email_hash`,
`staff_members.staff_role`, and `display_name` for categories, provinces and
communes. Go fields and JSON keep their own names where an API already
published them (`role`, `value`, `action`, `at`). `.sqlfluffignore` skips
the generated `0005_regions.sql`, which is past the parser's node limit.

IDs are chosen by the caller before the insert: `restatex.NewID(ctx, at)`
inside a Restate service, `uuid.NewV7()` in a direct use case. Only
`accounts.id` keeps `uuidv7()` as a column default. `Create` inserts with
`ON CONFLICT (id) DO NOTHING`.

`write_id` is the ID of the last write to an aggregate and replaces a version
counter. `Get` returns it as T. `Save` picks a new ID N (from
`restatex.NewID(ctx, at)` in a Restate service, so a retry sends the same N)
and runs:

```sql
UPDATE ... SET ..., write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
```

Every SQL statement passes parameters as `pgx.StrictNamedArgs` with `@name`
placeholders, in stores, migrations and tests alike.
Positional `$1` placeholders are not used. The strict variant fails the query
when a name in the SQL has no value or a value has no name; plain
`pgx.NamedArgs` binds a misspelled name as NULL, and a NULL `write_id` turns
every save into `ErrConflict`. In SQL, `@read_write_id` is T and `@write_id`
is N.

A normal write finds T. A retry after a crash-after-commit finds N and writes
the same values again. A write by anyone else leaves M, so the stale write
fails with `ErrConflict`. A counter cannot do this: another writer can raise
it to the same value.

Accepted risk: when a step commits, the process dies before Restate journals
the result, and another writer commits before the retry, the retry gets
`ErrConflict` although its change landed. The service handles it like any
conflict: read again and decide.

`write_id` differs from the client's `Idempotency-Key`, which names a whole
invocation so Restate runs it once.

### OTP send

Service `OTP`, handler `Send`, run once per `Idempotency-Key`:

1. Admit the request against two `Quota` aggregates, `ip:<address>` first
   and then `dest:<destination>`, in table `otp_quotas(subject PK, write_id,
   sent_at timestamptz[])`. An IPv6 address is keyed by its /64. The address
   goes first so an address over its limit cannot spend the quota of the
   numbers it names. `Quota.Admit(now, rule)` holds the limits: one code per
   destination per 45 s, five per destination per hour, twenty per address per
   hour. Each quota is read in one step, judged by `Admit` between steps and
   saved in the next step. Parallel requests conflict on `write_id` and read
   again; after three lost races the request gets 429. Every 429 carries
   `retry_after` in seconds: `otp_too_soon` for the 45 s rule, `otp_limit`
   for an hourly rule or three lost races. A quota that admitted a request
   stays spent even when a later step fails.
2. `restate.Run`: create the `Challenge`.
3. `restate.Run`: send the SMS or email through `sms.Sender` or `email.Sender`.
4. Return `challenge_id`; the HTTP handler waits for it.

### Idempotency-Key

Every endpoint that invokes a Restate service requires an `Idempotency-Key`
header (a UUID) and passes it as the invocation's idempotency key. A repeated
key attaches to the first run and returns its result, so a retried tap gets
the same answer. `PATCH /v1/accounts/me` with an avatar is the exception: it
calls `MediaProcess` keyed by the upload ID, which already makes a repeat
attach to the first run.

### OTP verify and sign-up

Service `SignIn`, handler `Run`, returns the challenge ID, the account ID,
`is_new` and a session ID:

1. A step loads the challenge and whether it is the newest for its
   destination. Between steps: an unknown, replaced or non-login challenge
   is `code_expired`, and when `Open` refuses it (used, expired or locked)
   the run ends with that refusal and saves nothing. Otherwise `Verify`
   counts the guess, the next step saves it, and a wrong code ends the run
   with `invalid_code`. A save that loses a race gets `try_again` for a
   right and a wrong code alike.
2. A step finds the identity in `Credentials`.
   - Found: a step reads the account status (suspended ends the run), and
     the next step marks the identity used, ignoring a lost race.
   - Not found: create the `Account` with
     `accountID := restatex.NewID(ctx, at)`, then the `Credentials` with the
     identity. `ErrTaken` from the second step deletes the account (see
     Durable flows).

Tokens never enter Restate: its journal and invocation results are stored and
readable through the Restate admin API. The HTTP handler issues the session
after the service returns, with the session ID the service chose. When the
same `Idempotency-Key` comes back, that session already exists, so the handler
rotates its refresh token and returns new tokens for the same session.

### Session use cases

Refresh, logout and the device list are plain `Auth` methods against the
`Session` aggregate; none of them goes through Restate.

Refresh:

1. Find the session by the hash of the presented token. When none has it as
   its current hash but one has it as the previous hash, the token was
   replayed: `Revoke`, `Save`, return `session_revoked`. When nothing matches,
   return `invalid_refresh_token`.
2. `s.Usable(now)`, then the account status through the account port. A
   suspended account gets its session revoked and `account_suspended`.
3. `token := s.Rotate(now)`, `Save`. On `ErrConflict` the use case reloads
   the session. Revoked means a sign-out won the race, and the answer is
   `session_revoked`. Otherwise a concurrent refresh with the same token won:
   the use case revokes, logs the reuse and returns `session_revoked`.
4. Sign the access token with the session's `auth_time`.

Logout and removing one device load the session, check that the caller owns
it (another account's session is 404), `Revoke` and `Save`, retrying once on
`ErrConflict`.

Two kinds of call skip the aggregate:

- A read returns view structs from a query and never loads an entity. The
  device list is `sessions.ListActive(accountID, now, cursor, limit)`.
- A bulk change that only moves a one-way flag may run as one set-based
  statement that also changes each row's `write_id`. `logout-all` is
  `UPDATE sessions SET revoked_at = @now, write_id = uuidv7() WHERE
  account_id = @account_id AND revoked_at IS NULL`. Loading and saving each session
  would cost one transaction per device and protect no rule.

The access token is checked by signature and expiry only; the middleware reads
no database.

### Staff and authorization

Moderators and admins run the web console on staff accounts of their own
(module `staff`), separate from customer accounts.
`~/shopnexus/specs/2026-10-24-server2-staff-authorization-design.md` holds
the decisions and the API.

- Every sign-in call is `POST /v1/staff/login` with the email and the
  password. The server reads the member and answers 428 with what the call
  lacks: `password_change_required` (send `new_password`),
  `totp_enrollment_required` (call `/v1/staff/login/totp-enrollment`, which
  takes the email and password too, then send the first code) or
  `totp_required` (send `code`). An unknown email, a wrong password and a
  disabled member all get `invalid_credentials`.
- Each call is charged to the email's lock in `staff_login_locks` before
  anything is checked, in one statement, so concurrent guesses cannot pass
  the check together. A wrong password or code keeps the charge; any other
  answer refunds it. Five kept charges lock the email for 15 minutes,
  whether or not the email is a member's. The lock is keyed by an HMAC of
  the email, and a finished sign-in clears the count.
- A finished sign-in sets the `staff_session` cookie: a random token,
  `HttpOnly; Secure; SameSite=Strict`, path `/v1/staff`, 8 hours, never
  extended. `staff_sessions` holds its SHA-256. There is no access token and
  no refresh, so the console's tabs share one cookie.
- Every staff route requires `X-Requested-With: shopnexus-console`, which a
  form or link on another site cannot set. The spec declares it and
  `handler.FromConsole` checks it again. With SameSite=Strict, the console
  and the API must be on the same site (`console.x.vn` and `api.x.vn`).
- `handler.Require(..., floor)` calls `Staff.Authenticate`, which reads the
  session joined with its member on every request. A session lives while
  it is unrevoked, younger than 8 hours, its member is active, and its
  credentials version equals the member's. A password change, a password
  or TOTP reset and a disable raise the version, which ends every session
  of the member, the caller's included; enabling a member brings none back.
- Admins exist only through `cmd/staff`. The API manages moderators, and
  `Member.ManagedBy` refuses an admin target to anyone but the CLI.
  `cmd/staff unlock` clears the sign-in lock on an admin's email, which
  anyone can set by sending five wrong passwords.
- Every staff write on an account or on another member is a row in
  `staff_actions`, written by `audit.Insert` inside the repository
  transaction of the change (`SaveAudited`, `CreateAudited`, `Unlock`). Its
  action is an `audit.ActionKind` constant (`audit.AccountSuspend`,
  `audit.StaffDisable`, ...), the list the `Action` schema's enum names; a
  new action adds both. `details` holds what the action does not say, such
  as a suspension's reason and end.
  Refused sign-ins go to the log as `security: staff sign-in refused` with
  the email and IP; finished ones are rows in `staff_sessions`.
- TOTP secrets are sealed with AES-GCM under a key HKDF derives from
  `STAFF_KEY`, bound to the member's id. Passwords are argon2id
  (64 MiB, 3 passes, 4 lanes) in PHC form, at most four runs at once.
- Staff reaches customer data through `accountsvc.Service` and
  `authsvc.Service`. CORS allows
  `STAFF_CONSOLE_ORIGIN` on `/v1/staff/` and nothing else.

### Object store

RustFS 1.0.4 through its S3 API, reached with minio-go
(`internal/platform/objectstore`). One bucket, `OBJECT_STORE_BUCKET`:
`uploads/<id>` is private and written only through presigned PUTs;
`media/` is public-read and holds processed photos. `Store.Prepare` creates
the bucket and the `media/*` read policy; `make run` calls it through
`cmd/objectstore-init`, and `objectstoretest.New` calls it once per test
process. The server reaches the store at `OBJECT_STORE_ENDPOINT` and signs
presigned URLs for `OBJECT_STORE_PUBLIC_ENDPOINT`, the address phones reach;
the signature covers the host, so a proxy cannot rewrite it. Configuration:
`OBJECT_STORE_ENDPOINT`, `OBJECT_STORE_PUBLIC_ENDPOINT`, `OBJECT_STORE_REGION`,
`OBJECT_STORE_ACCESS_KEY`, `OBJECT_STORE_SECRET_KEY`, `OBJECT_STORE_BUCKET`
and `MEDIA_PUBLIC_URL`, the base a public URL is built on. RustFS listens on
127.0.0.1:56490 in development and 56491 in tests; the test container keeps
`/data` on a tmpfs owned by uid 10001, the user the image runs as.

### Media

Module `media` owns uploads; catalog, chat and account (avatars) use
them. An `Upload` moves waiting → attached → ready or failed, or
waiting → expired.

- `CreateUpload` (`POST /v1/uploads`) takes a content type (JPEG, PNG or
  WebP; the app converts HEIC) and a size up to 15 MB, and returns a PUT URL
  valid for 15 minutes that signs both. Before inserting the row it sends
  `MediaExpire.Run(upload_id)` with a 24-hour delay, keyed by the upload ID.
  `MediaExpire` deletes an upload still waiting, and its object.
- `Attach(owner, holder, ids)` gives the uploads to a holder, such as a
  listing, all or none: each must be the owner's and either held by that
  holder already or waiting and present in the store with the declared type
  and size. Otherwise `invalid_upload`. An upload the holder already holds
  passes, so a retried attach succeeds; `uploads.holder_id` records it.
- `MediaProcess.Run(upload_id)` processes an attached upload in one step:
  it checks the magic bytes against the type, reads the dimensions from the
  header and refuses more than 40 megapixels, or more than 160 MB of decoded
  pixels (20 megapixels at 16 bits a channel), before decoding. A box filter
  then bilinear scaling make a JPEG at most 1600 px on its long side, turned
  upright by the JPEG's EXIF Orientation, and the 400 px thumbnail is made
  from that photo; neither keeps more than its output in memory. Both go
  under `media/`. Re-encoding drops EXIF, GPS included. Two photos decode
  at once in the process (`processSlots`); the others wait for a slot. The photo's bytes never
  enter the journal; the step returns the keys. A later step saves the
  upload ready or failed with the reason and deletes the original. A second
  run returns the first run's result.
- An upload has a `shape`, recorded when it is attached: `photo` for the
  above, `square` for an avatar (decided 2026-10-27). A square is the
  middle of the upright image, at most 400 px a side and never scaled up,
  with no thumbnail. `Service.Square` attaches the upload and waits on
  `MediaProcess`, keyed by the upload ID, so the caller answers once the
  square exists; a refused photo is `invalid_upload`. A replaced avatar's
  objects stay in the store.

### Catalog

Module `catalog` holds listings, categories and regions. A listing is one
item at one price, C2C, cash on delivery. Drafts stay on the device; the
server receives a complete listing.

- A price is whole đồng, 10,000 to 1,000,000,000. The ceiling (decided
  2026-10-26) refuses a price typed with extra zeros and keeps a deal's
  amount arithmetic far from int64 overflow; migration 0009 adds it to the
  table's check.
- Status: processing → active or rejected; active ⇄ hidden; active or
  hidden → sold → active; any status → deleted; active ⇄ reserved →
  sold. Active and reserved are public. An edit that adds an upload, or an
  edit of a rejected listing, returns the listing to processing; an edit
  of a sold, deleted or reserved listing is `listing_state_conflict`.
- A listing is reserved while an order's parcel travels, and
  `reserved_for` names the order (migration 0010). `Service.Reserve`,
  `Release` and `SellTo` take the listing and the order, so module deal's
  flows can repeat them: reserving again for the same order, releasing for
  another order and selling an already sold listing change nothing. They
  write `listing.reserved`, `listing.released` and `listing.sold`. A
  reserved listing counts toward the cap and shows in the `on_sale` pane;
  the seller cannot hide, edit, mark sold or delete it.
- Posting and editing run as Restate services, `ListingPost` and
  `ListingEdit`, through `restatex.Caller`. `POST /v1/listings` and
  `PUT /v1/listings/{id}` take an `Idempotency-Key`, and the invocation's
  key is `<seller id>/<Idempotency-Key>`: Restate scopes a key to the
  service, so the seller in it keeps two sellers' keys apart.
- `ListingPost` checks, in order: account suspended, no verified phone (an
  identity of kind phone from `authsvc.Service.Identities`), 20 listings in
  processing, active, hidden or rejected, then the fields. The category and
  commune lookups reach the domain as `domain.Refs`, so one
  `invalid_listing` lists every field. It inserts the row under
  `pg_advisory_xact_lock(hashtext(seller_id))`, which holds the cap when
  posts race, then attaches the uploads, marks' photos included, to the
  listing. A cap reached at the insert refuses before any upload is taken;
  an attach that refuses deletes the row again (`Listings.Discard`). The
  last step sends `ListingPublish`, journaled like the rest.
- `ListingEdit` loads the seller's listing, edits it, attaches the uploads
  the edit adds and saves it with `listing.updated`. A save that loses a
  race is `try_again`; the uploads stay with the listing, so the next try
  attaches them again.
- Unmarking a sale counts toward the cap: `Listings.Save` takes the seller's
  lock and refuses a listing coming back on sale with `listing_cap_reached`.
- `ListingPublish.Run(listing_id)` starts after every write that leaves the
  listing in processing, with idempotency key `<listing id>/<write id>`. It
  loads the listing, calls `MediaProcess` for each unprocessed upload with
  the upload ID as idempotency key, so two runs share one processing, and
  waits for all. All ready: active, the photo keys recorded and
  `listing.published` written. One failed: rejected, the reason kept on the
  photo. The save ignores `ErrConflict`: a listing changed meanwhile has a
  run of its own.
- The listing stores object keys; views build URLs from `MEDIA_PUBLIC_URL`
  at read time.
- Another seller's listing is `listing_not_found`. `GET /v1/listings/{id}`
  sits behind `auth.Identify`, which lets a request without an
  `Authorization` header through signed out and holds one with it to
  `auth.Protect`'s rules; the seller sees every status but deleted, anyone
  else only active. Post and edit refuse a suspended account; hide, unhide,
  mark sold, unmark sold and delete do not.
- `PUT /v1/listings/{id}` replaces the whole listing with the body. The
  catalog spec named `PATCH`; the user chose `PUT` on 2026-10-26, since the
  app sends the whole form.
- Each change writes its event in its transaction: `listing.published`,
  `listing.updated`, `listing.hidden`, `listing.unhidden`, `listing.sold`,
  `listing.unsold`, `listing.deleted`; search and recommendations will
  subscribe to them.
- `categories(code, parent_code, display_name, sort_order)` is loaded by
  migration 0007: 9 roots, each with subcategories coded `<root>.<slug>` and
  ending with `<root>.other` ("Khác"). A listing's `category_code` must name
  a subcategory, a row with a parent.
- `provinces` and `communes` (migration 0005) hold the 34 provinces and
  3,321 communes after the 2025 merger, from
  thanglequoc/vietnamese-provinces-database v5.2.0 (commit 8b78ba511871,
  MIT), display names as its `full_name`. A commune's `lat` and `lng` are
  `ST_PointOnSurface` of its boundary rounded to 4 decimals, computed once
  in a throwaway PostGIS container. The listing stores the commune; the
  seller's position is never stored.
- `GET /v1/sellers/{id}/listings` is public: the seller's active and
  reserved listings, newest first. `SellerCounts` gives screen and trust a
  seller's listings on sale (active or reserved) and sold; "Đã bán" counts
  every sold listing, sold through an order or marked by hand (decided
  2026-10-27).

### Deal

Module `deal` holds deals: a buyer and a seller agreeing a price on one
listing. Spec: `~/shopnexus/specs/2026-10-26-server2-deal-design.md`.

- One deal per buyer and listing, unique on `(listing_id, buyer_id)`.
  `POST /v1/deals` returns the caller's deal on the listing with 200, or
  starts one with 201. Starting needs an account that is not suspended and
  has a verified phone, on someone else's active listing.
- A deal is `negotiating` or `agreed`. While negotiating it holds at most
  one open price, the buyer's offer or the seller's counter. Several buyers
  may agree a price on one listing; agreeing reserves nothing, and an open
  price has no expiry (decided 2026-10-26).
- Moves: the buyer offers with nothing open or under the seller's counter;
  the seller counters the buyer's open offer; the side facing an open price
  accepts or declines it; its maker withdraws it; the buyer accepts the
  listed price at any time while negotiating; either side cancels an
  agreement, and the deal negotiates again (decided 2026-10-26).
- Amounts are whole đồng in multiples of 1,000. An offer is at least half
  the listed price rounded up to 1,000 and below the counter or the listed
  price; a counter is above the offer and below the listed price. The view
  carries `offer_range`, the amounts the viewer's next offer may take, so
  the rule lives only in the server. `accept` carries the price the person
  saw; another price is `price_changed`.
- `offer` and `accept` need an active listing and refuse a suspended
  account. `decline`, `withdraw` and `cancel` take a price back and work in
  any state of the listing and for a suspended account.
- A save that loses a race is `deal_changed`, where other modules answer
  `try_again`: after the other side moved, the same move may mean something
  else, so the app reads the deal again instead of retrying.
- Each save writes the new moves to `deal_moves` and a `deal.moved` event in
  its transaction. The view lists every move, oldest first, for the thread.
- `worker.ListingWatch` subscribes to `listing.updated`, `listing.hidden`,
  `listing.sold` and `listing.deleted` and calls `ListingChanged`, which
  reads the listing through `catalogsvc.Service.Summaries` (any status) and
  each deal on it. A hidden, sold or deleted listing drops the open price;
  a sold or deleted one also ends an agreement. A new listed price, or
  offers turned off, drops the open price. Each change is a system move
  (`listing_closed`, `price_changed`, `offers_closed`). A listing in
  processing or rejected after a photo edit is not closed. The deal stores
  the listed price at its open price's time to see a price change, so a
  repeated or late event changes nothing.
- Chat, a later module, is one conversation per buyer and seller holding
  the deals of that pair (decided 2026-10-26).

Orders, cash on delivery, live in module `deal` (spec:
`~/shopnexus/specs/2026-10-26-server2-order-design.md`).

- No carrier is integrated (decided 2026-10-26). The seller ships on the
  carrier's own app and records the waybill; the waybill keeps `source`
  (`self`) for an integration later. The COD setup module, with carrier
  accounts and pickup addresses, was dropped with the integration, and
  ShopNexus stores nothing about the seller's payout account.
- An order is `requested` → `shipped` → `delivered` or `returned`, or
  `requested` → `cancelled`. Nothing sits between requested and shipped,
  so a buyer cannot cancel on a seller who already packed the parcel
  (decided 2026-10-26). Either side cancels a requested order with the
  deal's `cancel`, which then needs no Restate. A delivered order makes the
  deal `sold`, its final status; a return or a cancel makes it negotiate
  again.
- The `Deal` aggregate holds its latest order; `orders` keeps the earlier
  ones (migration 0011, with an index on `(deal_id, id)` for the latest
  order every deal read joins). `Deals.Save` writes deal, order, moves and
  `deal.moved` in one transaction. Each order step is a move:
  `cod_requested`, `shipped`, `delivered`, `returned`, `order_cancelled`.
  The order's code `SN-100001` comes from the sequence `order_codes`, so a
  use case reads the deal back after its save.
- A buyer has at most 3 orders requested or shipped; `Save` counts them
  under `pg_advisory_xact_lock` on the buyer and refuses the fourth with
  `cod_limit_reached`.
- A suspended account cannot ask for COD; ship, delivered, returned and
  cancel finish or take back an order already under way, and refuse no
  one for a suspension.
- Ship, delivered and returned change the listing, so they run as Restate
  flows, `DealShip` and `DealFinish` (one service for both endings, with
  the outcome in its input), with the invocation key `<caller
  id>/<Idempotency-Key>`. `DealShip` reserves the listing first, so a
  listing no longer on sale refuses before anything is written, then saves
  the order, and releases the listing when that save loses a race.
  `DealFinish` saves, then sells or releases the listing.
- A flow journals the deal it loaded and replays the domain move, which
  mints new move IDs. `Save` therefore treats a row that already has the
  write ID as an earlier try that committed and writes nothing more, where
  catalog's `Save` accepts either write ID.
- `DealShip` keeps the listing when its save loses to another try that
  shipped the same order (a second tap with a key of its own), and
  releases it otherwise. Once shipped, it calls `ListingChanged`, which
  cancels the listing's other requested orders with `sent_to_another`.
  `ListingChanged` reads a listing reserved for an order that is not
  shipped yet as paused, so a ship that loses to a cancel ends no other
  buyer's request. The deal worker cancels requested orders whose listing
  is hidden, sold off the app or deleted (`listing_closed`).
- The buyer always reads the recipient in full; the seller reads it in
  full except on a cancelled order, which keeps only the name and the
  commune.
- `POST /v1/deals/{id}/cod` takes an `address_id` from the buyer's address
  book, never a free-form recipient, so the app sends only addresses the
  book checked (decided 2026-10-27). The order copies the address, so
  editing or deleting it later leaves the order as it was. Someone else's
  address is `address_not_found`. The request reads the deal (party,
  suspension), then the address, then applies the move's rules, so a
  missing address answers before `move_not_allowed`.

### Stream

`GET /v1/stream` tells an open app what changed for the caller, as
server-sent events (decided 2026-10-26).

- An event carries IDs only: `message {conversation_id, message_id}`,
  `read {conversation_id}`, `deal {deal_id}`, after `ready` on connect. The
  app reloads what it shows, and reloads everything after a reconnect, so
  nothing is stored for replay and the stream needs no `Last-Event-ID`.
- A repository calls `stream.Notify(ctx, tx, accounts, hint)` in the
  transaction of the change. It runs `pg_notify('stream', …)`, which
  Postgres delivers on commit and drops on rollback, so every instance
  hears every hint. The deal repository notifies the buyer and the seller
  on each save; the chat repository notifies the members a message is
  visible to, and the other member on a read.
- `stream.Hub` listens on channel `stream` on a connection of its own, as
  the events bus does, and hands each hint to the open streams of its
  accounts, 16 waiting at most per stream; more are dropped. App runs it
  beside the bus and stops both on close.
- The handler clears its request's write deadline through
  `http.ResponseController`, since the server's `WriteTimeout` of 30
  seconds would end the stream, and writes a comment every 25 seconds.
- `http.Server.Shutdown` waits for every request, and a stream never ends
  on its own, so the server registers `App.CloseStreams` with
  `RegisterOnShutdown`. `Hub.Close` closes every subscriber's channel, the
  handlers return, and the apps reconnect to another instance and reload.

### Blocks

Blocks live in module `account`, since both `chat` and `deal` check them. A block stands either way: neither person
may message the other, open a conversation with them or open a deal on
their listings (403 `blocked`). The blocker's inbox leaves the conversation
out. Running deals and orders stay as they are. Each block has a UUIDv7
`id`, so `GET /v1/blocks` pages like every list.

### Account profile and addresses

Decided 2026-10-27 (spec:
`~/shopnexus/specs/2026-10-27-server2-app-alignment-design.md`).

- `PATCH /v1/accounts/me` takes any of `display_name`, `bio` (0 to 200
  characters, public, no content filter) and `avatar_upload_id` (`null`
  removes the avatar). It writes `account.updated`. A text rule broken is
  `invalid_account` with a field per rule.
- The address book holds up to 10 addresses per account (name, a
  Vietnamese mobile, street, commune), one of them the default: the first
  address becomes it, and deleting it makes the newest remaining one the
  default. A delete removes the row. Only the owner reads or changes an
  address; anyone else gets `address_not_found`.
- `POST /v1/addresses` requires an `Idempotency-Key` without running
  Restate (decided 2026-10-27): the row keeps it as `request_key`, unique
  per account, and a repeated key returns the address its first request
  added, at the cap too. A retry after that address was deleted adds it
  again.
- Insert, set-default and delete run under `pg_advisory_xact_lock` on the
  account, which keeps the cap and the single default; a partial unique
  index on `(account_id) WHERE is_default` backs the second. An edit checks
  `write_id` and is `try_again` after a race.
- The account repository checks the commune and reads its names from
  `communes` and `provinces` in SQL: catalog imports account, so account
  cannot call catalog.

### Chat

Module `chat` holds conversations (spec:
`~/shopnexus/specs/2026-10-26-server2-chat-design.md`).

- One conversation per pair of people, whatever their roles, stored as
  `(account_a, account_b)` with `account_a < account_b`. Screen's chat
  view shows every deal between the two, both ways, through
  `dealsvc.Service.Between`; chat itself does not call deal (decided
  2026-10-27).
- A buyer opens it from someone else's active listing, with a verified
  phone and no block, and the first message names the listing. A pair that
  has a conversation reuses it.
- A message has text of 1 to 1,000 characters, up to 4 photos, or both,
  and may name a listing of either member. Messages cannot be edited or
  deleted.
- Every send runs as the Restate flow `MessageSend` with the key `<sender
  id>/<Idempotency-Key>`, text messages included, so a retried tap sends
  one message. One step reads what the send depends on (status, listing,
  phone, conversation, block) and the flow decides between steps; then
  steps open the pair's conversation, add the message and attach its
  photos to it in media (holder: the message), deleting the message again
  when media refuses the attach; a photo message then sends
  `MessagePublish`.
- A photo message is `processing` until `MessagePublish` has its photos
  through `MediaProcess`, then `sent` or `failed`. Only its sender sees it
  until it is sent.
- Each member has a read marker, which only a newer message moves, and an
  archive, each written by a statement of its own (`MarkRead` updates only
  when the message is newer), so two devices never undo each other. The next sent message clears both members' archives, so
  archived means `archived_at` is set. Unread counts the other member's
  sent messages after the marker. The inbox pages by the conversation's
  last sent message ID.

### Screen

Module `screen` serves one `GET` per view of the app (decided 2026-10-27,
spec: `~/shopnexus/specs/2026-10-27-server2-app-alignment-design.md`). The
app reads a view in one call, and after a stream hint it reloads the view
it shows in one call. Mobile and desktop read the same views; a desktop
page that shows two views calls both at once.

- Screen owns no table. Each view is one method of its service that reads
  the other modules' services top to bottom; depguard forbids every other
  module to import it, so it adds no dependency between the others.
- `GET /v1/screens/listing/{id}` (signed in or not): the public listing,
  the seller's summary and, for a signed-in reader who is not the seller,
  `viewer {saved, deal, conversation_id}`, where `deal` is deal's
  `DealSummary` of the reader's deal. A listing that is not public is 404
  for everyone; its seller reads it through `GET /v1/listings/{id}`.
- `GET /v1/screens/seller/{id}` (signed in or not): trust's seller
  profile, the first page of the seller's public listings and of their
  reviews, and `viewer {blocked, conversation_id}`; `blocked` holds either
  way.
- `GET /v1/screens/inbox?archived=&cursor=&limit=`: chat's inbox page,
  each row with the pair's deal from `PairSummaries` and
  `other_active_deals`, and the caller's unread total. The row's deal is
  the newest of the caller's turn, else the newest active (negotiating or
  agreed), else the newest, where newest means the newest move (a deal
  without moves counts from its opening); deal's domain holds the turn
  rule (`TurnOf`). The buyer's turn on an agreed deal needs the listing
  still active, since `RequestCod` refuses a hidden or reserved one. The
  chat view lists the pair's deals by when they opened, newest first. Its `cursor` pages the inbox, since a row of the next page
  needs the same deal fields.
- `GET /v1/screens/chat/{conversation_id}`: the conversation, the pair's
  deals and the first page of messages; anyone but a member gets
  `conversation_not_found`.
- "Load more" uses the list endpoints, which return the same item shapes:
  `/v1/sellers/{id}/listings`, `/v1/sellers/{id}/reviews`,
  `/v1/conversations/{id}/messages`.
- Each piece of data has one read path: `GET /v1/conversations`,
  `GET /v1/conversations/{id}` and `GET /v1/sellers/{id}` were removed
  when the views came, and no resource carries fields for one screen.
- A view makes a fixed number of service calls per page, and the services
  read a page's people, communes and listings in one query each.

### Trust

Module `trust` holds reviews (spec:
`~/shopnexus/specs/2026-10-26-server2-reviews-saved-design.md`).

- The buyer of an order reviews its seller once the order is `shipped`,
  `delivered` or `returned`: 1 to 5 stars and up to 500 characters of
  text. One review per order, since a deal can hold several orders.
- The buyer edits, deletes or posts it again within 7 days of the first
  submission (`created_at`); after that it is `review_locked`. A delete
  keeps the row with `deleted_at` and no text, so posting again cannot
  restart the 7 days (decided 2026-10-27). A suspended account cannot
  write one.
- Review writes follow the `write_id` rule: `Insert` refuses an order that
  has a review and `Save` a stale row, both `try_again`.
- `trust` reads the order through `dealsvc.Service.Order`; someone else's
  order is `order_not_found`. `GET /v1/sellers/{id}/reviews` is public and
  carries the seller's average and count over all reviews.
- `Service.Seller` is a seller's public profile: name, avatar, bio, when
  they joined, rating, and catalog's on-sale and sold counts. Screen reads
  it; it has no endpoint of its own.

### Discovery

Module `discovery` holds saved listings.

- A person saves someone else's listing that is active or reserved, at
  most 500; the save keeps the price at that moment. Saving again changes
  nothing.
- `GET /v1/saved` lists the saves newest first with each listing as it is
  now. A listing hidden or deleted after the save shows its status alone,
  since its seller took it down.
- Each save has a UUIDv7 `id`, so the list pages by it. The 500-save cap
  is counted under `pg_advisory_xact_lock` on the account.

### Testing

There are no in-memory fakes. Anything that touches a store is tested on
Postgres: `pgtest.New(t)` clones a migrated template database per test. A
fake would copy Postgres behaviour (microsecond timestamps, the `write_id`
rule, `ON CONFLICT`, `UNIQUE (kind, value)`) and need a contract suite to
prove the copy. `go test -short` runs only what needs no database.

- Entities: unit tests, no database. The business rules live here.
- Repositories: tests on Postgres, including `write_id`: a stale T
  conflicts, a second `Save` with the same N succeeds, and two `Save`s in a
  row with new IDs both succeed.
- Services: tests on Postgres, through the module's service built on real
  repositories with a stopped clock. A test sets only the dependencies its
  method uses, and wraps a repository when it needs a conflict or a failure.
- Handlers: middleware tests with `httptest`; the endpoints run in e2e.
- Restate services: integration tests against a Restate server from the test
  stack, with the endpoint served in-process (`restatetest.Start`) and a
  hand-moved clock (`restatetest.Clock`). They hold the flow's business
  rules, since the flow lives in the handler. A store wrapper that embeds
  `restatetest.CommitThenFail` makes a store commit and then fail once with
  a retryable error; Restate retries the step, and the test checks that no
  row is duplicated, no send is counted twice and the compensation ran at
  most once.
- E2E: the server in-process (HTTP and Restate endpoint), Restate and Postgres
  from the test stack, the deployment registered in `TestMain`, SMS and email
  captured by a fake sender. The four OpenAPI conformance tests from plan 1
  run unchanged on top of it. `e2e/helpers_test.go` holds the HTTP helpers
  every module's tests use; `auth_helpers_test.go` signs a test account in.

Every test and subtest calls `t.Parallel()`; paralleltest enforces it. A test
that sets process environment variables or installs the global OTel tracer
provider runs serially and says why in its `//nolint:paralleltest` comment.
The httpx package installs an SDK tracer provider in `TestMain`, so only the
tests that read recorded spans swap the global.

### Linting

`make test` runs golangci-lint with `.golangci.yml`, which also checks gofumpt
formatting. The rules that shape the code:

- wrapcheck: an error from another package is returned with context,
  `fmt.Errorf("get session: %w", err)`. A Restate handler wraps each step's
  error with the step's name. Code that builds an `*apperr.Error` and
  `MarshalJSON`/`UnmarshalJSON` methods return errors as they are; so do
  `http.ResponseWriter` and `slog.Handler` wrappers, with a `nolint` comment.
- exhaustruct, for `config` types only: a `config.Config` literal names
  every field, so a new variable shows every place that builds one. A field
  that may stay empty carries `//exhaustruct:optional`. Other struct
  literals name only the fields they set.
- govet shadow: no `err` hides another `err` in scope. Inside a Restate step
  closure the error is `stepErr`.
- recvcheck: a type with one pointer-receiver method has only pointer
  receivers, except `MarshalJSON`, which `json.Marshal` skips on a value
  otherwise.
- nolintlint: every `//nolint` names the linter and gives the reason.
- depguard: a module's domain, repository and service import no layer above
  them, a handler or worker imports no repository, nothing imports a
  worker, and production code reaches another module only through its
  `service`. An error a client sees, such
  as `account_not_found`, lives in `domain` for that reason.

### Restate in practice

Plan 2 settled these details on Restate 1.8.0 and Go SDK v1.1.0.

- Restate runs with `network_mode: host` on 127.0.0.1 ports: 56580 ingress,
  56570 admin and 56560 node in development, one higher in tests. The
  development machine's ufw drops container-to-host traffic.
- At start the server serves its services on `RESTATE_LISTEN_ADDR` and
  registers `RESTATE_ENDPOINT_URL` through the admin API with
  `use_http_11`, because the endpoint is a standard library HTTP/1.1 server.
- The Restate server signs every request to the endpoint with the Ed25519
  key in `.restate/identity.pem`
  (`worker.invoker.request-identity-private-key-pem-file`), and the endpoint
  runs only requests signed with it: its public half is
  `RESTATE_IDENTITY_KEY`, required with `APP_ENV=prod`. `make run` and
  `make test` create the key file, and tests read the public key from it.
- With `APP_ENV=prod` the server refuses to start while SMS or email goes
  through a log sender, which would write every OTP code to the log.
- An OTP code is `HMAC(OTP_KEY, "otp-code:" ‖ challenge_id) mod 10^6`. The
  send step recomputes it on a retry, and it is never stored or journaled.
  The verify handler loads the challenge and sends `SignIn` the code's hash.
  A check of the journal after a manual sign-in on 2026-10-20 found the phone
  number and the code hash, and no code or token.
- `RESTATE_SERVICE_PREFIX` (`config.RestateConfig.ServicePrefix`) goes before
  every service name. Test packages share one Restate server and a name
  routes to one endpoint, so `restatetest.Config` gives every registration a
  fresh prefix.
- Plan 2 first judged inside a step. A retried step then applied its change
  again (finding I1 of the plan 2 review: a spurious `otp_too_soon` or
  `code_expired`). The fix had each step look for its own write ID and
  rebuild the verdict, and that check reported an expired code as
  `invalid_code` and a wrong code on a used challenge as a success. The read,
  decide, save split replaced both on 2026-10-21.
- Restate answers a repeated `Idempotency-Key` with the first result,
  whatever the new request carries. The verify handler therefore issues tokens
  only when the result names the request's challenge and the request's code
  hashes to the challenge's `code_hash`, and a replay never revives a session
  that was signed out since.

