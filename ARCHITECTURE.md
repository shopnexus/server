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

Postgres and Restate (server 1.8.0, Go SDK v1.1.0). Restate replaced the
relay, JetStream and NATS. The outbox is a table, `events`, that a bus
drains into Restate (see Events). `/readyz` checks Postgres and the Restate
admin health endpoint.

### Use cases

A use case is a method of the module's service, in its own file:
`func (s *Account) Rename(ctx context.Context, id uuid.UUID, name string) (AccountView, error)`
in `account/service/account_rename.go`. `service.Account` holds the module's
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

- `Account` (module account): display name, status, suspension.
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
  `aggregate.past_tense` (`account.renamed`, `account.suspended`,
  `account.unsuspended`), the `SubjectID` of the aggregate, and a `Data`
  map that only a business event fills. A subscriber reads the subject's
  current state through its module's `Service`, so order, delay and
  duplicates do not change the result.
- A repository writes the events of a change with `events.Insert` in the
  change's transaction, as it writes an audit entry: `Save(ctx, a, writeID,
  evs...)`. A conflicting write leaves no event. An event with an ID that
  exists already is not written again, so a durable step that retries its
  save with the event IDs from its journal writes one event. `Insert` also
  runs `pg_notify('events', '')`, which Postgres delivers on commit.
- A subscriber module's `Mount` calls `d.Events.Subscribe(kind, service,
  handler)` for a handler of one of its Restate services, with the
  signature `func(restate.Context, events.Event) error`. `Subscribe` after
  the bus has started panics, and `app.New` refuses to start when a
  subscription names a service or handler this process does not serve.
- `events.Bus.Run`, started by `app.New` and stopped by `App.Close`,
  repeats a round: in one transaction it takes up to 100 pending rows
  `FOR UPDATE SKIP LOCKED`, sends each to every subscription through
  `ingress.ServiceSend` with the idempotency key
  `<event id>/<service>/<handler>`, and sets `dispatched_at` on what it
  sent. Each send gives up after 5 seconds. When Restate refuses one
  delivery (4xx), the bus logs it at ERROR, leaves that event pending and
  goes on; any other failure ends the round, and the next round sends the
  rest again. Restate drops a repeated key for 24 hours. Once an hour, and
  at start, the bus deletes rows dispatched more than 7 days ago, 10,000 at
  a time.
- The bus listens on channel `events` on a connection of its own, outside
  the pool, and also runs a round every second. A pooled connection would
  hold a slot for good, and `Pool.Close` would wait for it.
- Each event to each subscriber is its own Restate invocation, which
  Restate retries until the handler succeeds; one failing event blocks no
  other. While Restate is down, events wait in the table and the API keeps
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
    account_rename.go  one use case per file, a method of Account
  handler/           HTTP: request structs, one method per endpoint
    handler.go         type Handler, Register
    account_rename.go
  account.go         Mount: builds the layers, returns service.Service
```

Modules auth and staff have the same four packages. Imports run one way,
`handler` → `service` → `repository` → `domain`, and depguard rules in
`.golangci.yml` refuse an import against it.

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
   and then `dest:<destination>`, in table `otp_quotas(key PK, write_id,
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
the same answer.

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
  them, a handler imports no repository, and production code reaches
  another module only through its `service`. An error a client sees, such
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

