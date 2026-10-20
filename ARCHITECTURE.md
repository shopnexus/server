# server2 architecture

The redesign of 2026-10-20 replaces parts of
`~/shopnexus/specs/2026-10-19-server2-foundation-design.md`. Where the two
disagree, this file wins until the spec is rewritten. Plan 2
(`~/shopnexus/plans/2026-10-20-server2-plan2-otp-sessions.md`) was written for
the old design and is on hold.

The code takes from two branches of the old `server` repository.
`origin/microservice-ready` reads top to bottom with few layers.
`origin/cleancode` has hand-written SQL repositories, idempotent inserts and
Restate workflows split by phase. server2 adds rich entities, OpenAPI-first
contracts and contract tests.

## Decided

Every section below is decided. New proposals go in a section at the end
until the user approves them.

### Infrastructure

Postgres and Restate (server 1.8.0, Go SDK v1.1.0). Restate replaces the
outbox, the relay, JetStream and NATS; package `jobs` is removed.

### Use cases

A use case is a plain function that takes ports and parameters:
`func AccountRename(ctx context.Context, accounts Store, now time.Time, id uuid.UUID, name string) (Account, error)`.
A port is an interface; real and fake adapters implement it. Another module's
use case reaches a caller through a port the caller declares. Business code
takes no functions as arguments: no update callbacks, no transaction closures.

### Transactions and concurrency

A transaction lives inside an adapter method and covers one aggregate.
Concurrent writes are caught optimistically: `Save` fails with `ErrConflict`
when the aggregate changed since it was read. A conflict reply to an OTP guess must not reveal whether
the guess was right. `write_id` (below) is the column that detects it.

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

Only flows that need durability run as Restate workflows. Other use cases are
called directly from HTTP handlers with `context.Context`, so reads and
refresh keep working when Restate is down.

- OTP send is a workflow keyed by the client's `Idempotency-Key`. One
  `restate.Run` stores the challenge, the next sends the SMS or email.
- A flow that touches two aggregates (sign-up: `Account`, then `Credentials`)
  is a workflow with a saga. Each step registers its compensation before it
  runs. Compensations run in reverse on a terminal error, each inside
  `restate.Run` so a replay does not repeat one that already ran.
- Every step inside `restate.Run` is idempotent. IDs are minted before the
  step by `newID(ctx, at)`, a UUIDv7 built from the journaled clock and the
  invocation's deterministic random generator (`restate.UUID` gives version
  4, which would break the spec's time-ordered cursors). Inserts use
  `ON CONFLICT (id) DO NOTHING`. The same ID is the idempotency key sent to a
  payment provider.
- A workflow ID comes from the client's `Idempotency-Key` or from a business
  key, never from `uuid.New()`. Restate refuses a second submission of the same
  key.
- Workflows live in `*_workflow.go` files of their module and are the only code
  that passes closures, to `restate.Run`.

### Module layout

```
internal/account/
  account.go              entity: rules, write_id
  account_store.go        port
  account_store_pg.go     adapter: hand-written SQL, one transaction per method
  account_rename.go       use case and its HTTP handler
  routes.go               binds adapters to handlers
  accounttest/            fake and contract suite
```

One binary serves the HTTP API on `:8080` and the Restate endpoint on `:9080`.
HTTP handlers either call a use case or call a workflow through the Restate
ingress and wait for its result. Module account offers an adapter
(`account.Client`) that implements the ports other modules declare.

### Tables and aggregates

| Aggregate | Tables | Notes |
|---|---|---|
| `Account` | `accounts` | adds `write_id` |
| `Credentials` | `credentials` (root: `account_id` PK, `write_id`, later `password_hash`), `identities` (children) | `Save` changes the root's `write_id` and rewrites the account's identities in one transaction; the planned `passwords` table folds into the root |
| `Session` | `sessions` | adds `write_id` and `auth_time` |
| `Challenge` | `otp_challenges` | adds `write_id` |

IDs are chosen by the caller before the insert: `newID(ctx, at)` inside a
workflow, `uuid.NewV7()` in a direct use case. `uuidv7()` stays as the column
default. `Create` inserts with `ON CONFLICT (id) DO NOTHING`.

`write_id` is the ID of the last write to an aggregate and replaces a version
counter. `Get` returns it as T. `Save` picks a new ID N (from
`newID(ctx, at)` in a workflow, so a retry sends the same N) and runs:

```sql
UPDATE ... SET ..., write_id = @write_id
WHERE id = @id AND (write_id = @read_write_id OR write_id = @write_id)
```

Stores pass parameters as `pgx.StrictNamedArgs` with `@name` placeholders.
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
`ErrConflict` although its change landed. The workflow handles it like any
conflict: read again and decide.

`write_id` differs from the client's `Idempotency-Key`, which names a whole
workflow so Restate refuses a duplicate submission.

### OTP send

Workflow `OTPSend`, run once per `Idempotency-Key`:

1. Admit the request against two `Quota` aggregates, `ip:<address>` first
   and then `dest:<destination>`, in table `otp_quotas(key PK, write_id,
   sent_at timestamptz[])`. An IPv6 address is keyed by its /64. The address
   goes first so an address over its limit cannot spend the quota of the
   numbers it names. `Quota.Admit(now, rule)` holds the limits: one code per
   destination per 45 s, five per destination per hour, twenty per address per
   hour. Parallel requests conflict on `write_id` and retry; after three lost
   races the request gets 429. A quota that admitted a request stays spent
   even when a later step fails.
2. `restate.Run`: create the `Challenge`.
3. `restate.Run`: send the SMS or email through `sms.Sender` or `email.Sender`.
4. Return `challenge_id`; the HTTP handler waits for it.

### Idempotency-Key

Every endpoint that starts a workflow requires an `Idempotency-Key` header (a
UUID) and uses it as the workflow ID. A repeated key attaches to the first
run and returns its result, so a retried tap gets the same answer.

### OTP verify and sign-up

Workflow `SignIn` returns `{account_id, is_new}`:

1. `restate.Run`: load the challenge, `Verify`, `Save`. A wrong code is saved
   and ends the workflow with `invalid_code`; nothing needs compensating.
2. `restate.Run`: find the identity in `Credentials`.
   - Found: check the account status, mark the identity used.
   - Not found: take `accountID := newID(ctx, at)`, register the
     compensation `account.Delete`, create the `Account`, then create the
     `Credentials` with the identity. If that fails, the compensation deletes
     the account.

Tokens never enter Restate: its journal and workflow outputs are stored and
readable through the Restate admin API. The HTTP handler issues the session
after the workflow returns, with a session ID the workflow chose. When the
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
3. `token := s.Rotate(now)`, `Save`. `ErrConflict` means a concurrent refresh
   with the same token won, so the use case reloads, revokes and returns
   `session_revoked`.
4. Sign the access token with the session's `auth_time`.

Logout and removing one device load the session, check that the caller owns
it (another account's session is 404), `Revoke` and `Save`, retrying once on
`ErrConflict`.

Two kinds of call skip the aggregate:

- A read returns view structs from a query and never loads an entity. The
  device list is `sessions.ListActive(accountID, cursor, limit)`.
- A bulk change that only moves a one-way flag may run as one set-based
  statement that also changes each row's `write_id`. `logout-all` is
  `UPDATE sessions SET revoked_at = @now, write_id = uuidv7() WHERE
  account_id = @account_id AND revoked_at IS NULL`. Loading and saving each session
  would cost one transaction per device and protect no rule.

The access token is checked by signature and expiry only; the middleware reads
no database.

### Testing

- Entities: unit tests.
- Use cases: unit tests with fakes from `<module>test/`. A test passes only
  the ports its use case takes.
- Adapters: one contract suite per port, run against the fake and Postgres.
  It covers `write_id`: a stale T conflicts, and a second `Save` with the same N
  succeeds.
- Workflows: integration tests against a Restate server from the test stack,
  with the endpoint served in-process. One named test per workflow makes an
  adapter commit and then fail once with a retryable error. Restate retries the
  step, and the test checks that no row is duplicated and the compensation ran
  at most once.
- E2E: the server in-process (HTTP and Restate endpoint), Restate and Postgres
  from the test stack, the deployment registered in `TestMain`, SMS and email
  captured by a fake sender. The four OpenAPI conformance tests from plan 1
  run unchanged on top of it.

### Changes to the code from plan 1

- Removed: `internal/platform/jobs`, `natstest`, NATS in both Compose files,
  the outbox migration, `SendCommand` types in `provider/sms` and
  `provider/email` (their `Sender` ports stay), the relay and JetStream
  metrics.
- Migrations are squashed into new files that already contain `write_id`.
  Nothing is deployed, so `make down` with volumes and a fresh migrate is the
  whole cost.
- Added: Restate 1.8.0 in both Compose files, with ingress and admin ports in
  the 565xx range, and config `RESTATE_INGRESS_URL`, `RESTATE_ADMIN_URL`,
  `RESTATE_LISTEN_ADDR` and `RESTATE_ENDPOINT_URL` (how Restate reaches this
  process; the server registers it with the admin API at start). `/readyz` checks Postgres and the Restate admin
  health endpoint.
- Module account is rewritten as functions with ports.

### Spec and plan

`ARCHITECTURE.md` keeps the architecture rules. The spec in
`~/shopnexus/specs/` is rewritten to keep scope, endpoints, auth flows,
security rules and the testing matrix, and to point here for code shape,
transactions and durability instead of repeating them. Plan 2 is then written
again from both.

### Restate in practice

Plan 2 settled these details on Restate 1.8.0 and Go SDK v1.1.0.

- The durable flows are Restate services invoked with the client's
  `Idempotency-Key`. A repeated key returns the first result and runs no step
  again, which is all this phase needs from a workflow.
- Restate runs with `network_mode: host` on 127.0.0.1 ports: 56580 ingress,
  56570 admin and 56560 node in development, one higher in tests. The
  development machine's ufw drops container-to-host traffic.
- At start the server serves its services on `RESTATE_LISTEN_ADDR` and
  registers `RESTATE_ENDPOINT_URL` through the admin API with
  `use_http_11`, because the endpoint is a standard library HTTP/1.1 server.
- A business refusal travels in the result as a `Refusal`. The ingress turns
  a terminal error into a 500 carrying only a message.
- An OTP code is `HMAC(OTP_KEY, "otp-code:" ‖ challenge_id) mod 10^6`. The
  send step recomputes it on a retry, and it is never stored or journaled.
  The verify handler loads the challenge and sends `SignIn` the code's hash.
  A check of the journal after a manual sign-in on 2026-10-20 found the phone
  number and the code hash, and no code or token.
- Tests prefix every service name per registration, because test packages
  share one Restate server and a name routes to one endpoint.
- A retried step reruns its whole closure, `Get` included, after its first
  run may have committed. Every step that saves an aggregate therefore checks
  first whether the aggregate already carries the step's journaled write ID;
  when it does, the step returns the outcome that write left (a quota counts
  the send once, a challenge reports the verdict it saved) instead of
  applying the change again.
- Restate answers a repeated `Idempotency-Key` with the first result,
  whatever the new request carries. The verify handler therefore issues tokens
  only when the result names the request's challenge and the request's code
  hashes to the challenge's `code_hash`, and a replay never revives a session
  that was signed out since.

