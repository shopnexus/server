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
outbox, the relay, JetStream and NATS. `/readyz` checks Postgres and the
Restate admin health endpoint.

### Use cases

A use case is a plain function that takes ports and parameters:
`func Rename(ctx context.Context, accounts Store, id uuid.UUID, name string, now time.Time) (View, error)`
in package account.
A port is an interface, implemented by the Postgres adapter; a test wraps
that adapter to inject a fault. Another module's use case reaches a caller
through a port the caller declares. Business code takes no functions as
arguments: no update callbacks, no transaction closures.

Code is a function by default: a use case, an HTTP handler, an entity
method. A type with methods exists only where something needs one: an
adapter, because a port is an interface, and a Restate service, because the
SDK binds a struct.
A flow that runs as a Restate service is written in the service handler
itself (see Durable flows), so it is not a separate use case.

### HTTP handlers

A handler runs in one order: `httpx.Caller` on a signed-in route, `httpx.Bind`
for the request, the use case, then `httpx.Respond` or `httpx.WriteError`.
`httpx.Bind(r, &in)` fills a request struct. A field tagged `path`, `query` or
`header` takes that request value and also carries `json:"-"`; the other
fields come from the JSON body. The spec middleware has already checked every
value and filled in query defaults, so `Bind` converts strings to Go types
(`string`, `int`, `bool` and any `encoding.TextUnmarshaler`) and a handler
repeats no shape rule. `Bind` reads the tags of a type once and keeps the
result in a `sync.Map`.

### Transactions and concurrency

A transaction lives inside an adapter method and covers one aggregate.
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
  and message of a terminal one, which would lose `code` and `details`.
- A step that can lose a race or find an identity taken returns that as
  data: `restatex.Outcome(err, otp.ErrConflict)` gives `(true, nil)` for the
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
- A Restate service lives in a `*_service.go` file of its module, and is the
  only code that passes closures, to `restate.Run`. The HTTP handler that
  invokes it sits in the matching file (`otp_send.go`, `sign_in.go`).

### Module layout

```
internal/account/
  account.go              entity: rules, write_id
  account_store.go        port
  account_store_pg.go     adapter: hand-written SQL, one transaction per method,
                          methods in the port's order (Create, Get, Find..., Save, Delete)
  account_store_pg_test.go
  account_rename.go       use case and its HTTP handler
  routes.go               binds adapters to handlers
```

Every aggregate's files follow this layout.

One binary serves the HTTP API on `:8080` and the Restate endpoint on `:9080`.
HTTP handlers either call a use case or call a Restate service through the
ingress and wait for its result. Module account offers an adapter
(`account.Client`) that implements the ports other modules declare.

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

Refresh, logout and the device list run as plain use cases against the
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

### Testing

There are no in-memory fakes. Anything that touches a store is tested on
Postgres: `pgtest.New(t)` clones a migrated template database per test. A
fake would copy Postgres behaviour (microsecond timestamps, the `write_id`
rule, `ON CONFLICT`, `UNIQUE (kind, value)`) and need a contract suite to
prove the copy. `go test -short` runs only what needs no database.

- Entities: unit tests, no database. The business rules live here.
- Adapters: tests on Postgres, including `write_id`: a stale T conflicts,
  and a second `Save` with the same N succeeds.
- Use cases: tests on Postgres. A test passes only the ports its use case
  takes, and wraps an adapter when it needs a conflict or a failure.
- Restate services: integration tests against a Restate server from the test
  stack, with the endpoint served in-process and a hand-moved clock. They
  hold the flow's business rules, since the flow lives in the handler. Tests
  make a store commit and then fail once with a retryable error; Restate
  retries the step, and the test checks that no row is duplicated, no send
  is counted twice and the compensation ran at most once.
- E2E: the server in-process (HTTP and Restate endpoint), Restate and Postgres
  from the test stack, the deployment registered in `TestMain`, SMS and email
  captured by a fake sender. The four OpenAPI conformance tests from plan 1
  run unchanged on top of it.

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
- exhaustruct, for this module's types outside tests: a struct literal names
  every field, zero values included, so a new field shows every constructor
  that must set it. A field that is legitimately absent carries
  `//exhaustruct:optional`; `OTPSendResult` and `SignInResult` carry it on
  the type, since they hold either `Refused` or the other fields.
- govet shadow: no `err` hides another `err` in scope. Inside a Restate step
  closure the error is `stepErr`.
- recvcheck: a type with one pointer-receiver method has only pointer
  receivers, except `MarshalJSON`, which `json.Marshal` skips on a value
  otherwise.
- nolintlint: every `//nolint` names the linter and gives the reason.

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

