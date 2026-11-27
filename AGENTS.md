# server

One Go binary, a modular monolith: modules own their tables and call each
other only through their service. It serves the HTTP API and the endpoint
Restate calls. Postgres (ParadeDB) holds every table; Restate runs the flows
that must survive a crash and delivers events between modules.

## Where things are written down

- How the system works (flows, states, rules, limits) is the docs site:
  `~/shopnexus/docs/web/src/content/docs/en/`. Read the page of the part you
  change, not all of them. When your change alters a flow, a rule or a
  limit, update that English page in the same piece of work.
- The API contract is `api/` (OpenAPI). Requirement IDs are
  `~/shopnexus/docs/registry/requirements.yaml`.
- `~/shopnexus/docs/superpowers/` specs and plans are history. The site and
  the code win where they disagree.

| Working on | Read |
|---|---|
| Module boundaries, a new module | `architecture/modules`, `architecture/data` |
| A Restate flow or an event | `architecture/durable-flows`, `architecture/events` |
| Sign-in, sessions, account, erasure | `architecture/sign-in`, `sessions`, `account` |
| Listings, photos | `architecture/catalog`, `listings-lifecycle`, `media` |
| Deals, orders, carriers | `architecture/deal`, `shipping` |
| Chat, notifications, the live stream | `architecture/chat`, `notifications`, `api/live-updates` |
| Reviews, reports, sanctions, scam warnings | `architecture/trust` |
| The one-call views behind each app screen | `architecture/screens` |
| Search, saved searches, Home feeds | `architecture/search-and-discovery`, `feeds` |
| Listing reads, models, AI settings | `architecture/assist`, `ai` |
| Console, the console agent | `architecture/console`, `agent/` |
| Errors, paging, idempotency, rate limits | `api/` |
| Tests, metrics, seed data | `operations/testing`, `monitoring`, `local-stack` |

## Working rules

- Use the newest release of every component (Go, Postgres, Restate,
  libraries). Look the version up before pinning it; a release candidate is
  fine in development.
- Rename or restructure whenever a better name or shape exists, and report
  it. Refactor effort is not a reason to keep a worse name.
- A feature starts with its OpenAPI spec: endpoints, schemas, errors. The
  data model and the code follow. When the spec shows patches (a field for
  one caller, overlapping endpoints, names that disagree), redesign that
  part of the spec in one change and refactor the code to it.
- Configuration has no defaults, in Go, Compose, the Makefile or test
  helpers. A missing variable stops the process with its name. A variable
  whose empty value means something is still set, to the empty string.
- Design decisions go to the user one question at a time, with a check
  question on choices that touch security or data. An approved plan runs
  without stopping to ask.
- Scratch files, logs and test output go in `tmp/` (ignored), never `/tmp`.

## Code shape

- A module has `domain` (entities, rules, client-visible errors),
  `repository` (one interface per aggregate, hand-written SQL), `service`
  (the module's contract and its use cases), `handler` (HTTP) and, when it
  reacts to events, `worker`. Imports run handler/worker → service →
  repository → domain, and another module is reached only through its
  service; `internal/layers_test.go` enforces both.
- A use case is one method of the module's service, in its own file. It
  reads top to bottom: no phase methods or helpers called once, no
  callbacks or transaction closures passed in. It reads the clock once. A
  helper is fine when several places call it.
- A handler binds the request, calls one service method and responds. The
  spec middleware has already validated the request, so a handler repeats
  no shape rule.
- An entity keeps its fields unexported; only repositories rebuild one or
  record its write. Validation collects every bad field into one error.
- A view behind an app screen makes a fixed number of service calls per
  page, whatever the page size.
- A use case names its requirements: the service method's doc comment ends
  on its IDs, steps are marked `// 1.`, `// A1`, `// E2` where the branch
  starts, and a test names the flows it covers. Handlers, repositories and
  the domain carry no UC IDs. `make trace` in the umbrella checks them.

## Writes

- A transaction lives in one repository method and covers one aggregate.
- Saves are optimistic through `write_id`: the caller mints the new ID
  before the write, so a retry sends the same one, and zero rows is a
  conflict. A use case that loses the race answers 409 `try_again`.
- The caller mints every ID (UUIDv7) and inserts with
  `ON CONFLICT DO NOTHING`. Every write a retry can repeat has a dedupe
  key: a natural UNIQUE, the request's `Idempotency-Key`, or an ID from an
  earlier journaled step. Outside calls carry our key.
- A repository sends its save, its events, its stream updates and its audit
  entry in one batch; a loop over rows is one statement over arrays. Break
  the batch only where a result decides the next write.
- SQL takes named parameters through the strict variant, never `$1`.
  Advisory locks go through the platform helper.
- A rule one row can show is also a CHECK (added `NOT VALID` on a table
  with rows). Every foreign key has an index. No column is named after an
  SQL keyword. Never edit an applied migration.
- A cap or "only one" rule holds under an advisory lock on the owner, in
  the insert's transaction, backed by a partial unique index where it can
  be. A write that keeps a row of the caller share-locks the account row
  and refuses a deleted account.
- A state change writes its event, and its audit entry, in the same
  transaction; every console write has its audit row.
- A request that finds its change already made answers the current state
  and writes nothing.
- Best-effort side writes (history, view counts) run after the main work,
  log a warning and never fail the request.
- Store object keys, never URLs. Settings with no row read as defaults;
  `PUT` replaces the whole document.
- Lists page by keyset over the ID, newest first.

## Flows and events

- Only work that must finish after a crash runs on Restate; everything else
  is a plain call, so the API keeps working while Restate is down.
- A step only reads or writes a store. Decisions are taken between steps
  on journaled values. IDs and the clock come from journaled helpers,
  never inside a step. A business refusal leaves the flow as data, not as
  an error Restate would retry.
- Tokens, codes, file bytes and private input never enter the journal:
  check or fetch outside it, or inside one step, and journal only the
  result or a key.
- Modules share no transaction. A flow reserves, calls out, then saves;
  on a refusal or a lost race it undoes what it did, and any leftover must
  be harmless. A module maps another module's refusals to its own codes.
- A periodic job that touches a store has no in-process timer: every
  replica sends it to Restate keyed by its period, so it runs once, on the
  newest version, never on a draining one.
- A timer is a delayed send naming the entity. When it fires it reads the
  current state and does nothing if the entity moved on.
- An event is thin: a kind (`aggregate.past_tense`), the subject ID, and a
  payload only for a business event. A subscriber reads current state
  through the owning module's service, so order, delay and duplicates do
  not matter, and a subject that is gone is a no-op. Subscriptions are
  made in the module's mount.
- Code that syncs settings to an outside system (the AI gateway) saves the
  row first, then syncs, reconciles on a schedule, and touches only what it
  created.

## Errors, text and limits

- Wrap an error from another package with context; a Restate handler
  wraps each step's error with the step's name, and the error inside a
  step closure is `stepErr`. A client-visible error is a code in `domain`;
  `field_codes` sit beside `fields`.
- Outgoing HTTP goes through the platform client, never a bare
  `http.Client`. Metric labels are route patterns or fixed sets, never a
  raw path or a client value. Exact locations, photos and vectors never
  reach a log.
- Text the server writes follows `Accept-Language` (Vietnamese by
  default). Whatever a model reads or is stored stays Vietnamese.
- Every route has a rate limit tier, named in the spec with
  `x-rate-limit`; quotas that must hold across replicas live in Postgres or
  Restate.
- The spec keeps to the shapes the app's generated client takes: a
  `oneOf` has a discriminator, alternative field sets are one object with
  `required`-only branches, no free-form maps.

## Tests

- No in-memory fakes. Anything that touches a store runs on Postgres, a
  cloned database per test; flows run against the test stack's Restate,
  with a store wrapper that commits then fails to prove a retry is safe.
- Entities carry the business rules and their unit tests. Service tests
  run on real repositories with a stopped clock and wrap a repository to
  inject a conflict or a failure. Repository tests cover the `write_id`
  cases.
- Every test and subtest calls `t.Parallel()`; a serial one says why in
  its `nolint` comment. Every `nolint` names the linter and the reason.
- `exhaustruct` applies to config types only. Test-support packages
  (`*test`) are imported only by tests.

## Commands

- `make run`: start the development stack and the server. The first run
  writes `.env` with fresh keys; migrations apply every run.
- `make test`: every test against the test stack, which it starts, then
  `golangci-lint run`. Afterwards the stack stays up, so
  `go test ./internal/<module>/...` runs one module. Run `go vet ./...`
  too: it compiles every test file.
- `make down`: stop both stacks.
- `go run ./cmd/console create-admin --email <email> --name <name>`: create
  a console admin and print its temporary password. `disable`, `enable`,
  `unlock`, `reset-password` and `reset-totp` take `--email`. The API has no
  way to create or change an admin.
