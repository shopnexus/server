# server architecture

`~/shopnexus/docs/superpowers/specs/2026-10-19-server-foundation-design.md` holds scope,
endpoints, auth flows, security rules and the testing matrix, and points here
for code shape, transactions and durability. Where the two disagree, this file
wins. Plan 2 (`~/shopnexus/docs/superpowers/plans/2026-10-20-server-plan2-redesign-otp.md`)
built the sessions on this design; sign-in moved to Firebase on 2026-10-28,
and email codes and Google came back on 2026-11-10 (see Sign-in).

The code takes from two branches of the old `server` repository.
`origin/microservice-ready` reads top to bottom with few layers.
`origin/cleancode` has hand-written SQL repositories, idempotent inserts and
Restate workflows split by phase. server adds rich entities and
OpenAPI-first contracts.

## Decided

Every section below is decided. New proposals go in a section at the end
until the user approves them.

### Infrastructure

Postgres 18 as ParadeDB 0.26.3 (`paradedb/paradedb:0.26.3-pg18`, with
`pg_search` and `vector`), Restate (server 1.8.0, Go SDK v1.1.0) and RustFS
for photos (see Object store). Postgres went down from 19rc1 to 18 for
search, since pg_search builds for 15 to 18; it goes back to 19 when
ParadeDB supports it (decided 2026-10-27). Restate replaced the relay, JetStream and NATS. The outbox is
a table, `events`, that a bus drains into Restate (see Events). `/readyz`
checks Postgres and the Restate admin health endpoint.

Every model call goes through the stack's `ai-gateway`, the LiteLLM of the
`ai` repo, under an alias and a key per purpose (decided 2026-11-17, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-17-ai-hub-design.md`). The text
embedding model of search is the alias `embed-text`, `BAAI/bge-m3` through
OpenRouter, at `EMBED_TEXT_BASE_URL`; locally the server calls OpenRouter
itself. TEI served bge-m3 until 2026-11-17; OpenRouter's DeepInfra gives the
same vectors, so `EMBED_TEXT_VERSION` did not change. Photo vectors, speech
to text and video frames are module ai's, in this binary (see Module ai);
they went through LiteLLM and the `ai` repo's `ai-models` until 2026-11-18.
`/readyz` checks neither.

Each kind of vector can be off: an empty `EMBED_TEXT_MODEL` or
`AI_EMBED_MODELS` gives the modules a nil model. `ListingEmbed` then plans
no vector of that kind, `q` and the price sample leave its list out, and a
`photo` search with photos off answers 503 `search_unavailable`, as when the
model fails. The kind's version may then be empty, which matches no stored
vector, so `like` and the market price read none of it either.

A video read (see Assist) calls three more models. Frames and speech to
text are module ai's `Frames` and `Transcribe`. The vision model is at
`ASSIST_LLM_BASE_URL`: LiteLLM's alias `listing-read` in the cluster, the
LLM in `.env` locally. A 402 from the gateway (a spent
budget or OpenRouter credit) is a passing failure in `provider/llm`, like a
429, not a refused request.

A link read (see Assist) calls the same model at `ASSIST_LLM_BASE_URL`
with text only, and, through the cluster's open egress, Chợ Tốt's public
gateway at `CHOTOT_GATEWAY_URL` and its CDN at `CHOTOT_CDN_URL`.

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

A use case names the requirements it implements, by their IDs in
`registry/requirements.yaml` of the docs repository. The doc comment of a
`Service` method ends on a line of IDs, `// UC-DEAL-02 · NFR-CONC-01`. The
method's own doc comment starts with its name and says which UC, flow or
step it is: `// Open is UC-DEAL-02.`, `// Cancel is UC-DEAL-02 A3 on an
agreed deal and UC-DEAL-06 on a pending order.` A Restate handler does the
same on `Run`. Inside the method, `// 1.` is a step of the UC's main
flow, `// A1` an alternate flow and `// E2` an exception flow, each on the line
where the branch starts. A test names the UC and the flows it covers above
`func Test…`: `// UC-DEAL-02 A1, E1, E2, E3, E5.` An operation in `api/`
lists its UCs in `x-uc`. Handlers, repositories and the domain carry no
UC IDs; an NFR, BR or ADR ID may stand on the line that implements it.
`make trace` in the umbrella repository fails on an ID the registry does
not have, a flow the UC lacks or an `x-uc` that disagrees with the UC's
`api` list, and lists each finished UC and exception flow that no code or
test names.

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
(`string`, `int`, `float64`, `bool`, any `encoding.TextUnmarshaler`, and
named string types and slices of them, such as `[]catalogdomain.Condition`)
and a handler repeats no shape rule or conversion. `Bind` reads the tags of
a type once and keeps the result in a `sync.Map`.
`httpx.ClientIP(r)` is the caller's address, which the `ResolveClientIP`
middleware works out once per request from `TRUSTED_PROXIES`. On a route
anyone may call, `httpx.Viewer(r)` is the signed-in caller's account ID, or
nil signed out.
`httpx.Prefetch(r)` reports a browser's speculative fetch (`Sec-Purpose`),
for a read that would otherwise write (see Tin của bạn and listing views).

Every request passes `Trace`, `Recover`, `ResolveClientIP`, `Visitors`, CORS,
the rate limit and the spec validator, in that order, before the router. `Trace` is
outermost: it opens the span, records the duration and writes the access log
line after the request, so a request that panicked logs the 500 `Recover`
wrote, with its trace ID.

`Visitors` notes who called, for Grafana's Users dashboard, which counts
from the access log in Loki (decided 2026-11-23). The line gets `client`
(`android` for OkHttp, `website` for Node's fetch or a browser, `console`
for `/v1/console/`, `bot`, else `other`), which the request metric carries too,
and `visitor` with `country`. `visitor` is `a:<account>` or `m:<member>`
once the auth middleware ran (`authn.Track` hands it a slot), otherwise
`g:` and 12 hex digits of SHA-256 over the day and the address, so the
address stays out of the logs. A health probe and a call from a trusted
proxy itself, such as the website rendering without a buyer, get no
visitor.

A bot gets `client` `bot`, `visitor` `b:` with the same hash, and `ua`
with its User-Agent cut to 160 bytes, whoever it signed in as. The
User-Agent is the request's own, or `X-Forwarded-User-Agent` when a
trusted proxy sent it: the website forwards its browser's there, since
its own calls carry Node's. A bot matches a list of crawler, preview,
headless-browser, scanner and HTTP-library words, or lacks `Mozilla/`
while forwarded or while its `client` would be `other`. `httpx.Bot(ctx)`
answers before the handler runs, and `POST /v1/client-events` drops a
bot's batch with the usual 202. A bot with a browser's User-Agent passes
as a guest (decided 2026-11-23). `country` comes from `GEOIP_DB`, a MaxMind-format database the
image carries (`scripts/fetch-geoip.sh`, ip-location-db's CC0
geo-whois-asn-country); empty logs no country.
The validator matches only the operations the router serves, so a request
for a spec route this server does not mount, such as a dev route in
production, gets the router's 404 for an unknown path whatever its body
(decided 2026-11-15).

### Rate limits

Every route has a rate limit tier, and the tiers are one table in
`internal/platform/httpx/ratelimit.go` (decided 2026-11-23). An operation
names its tier in the spec with `x-rate-limit`; without it, GET is `read`
and every other method `write`, so a new route is limited from its first
commit. A path no route serves counts as `read` or `write` too.

| Tier | Per address | Per caller | Operations |
|---|---|---|---|
| `read` | 50/s, burst 1,000 | 20/s, burst 400 | GET by default |
| `write` | 10/s, burst 200 | 5/s, burst 100 | other methods by default |
| `auth` | 1/s, burst 60 | 1 per 5 s, burst 10 | sign-in, reauthentication, console login |
| `code` | 1 per 10 s, burst 10 | 1 per 30 s, burst 5 | the two that mail a code |
| `costly` | 1/s, burst 30 | 1 per 2 s, burst 20 | link previews, listing reads, price guides, carrier quotes and pickups |
| `telemetry` | 2/s, burst 60 | none | `POST /v1/client-events` |
| `exempt` | none | none | health checks, carrier webhooks, the dashboards' forwardAuth |

The rate limit middleware, between CORS and the spec validator, finds the
route with `Router.Route` and counts the request against its tier by client
address; an IPv6 /64 counts as one address. Carrier NAT puts many phones on
one address, so the address limits are the looser ones. Once the account
guard or the console's `Require` knows the caller, `httpx.LimitCaller`
counts the same tier per account or member. `httpx.Limit(tier)` counts a
module's own tier on part of a route, on top of the route's tier, and
refuses a shared tier's name: discovery's `guest-search` (2/s, burst 30 per
address) counts a guest's `GET /v1/search` with `q`. Past a limit the answer is 429 `rate_limited`
with `details.retry_after`, the seconds until the next token, and the
counter `http.server.rate_limited` grows by tier, route and what was
counted.

Each process keeps its buckets in memory and, once a minute, drops the
buckets that have refilled. Three replicas let an address through about three times as fast;
the limits stop floods and loops, and the rules that must hold exactly
across replicas are quotas a module keeps in Postgres or Restate:
`otp_limit`, `login_locked`, `read_limit_reached`, `agent_budget`,
`too_many_tickets`. `TestSpecRateLimitTiers` refuses an unknown tier name
and holds the list of `exempt` operations, and `NewLimits` refuses an
unknown name at startup. The e2e suite gives each test its own IPv6 /64 in
`X-Forwarded-For`, so tests running together do not share buckets.

### Spec shapes for generated clients

The app generates its Ktor client from the bundled spec with Fabrikt and
kotlinx.serialization. The spec keeps to four shapes that Fabrikt takes as
written (decided 2026-11-17):

- A `oneOf` of `$ref`s has a `discriminator` with a `mapping`, and each
  branch requires that property with a single `enum` value. `ShipmentDraft`
  picks its branch by `mode`. kin-openapi then checks a body against that
  branch alone; a body without `mode`, or with another value, is 400
  `invalid_request` with `fields.body: discriminator`.
- A body that takes one of several sets of fields is one object, written
  like `SignInRequest`: `properties` with every field, a `oneOf` whose
  branches list only `required`, and `additionalProperties: false`.
  `dependentRequired` keeps the sets apart where a field of one set could
  join another. `ListingReadRequest` takes `transcript` only with `video`;
  in `CarrierLogin`, GHTK's `email` and `password` need each other, and so
  do GHN's `token` and `shop_id`. A body that breaks it is 400 with
  `required` on each property it lacks. The app's script drops the
  `oneOf`, so its model has every field optional.
- No schema that an app operation reaches is a map of any values.
  `Error.details` lists `retry_after`, `attempts_left` and `reason` with
  `additionalProperties: false`. `apperr.Details` is a struct with the same
  three fields as pointers, set by `WithRetryAfter`, `WithAttemptsLeft` and
  `WithReason`, so a new detail is a field in both places. A detail that
  was set is sent, a blank `reason` included, and an unset one is not.
- The tag `internal` marks the operations the app does not call: the
  console, `/_dashboard/callback`, the carrier webhook, `/healthz`,
  `/readyz`, the dev routes and the website's `/v1/sitemap` routes.
  `websocket` marks `GET /v1/ws`, which the app opens with its socket
  code. The two tags are the one list of the routes the app leaves out:
  its client drops every operation with either tag. Neither tag is in
  `x-tagGroups`, so the docs list each operation under its module's tag
  only. kin-openapi v0.151.0, the version `go.mod`
  pins, rejects OpenAPI 3.2's tag `kind`, and v0.153.0 rejects it too, so
  the two tags have none. `TestSpecTags` checks that each operation has
  one tag of `x-tagGroups`, that each route above has its tag, and that no
  other operation has either.

### Pagination

A list endpoint pages by keyset over UUIDv7 IDs, newest first. Its handler
binds `page.Request` (the spec's shared `Cursor` and `Limit` parameters),
alone or embedded in its request struct, and passes it to the service and
the repository. The repository lists rows below `req.Before()`, which is
`uuid.Max()` on the first page, and reads `req.Fetch()` rows, one more than
the page holds. The service returns `page.Of(rows, req, id)`, which drops the extra
row and sets `next_cursor` to the ID of the last item kept.

A list ordered by another column keeps the ID cursor. The repository reads
the cursor row's sort key and lists the rows after it in (key, id) order.
`Accounts.ListSuspended` does this with the suspension's end, a missing end
as infinity. A list that needs a count adds a `total` field to its own
response schema, as `ConsoleAccountList` does; `page.Page` has none
(decided 2026-11-17).

### Transactions and concurrency

A transaction lives inside a repository method and covers one aggregate.
There are two exceptions. Sign-up: `Credentials.SignUp` inserts a new
`Account` and its `Credentials` together, so a sign-up that loses the phone
number writes neither. Erasure: `Accounts.Erase` saves the deleted account
and, in the same transaction, deletes its `Credentials`, addresses, blocks
and follows and revokes its sessions, so a deletion that loses a race
leaves the account whole (see Erasure). Concurrent writes are caught optimistically: `Save` fails with `ErrConflict`
when the aggregate changed since it was read, and `write_id` (below) is the
column that detects it. A use case that loses the race answers
`apperr.ErrTryAgain` (409 `try_again`) in every module.

### Aggregates in account

Module account holds who a person is and how they sign in. Module auth was
merged into it on 2026-11-08, so a sign-up writes the account and its
credentials in one transaction.

- `Account`: display name, bio, avatar, status, suspension.
- `Address`: one address of an account's address book.
- `Credentials`: the phone and email identities of one account. It holds
  the rules that an account keeps at least one identity and at most one of
  each kind.
- `Session`: one signed-in device. It is separate because sessions are
  unbounded and every refresh writes one.

`UNIQUE (kind, value)` on identities spans accounts, so the database
constraint holds it.

### Durable flows

Only flows that need durability run as Restate services. Other use cases are
called directly from HTTP handlers with `context.Context`, so reads and
refresh keep working when Restate is down.

- The client's `Idempotency-Key` is the idempotency key of the invocation. A
  repeated key returns the first result and runs no step again.
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
  outcome that changes nothing, such as a lost race on `last_used_at`. A
  step that calls another module's service, such as media's `Attach`,
  returns `restatex.AsRefusal(err)`: a `restatex.Refused` holding any
  `*apperr.Error` but an internal one, and every other error to retry. A
  step that checks the caller's account returns
  `accountsvc.Service.Suspended`, a bool, and the flow refuses with
  `account_suspended` between steps.
- A service takes `Now func() time.Time`, read once per invocation through a
  journaled step (`restatex.Now`), so tests move time by hand.
- Sign-up writes two aggregates, `Account` and `Credentials`, in one step:
  `Credentials.SignUp` inserts both in one transaction. When it returns
  `ErrIdentityTaken`, because another sign-up took the identity between the lookup
  and the insert, nothing was written and the client gets `try_again`.
  Restate retries every other failure, so no step ends the run with a
  terminal error.
- Every step inside `restate.Run` is idempotent. IDs are minted before the
  step by `restatex.NewID(ctx, at)`, a UUIDv7 built from the journaled clock
  and the invocation's deterministic random generator (`restate.UUID` gives
  version 4, which would break the spec's time-ordered cursors). Inserts use
  `ON CONFLICT (id) DO NOTHING`. The same ID is the idempotency key sent to a
  payment provider.
- Module account's Restate services, `SignIn`, `EmailCodeSend` and
  `AddIdentity`, live in `account/service/*_durable.go`. The `Account`
  methods in front of them call them through `restatex.Caller`, and
  `Account.Durable` returns their definitions for `account.Mount`.

`ListingEmbed` is the repo's first Virtual Object, keyed by listing ID, with
one exclusive handler, `Refresh`, which takes no input. Restate runs one
`Refresh` per listing at a time, and a send queues behind the run in
progress, so a burst of edits gives runs that each read the listing as it is
then: the first embeds the newest content and the others find their hashes
and photo keys in line. A run has a step that plans, a step for the text
vector when its hash moved, one step per photo without a vector, and a step
that drops the vectors of removed photos. No step returns a vector to the
journal. `ListingPublish` sends `Refresh` once a listing goes on sale, and
`ListingEdit` sends it after an edit that keeps the listing out of
processing. A refusal from the model (`embed.ErrRefused`) is logged and
skipped; any other error is retried by Restate.

### Events

A module announces a change with an event, and modules that react
subscribe to its kind; the module that changed data never lists who
reacts. Every subscriber is a module of server, so there is no broker.

- `events.Event` is thin: a `Kind` constant in the form
  `aggregate.past_tense` (`account.updated`, `account.suspended`,
  `account.unsuspended`), the `SubjectID` of the aggregate, and a `Payload`
  map that only a business event fills. A subscriber reads the subject's
  current state through its module's `Service`, so order, delay and
  duplicates do not change the result.
- A repository writes the events of a change with `events.Insert(ctx, tx,
  evs...)` in the change's transaction, as it writes an audit entry:
  `Save(ctx, a, writeID, entry, evs...)`, where `entry` is a
  `*audit.Entry` or nil. A conflicting write leaves no event. An event with
  an ID that exists already is not written again, so a durable step that
  retries its save with the event IDs from its journal writes one event.
  `Insert` also runs `pg_notify('events', '')` once per call, which
  Postgres delivers on commit; one notification wakes the bus for every
  event of the call.
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
    repository.go      package doc, ErrNotFound, ErrConflict
    account.go         interface Accounts
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

Modules console, media, catalog, deal, chat, trust, discovery and support
have the same four packages; module agent has them and `mcp`, its MCP
server (see Console agent); a module that reacts to events adds `worker`,
as catalog, chat, deal, discovery and notification do. Module `screen` has no
`repository` or `domain`: it only reads the other modules' services (see
Screen). Module `erasure` has only `service` and `handler`: it calls
account and deal to delete an account (see Erasure). Module `beacon` has
only `service` and `handler` too (see Client events).

The modules follow the product model in `app/mohinh.md`, not the old
server's split (decided 2026-10-26). There is no finance module: ShopNexus
holds no money, so there is no wallet and no payment session. Planned
next: `billing` (boosts and seller plans); `notification` holds the
in-app list and push (see Notifications). There
is no `kyc` module: a seller's verified phone number is enough, since a
carrier checks a seller's identity when it opens their account (decided
2026-10-27). Reports are in `trust` (see Trust); incident records will
join it. Reputation has no module or table: screen's reputation view reads
the figures from the modules that hold them (see Screen). Followed
searches, price alerts,
"Dành cho bạn" and vector search are in `discovery`, beside search and
the nearby feed. Imports run one way, `handler` and `worker` →
`service` → `repository` → `domain`, and depguard rules in `.golangci.yml`
refuse an import against it.

An entity keeps its fields unexported, so a repository in another package
rebuilds one through `Restore…` (`RestoreAccount`, `RestoreCredentials`; a
member through `domain.MemberState`) and records a write with
`Saved(writeID)`; only repositories call either.

One binary serves the HTTP API on `:8080` and the Restate endpoint on `:9080`.
A module's `Mount` takes `module.Deps` (router, pool, Restate ingress and
service prefix, event bus, clock, logger, and the `Protect` and `Identify`
middlewares), then the module's `Settings` struct when it has settings of
its own (account, catalog, console, media, discovery, notification), then the
services of the modules it calls, one argument each. It builds the
repositories, the service and the handler, registers the routes, and
returns what app and other modules need: a module with Restate services
returns `module.Mounted[service.Service]`, its `Service` and those services;
trust returns its `Service`; console returns `console.Mounted`, its
`Service` and its moderator guard; agent returns `*agent.Mounted`, whose
`Run` app starts (see Console agent); erasure and screen return nothing.
`app.mountModules` calls each `Mount` in dependency order and imports no
package inside a module. `account.Guards` builds `Protect`, which requires
a signed-in caller, and `Identify`, which lets a signed-out request through,
from the auth keys alone, so app builds them once, before any module, and
puts them in `module.Deps`. `console.NewService` builds the console service
on a pool; `console.Mount`, `cmd/console` and the e2e tests share it, and
`cmd/console` passes no account service. The commands that need the whole
configuration read it with `config.FromDotEnv(".env")`.

`app.New` picks the push sender once: the one a test passes, FCM when
`FCM_CREDENTIALS` is set, else the log sender, which `APP_ENV=prod` refuses.
The event bus, the stream hub and the agent's MCP server run on the app's
`sync.WaitGroup`; `App.Close` cancels them and waits for all three.

Tests of other modules create accounts through `internal/account/accounttest`
(`New`, `WithPhone`), beside `pgtest` and `restatetest`. `accounttest.Service`
builds the account service and `accounttest.Suspend` suspends an account.
`internal/catalog/catalogtest` builds the catalog service (`Service`) and puts
the standard listing, a used bicycle on sale, in the database (`Listing`).
`internal/chat/chattest.Pair` opens a pair's conversation, which a deal
row needs.
`internal/media/mediatest` builds the media service (`Service`), encodes a
JPEG (`JPEG`) and uploads it (`Upload`). `pgtest.EventKinds` lists the kinds
of the events about a subject, `restatetest.Ingress` is a client of the test
Restate's ingress, and `testenv.Eventually` polls a check until it returns
nil and fails the test with its last error.

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
| `Upload` | `uploads` | module media; status, content type, size, the processed keys |
| `Listing` | `listings` | module catalog; photos and specs are `jsonb` columns, read and written with the row |
| `ListingDraft` | `listing_drafts` | module catalog; `photos uuid[]`, `body text` the app owns; no `write_id`: the last write wins |
| `Address` | `addresses` | module account; one row per address, `write_id`; `recipient_name` keeps the keyword `name` out of the columns |
| feedback | `feedback` | module support; one row per feedback, `request_key` unique per account; no aggregate, no `write_id` |
| search history | `search_history` | module discovery; one row per folded query of an account, at most 10; no aggregate, no `write_id` |
| listing views | `listing_view_marks`, `listing_view_days` | module discovery; a person's last day of opening each listing, and the views per listing and UTC day; no aggregate, no `write_id` |
| notification settings | `notification_settings` | module notification; one row per account that saved settings; no aggregate, no `write_id`: the last `PUT` wins |
| quick replies | `quick_replies` | module chat; one row per account that saved its list, `items text[]` in order; no aggregate, no `write_id`: the last `PUT` wins |
| reply samples | `reply_samples` | module chat; one row per answer that ended a wait, kept 30 days; no aggregate, no `write_id` |
| helpful marks | `review_helpful` | module trust; one row per person and review; no aggregate, no `write_id` |
| agent reads | `console_agent_reads` | module agent; one row per read of personal data by an MCP tool, kept 90 days; no aggregate, no `write_id` |
| agent runs | `agent_conversations`, `agent_turns`, `agent_turn_inputs`, `agent_tool_calls`, `agent_turn_events`, `agent_documents`, `agent_document_versions`, `agent_skills` | module agent; the console agent's conversations and what their runs wrote; no aggregate, no `write_id`: a turn's status changes under its row lock or a `WHERE status` check |
| AI agents | `ai_agents`, `ai_agent_versions` | module agent; `ai_agents` adds `write_id`, and a save adds the version of new instructions (see AI settings) |
| model tags | `ai_tags`, `ai_model_tags` | module agent; no aggregate, no `write_id`: setting a model's tags replaces them |

No column is named after an SQL keyword (sqlfluff RF04, run with
`sqlfluff lint migrations/`): `events.occurred_at` and `payload`,
`console_actions.action_kind` and `acted_at`, `identities.identifier`,
`console_login_locks.email_hash`,
`console_members.console_role`, `console_agent_reads.tool_name` and
`read_at`, `ai_tags.tag_name`, `tag_kind` and `sort_order`,
`ai_agents.agent_kind`, `display_name` and `model_alias`,
`ai_agent_versions.version_no`, and `display_name` for categories, provinces and communes. Go fields and JSON keep their own names where an API already
published them (`role`, `value`, `action`, `at`). `.sqlfluffignore` skips
the generated `0005_regions.sql`, which is past the parser's node limit.

`cmd/migrate`, which compose's migrate service and the cluster's migrate
hook run, applies every file whose version `schema_migrations` lacks, in
version order, each in its own transaction, and stops when an applied
file's sha256 changed. A gap in the numbers is
fine, and so is a lower version that lands after a higher one, as when
two branches number apart and merge in either order; only a version two
files share is refused.

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

### Idempotency-Key

Every endpoint that invokes a Restate service requires an `Idempotency-Key`
header (a UUID) and passes it as the invocation's idempotency key. A repeated
key attaches to the first run and returns its result, so a retried tap gets
the same answer. `PATCH /v1/accounts/me` with an avatar is the exception: it
calls `MediaProcess` keyed by the upload ID, which already makes a repeat
attach to the first run. `POST /v1/deals/{id}/cod` and
`POST /v1/orders/{order_id}/cancel` take the header as optional (decided
2026-11-19), since app versions from before send none: with a key they
run once per caller and key, and without one every request runs its own
invocation. Sign-in puts the verified number in the key and
listing posts put the seller, so a key sent with another number's token or
by another seller runs apart. A reauthentication puts the caller's session
and the proven address in it.

### Sign-in

A person signs in, or signs up on first use, with one of three proofs sent
to `POST /v1/auth/sign-in` (decided 2026-11-10, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-sign-in-email-google-design.md`,
which reverses the email removal of the 2026-10-28 spec):

- `phone_token`, which proves a phone number: an ID token of Firebase
  Authentication after an SMS code, or a Phone Number Verification (PNV)
  token from the SIM.
- `google_token`, which proves an email address: a Google ID token the app
  gets from Android Credential Manager.
- `email_code`, which proves an email address: a challenge ID and the
  six-digit code server mailed to it.

The OpenAPI `oneOf` admits exactly one of them. An account holds a phone,
an email or both, one identity of each kind, and a value belongs to one
account. Google is no kind of its own: a Google token for an address that
is an email identity signs in to that account.

1. `Account.SignIn`, the use case behind the HTTP handler, resolves the
   proof outside Restate, whose journal and invocation results the admin
   API can read. A token goes through `provider/firebase.PhoneVerifier` or
   `provider/google.Verifier` and becomes a proven address. An email code
   becomes a `CodeGuess`: the challenge ID and `HMAC(OTP_KEY, address,
   code)`, never the code.
2. It calls Restate service `SignIn`, handler `Run`, with `ProvenSignIn`.
   The invocation key is `<kind>:<value>/<Idempotency-Key>` for a token
   and `<challenge_id>/<Idempotency-Key>` for a code, so a key sent again
   with another person's proof runs apart.
3. For a code, `judgeGuess` reads the challenge in one step and saves the
   counted guess in the next. A challenge that is unknown, not the newest
   for its address and purpose, of purpose `add_identity`, expired or out
   of guesses refuses; a lost race on the save is `try_again`.
4. A step finds the identity by kind and value.
   - Found: a step reads the account status (suspended ends the run), and
     the next step marks the identity used, ignoring a lost race.
   - Not found: create the `Account` with
     `accountID := restatex.NewID(ctx, at)`, then the `Credentials` with the
     identity, in one step and one transaction. `ErrIdentityTaken` writes
     nothing (see Durable flows).
5. `Account.SignIn` issues the session after the Restate service returns,
   with the session ID the service chose. Restate answers a repeated key
   with the first result whatever the request carries, so for a code the
   use case also compares the request's hash with the challenge's and
   answers `code_expired` when they differ. When the same key comes back
   with a valid proof, that session exists, so `SignIn` rotates its
   refresh token and returns new tokens for it; a session signed out since
   answers `session_revoked`.

The phone token checks live in `internal/provider/firebase`:

- `iss`, read before the signature, picks the key set, and the token must
  then carry that issuer's algorithm and audience. Firebase Authentication:
  RS256, `iss` `https://securetoken.google.com/<project id>`, `aud` the
  project ID, `firebase.sign_in_provider` `phone`, the number in
  `phone_number`. PNV: ES256 with `typ` `JWT`,
  `iss` `https://fpnv.googleapis.com/projects/<project number>`, `aud`
  holding `https://fpnv.googleapis.com/projects/<project id>`, the number in
  `sub`.
- `exp` is required, and the verification must be at most 5 minutes old:
  `auth_time` for Firebase Authentication, which a refreshed token keeps,
  and `iat` for PNV. Clocks may differ by 30 seconds. Within those limits a
  token signs in again; the user chose that over single use on 2026-10-28.
- A failed check is 401 `invalid_phone_token` on sign-in, and the log
  names the check. A number outside Vietnam is 422 `invalid_phone`.
- PNV test numbers (`test_number: true`, the country code and zeros) pass
  only with `PNV_TEST_NUMBERS=true` and are stored as they come.
  `APP_ENV=prod` refuses to start with `PNV_TEST_NUMBERS=true`.
- Firebase Authentication keeps a user record per SMS sign-in; server
  leaves it, and the app signs out of Firebase once it has the token.
- `FIREBASE_PROJECT_ID` and `FIREBASE_PROJECT_NUMBER` name the project;
  push uses the same project ID.

The Google token checks live in `internal/provider/google`: RS256, `iss`
`accounts.google.com` with or without `https://`, `aud` `GOOGLE_CLIENT_ID`
(the Web client the app passes to Credential Manager), `exp` required,
`iat` at most 5 minutes old, `email_verified` true, and `email` through
`contact.ParseEmail`. A failed check is 401 `invalid_google_token` on
sign-in. An empty `GOOGLE_CLIENT_ID` turns Google sign-in off: every token
is refused.

On `addMyIdentity` and `reauthenticateSession`, which carry a bearer, a
token that fails a check is 422 with the same code, as a wrong email code
is (decided 2026-11-17). A client takes a 401 on a bearer call for an
expired or revoked bearer: the app's SessionAuth refreshed, retried once
and then signed the person out over a bad proof. On those calls a 401 is
about the bearer alone: `unauthenticated`, `invalid_token`,
`session_revoked`, and `session_expired` on reauthentication.
`Account.verifyToken` takes the kind each use case answers a refused token
with.

Both providers fetch keys through `internal/provider/jwks`, one `Set` per
URL (`FIREBASE_AUTH_JWKS_URL`, `PNV_JWKS_URL`, `GOOGLE_JWKS_URL`). Keys are
cached for the response's `max-age`. An unknown `kid`, an expired cache or
no keys fetch again, at most once a minute, under one mutex. A failed fetch
keeps the keys held; with none ever fetched the endpoint answers 503
`unavailable`, so the app retries instead of asking for a new code.

Email codes:

- `POST /v1/auth/email-codes {email}` mails a sign-in code;
  `POST /v1/accounts/me/email-codes {email}`, signed in, mails an
  `add_identity` code bound to the caller's account. Both answer
  `{challenge_id, expires_at, resend_at}`, the same whether or not an
  account holds the address.
- Restate service `EmailCodeSend` admits the client's quota (twenty an
  hour, an IPv6 client by its /64), then the address's (one per 45 seconds,
  five an hour), creates the challenge and mails the code in a step. The
  code is derived from the challenge ID with `OTP_KEY`, so a retried send
  step mails the same code and no code is stored.
- A code lasts 5 minutes and takes 5 guesses: `invalid_code` with
  `attempts_left`, then `code_locked`; `code_expired` once it expires or a
  newer code to the same address replaces it.
- `EMAIL_SENDER` is `log` or `resend` (`RESEND_API_KEY`, `EMAIL_FROM`).
  `APP_ENV=prod` refuses the log sender. `provider/email.Sender` takes one
  `Message` (to, subject, text, key); Resend gets the key as its
  `Idempotency-Key`, so a retried step mails once. A code's key is its
  challenge ID.
- `login_total{method}` counts `phone`, `google` and `email`;
  `otp_sent_total` counts mailed codes.

### Adding an identity

`POST /v1/accounts/me/identities` takes a proof of the kind the account
lacks: a phone token for an email-only account, a Google token or an
`add_identity` code for a phone-only one. Restate service `AddIdentity`,
key `<account_id>/<Idempotency-Key>`, judges a code with `judgeGuess`
(the challenge must be of this account), reads the `Credentials` in a step,
`Add`s the identity between steps and saves it in the next. A kind the
account holds is 409 `identity_kind_taken`; a value another account holds
is 409 `identity_taken`; a lost race is `try_again`. Accounts are never
merged, and no endpoint changes or removes an identity, so adding one and
mailing the code that adds an email (`POST /v1/accounts/me/email-codes`)
need a sign-in within `domain.RecentAuth` (428 `reauth_required`) and an
account that may write (403 `account_suspended`, 401 `session_revoked`);
an identity added with a leaked access token would otherwise let its
holder in for good (decided 2026-11-10, after the branch's review).
`GET /v1/accounts/me` answers `MyAccount`, the account with its
`identities`, and `session_revoked` for a deleted account; `Account`, the
public view other modules return, has none.

### Reauthentication

`POST /v1/auth/sessions/current/reauthenticate` is the app's "Xác nhận lại
là bạn" after a 428 `reauth_required` (decided 2026-11-17). The app used to
call `POST /v1/auth/sign-in` again and read the new `sid` out of the JWT;
a Google account or an email address that no account held then signed up
an empty account.

- The body is `ProofRequest` in `api/schemas/auth.yaml`: `phone_token`,
  `google_token` or `email_code` (`EmailCodeProof`), a `oneOf` of
  `required` only, and `additionalProperties: false`. `addMyIdentity` takes
  it too. `SignInRequest` repeats the three properties beside
  `device_name`. A shared schema under `allOf` would need
  `unevaluatedProperties: false` to stay closed, and kin-openapi checks a
  schema with that keyword with its JSON Schema 2020-12 compiler, which
  cannot follow the spec's file refs such as `#/EmailCodeProof`: every
  sign-in failed with 400 when it was tried.
- `Account.Reauthenticate` loads the account (`CanWrite`: 403
  `account_suspended`, 401 `session_revoked` once deleted) and the caller's
  session (`Usable`: 401 `session_revoked` or `session_expired`). It checks
  a token outside Restate as `SignIn` does, or hashes a code into a
  `CodeGuess`.
- Restate service `Reauthenticate` runs under the key
  `<session_id>/<kind>:<value>/<Idempotency-Key>`, or
  `<session_id>/<challenge_id>/<Idempotency-Key>` for a code. It judges a
  code with `judgeGuess` for purpose `sign_in`, so the code comes from
  `POST /v1/auth/email-codes` with that endpoint's quotas and five guesses.
  Then `findIdentity`, the step `SignIn` also runs, looks the address up.
  An identity of another account and an address no identity holds both
  answer 403 `wrong_account`, so the answer does not tell whether an
  address is registered. The service writes only the counted guess and
  returns `at`, the time it checked the proof. A repeated key with another
  code is `code_expired`, as for sign-in.
- `Sessions.Reauthenticate` sets `auth_time = greatest(auth_time, at)` in
  one statement that keeps `write_id`, on the session if it is not revoked
  and at `at` neither past `SessionMaxAge` nor idle past
  `SessionIdleLimit`. No `Save` writes `auth_time`, so neither write can
  undo the other. A new `write_id` would make a refresh that read the
  session before it conflict, and Refresh treats that conflict as a reused
  token and revokes the session. `Save` returns the row's `auth_time`
  instead, so a refresh that read the session before a reauthentication
  signs with the new time. A session that the update misses is
  `session_expired` when `Usable(at)` fails on the session as first read,
  which covers one that ended while the code was judged, and
  `session_revoked` otherwise. A repeated key writes the first run's `at`
  again, which does not move `auth_time`.
- The reply is `AccessToken`, an access token for the same session signed
  with the new `auth_time`. The middleware reads `auth_time` from the
  access token and from no database (see Session use cases), so the app
  replaces its access token with this one. The refresh token stays: it
  carries no sign-in time, and rotating it would leave an app that lost
  the reply holding a token whose hash is now `prev_refresh_hash`, which
  signs the session out as a reuse. A refresh signs its access token with
  the `auth_time` of the row, the new one included.

Posting a listing, opening a deal and opening a chat need a phone identity:
`accountsvc.Service.HasPhone`, read by `ListingPost` in a step after the
suspension check, by `Deals.Open` and by `MessageSend`'s facts step when
it opens, refuses with 403 `phone_required`. Replying in a conversation,
`RequestCod` and everything else need none. An address's phone is not an
identity and is not verified.

### Session use cases

Refresh, sign-out and the device list are plain `Account` methods against the
`Session` aggregate; none of them goes through Restate.

The device list, the sign-outs and the reauthentication are one resource,
`/v1/auth/sessions`:

| Endpoint | Method | Does |
|---|---|---|
| `GET /v1/auth/sessions` | `ListSessions` | lists the devices |
| `DELETE /v1/auth/sessions/current` | `DeleteCurrentSession` | signs out the session of the access token |
| `POST /v1/auth/sessions/current/reauthenticate` | `Reauthenticate` | renews the sign-in time of the session of the access token after a proof of the same account; returns an access token (see Reauthentication) |
| `DELETE /v1/auth/sessions/others` | `DeleteOtherSessions` | signs out every session but the caller's; needs a sign-in within 15 minutes (428 `reauth_required`) |
| `DELETE /v1/auth/sessions/{id}` | `DeleteSession` | signs out one device |

`DELETE /v1/auth/sessions/current` replaced `POST /v1/auth/logout` on
2026-11-10. `DELETE /v1/auth/sessions`, which replaced
`POST /v1/auth/logout-all` and signed out the caller's session too, became
`DELETE /v1/auth/sessions/others` on 2026-11-17: the app's row is "Đăng
xuất các thiết bị khác", and the caller's own device stays signed in.
`current` and `others` are fixed segments. ServeMux and kin-openapi's
router, which the request validator uses, both match a fixed segment before
`{id}`; `TestFixedSessionRoutes` in `account/handler` holds both to it.

A revoked session keeps in `revoke_reason` the reason of its first
revocation: `sign_out`, `reuse`, `suspended` or `deleted`; sessions revoked
before 2026-11-17 have none.

Refresh:

1. Find the session by the hash of the presented token. When none has it as
   its current hash but one has it as the previous hash, the token was
   replayed: `Revoke`, `Save`, return `session_revoked`. When nothing matches,
   return `invalid_refresh_token`.
2. `s.Usable(now)`, then the account's status at `now`. A
   suspended account gets its session revoked for `suspended` and
   `account_suspended`; a deleted one's for `deleted` and
   `session_revoked`. A session revoked for `suspended` answers
   `account_suspended` again while the suspension lasts, so a client that
   lost the first answer learns of the suspension on its retry, and
   `session_revoked` once it ends. Only the holder of the session's current
   refresh token gets that answer; every other revoked session answers
   `session_revoked`.
3. `token := s.Rotate(now)`, `Save`. On `ErrConflict` the use case reloads
   the session. Revoked means a sign-out won the race, and the answer is
   `session_revoked`. Otherwise a concurrent refresh with the same token won:
   the use case revokes, logs the reuse and returns `session_revoked`.
4. Sign the access token with the `auth_time` the save returned, which a
   reauthentication may have moved after step 1.

`DeleteCurrentSession` and `DeleteSession` load the session, `Revoke` and
`Save`, retrying once on `ErrConflict`. `DeleteSession` first checks that the
caller owns the session (another account's session is 404).

Two kinds of call skip the aggregate:

- A read returns view structs from a query and never loads an entity. The
  device list is `sessions.ListActive(accountID, now, cursor, limit)`.
- A bulk change that only moves a one-way flag may run as one set-based
  statement that also changes each row's `write_id`. `DeleteOtherSessions`
  is `UPDATE sessions SET revoked_at = @now, revoke_reason = 'sign_out',
  write_id = uuidv7() WHERE account_id = @account_id AND id <> @keep AND
  revoked_at IS NULL`. Loading and saving each session would cost one
  transaction per device and protect no rule. The push tokens of the
  sessions it ends stop at the next push (see Notifications).
- `Sessions.Reauthenticate` moves one session's `auth_time` forward in one
  statement and keeps its `write_id` (see Reauthentication).

The access token is checked by signature and expiry only; the middleware reads
no database. `account/domain` signs and verifies it (EdDSA, `kid` header,
the previous key accepted during a key change) and requires `sid` and
`auth_time`; `platform/token` only makes opaque tokens and their hashes.

### Console and authorization

Moderators and admins run the web console as console members, on accounts
of their own (module `console`), separate from customer accounts.
`~/shopnexus/docs/superpowers/specs/2026-10-24-server-staff-authorization-design.md` holds
the decisions and the API.

- Module `console` was module `staff` until 2026-11-08 (spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-08-console-rename-design.md`).
  The tool is "console" and the person signed in to it is a "member", in
  routes, tables, cookies, configuration and audit kinds. Migrations 0003
  and 0023 were edited in place; every database was dropped once.

- Every sign-in call is `POST /v1/console/login` with the email and the
  password. The server reads the member and answers 428 with what the call
  lacks: `password_change_required` (send `new_password`),
  `totp_enrollment_required` (call `/v1/console/login/totp-enrollment`, which
  takes the email and password too, then send the first code) or
  `totp_required` (send `code`). An unknown email, a wrong password and a
  disabled member all get `invalid_credentials`.
- Each call is charged to the email's lock in `console_login_locks` before
  anything is checked, in one statement, so concurrent guesses cannot pass
  the check together. A wrong password or code keeps the charge; any other
  answer refunds it. Five kept charges lock the email for 15 minutes,
  whether or not the email is a member's. The lock is keyed by an HMAC of
  the email, and a finished sign-in clears the count.
- A finished sign-in sets the `console_session` cookie: a random token,
  `HttpOnly; Secure; SameSite=Strict`, path `/v1/console`, 8 hours, never
  extended. `console_sessions` holds its SHA-256. There is no access token and
  no refresh, so the console's tabs share one cookie.
- Every console route requires `X-Requested-With: shopnexus-console`, which a
  form or link on another site cannot set. The spec declares it and
  `handler.FromConsole` checks it again. With SameSite=Strict, the console
  and the API must be on the same site (`console.x.vn` and `api.x.vn`).
- `handler.Require(..., floor)` calls `Console.Authenticate`, which reads the
  session joined with its member on every request. A session lives while
  it is unrevoked, younger than 8 hours, its member is active, and its
  credentials version equals the member's. A password change, a password
  or TOTP reset and a disable raise the version, which ends every session
  of the member, the caller's included; enabling a member brings none back.
- Admins exist only through `cmd/console`. The API manages moderators, and
  `Member.ManagedBy` refuses an admin target to anyone but the CLI.
  `cmd/console unlock` clears the sign-in lock on an admin's email, which
  anyone can set by sending five wrong passwords.
- Every member's write on an account or on another member is a row in
  `console_actions`, written by `audit.Insert` inside the repository
  transaction of the change (`Save` with an entry, `Create`, `Unlock`). Its
  action is an `audit.ActionKind` constant (`audit.AccountSuspend`,
  `audit.MemberDisable`, ...), the list the `Action` schema's enum names; a
  new action adds both. `details` holds what the action does not say, such
  as a suspension's reason and end.
  Refused sign-ins go to the log as `security: console sign-in refused` with
  the email and IP; finished ones are rows in `console_sessions`.
- TOTP secrets are sealed with AES-GCM under a key HKDF derives from
  `CONSOLE_KEY`, bound to the member's id. Passwords are argon2id
  (64 MiB, 3 passes, 4 lanes) in PHC form, at most four runs at once.
- Module console reaches customer data through `accountsvc.Service`. CORS allows
  `CONSOLE_ORIGIN` on `/v1/console/`, with credentials. The docs site's API
  playground calls from the origins in `DOCS_ORIGINS`, on `/v1/` outside
  `/v1/console/` and on the health checks, without credentials: the
  customer API takes a bearer token, and the console cookie never leaves
  its origin.

### Traces and metrics

Every call this process makes to another service goes through
`httpx.NewClient`, or `httpx.Transport` for a client that brings its own
transport (minio). It opens a client span named by method and host, sends
`traceparent` on, and records `http.client.request.duration` by host, method
and status, or `error.type` when no answer came. Providers take the client
from the wiring in `app` and `agent`, and none builds a plain `http.Client`.
The Restate ingress client is one of them, so Restate carries the request's
trace to the handler it starts.

`restatex.Serve` opens a server span per handler attempt, named
`Service/Handler`, and records `restate.invocation.duration`. The SDK reads
the parent span from `traceparent`, so the endpoint writes its own span
there before the SDK runs. A retried or suspended handler counts once per
attempt. Durations use `telemetry.Seconds`, the semantic conventions'
buckets from 5 ms to 10 s (decided 2026-11-23).

### Inventory gauges

Modules account, catalog, deal, chat, trust and support export gauges of
what the marketplace holds now, for the Grafana dashboard (decided
2026-11-23). Each module counts its own rows through its repository and
registers the gauges in its `Mount` (`gauges.go`), so a SERVER_ROLE=ai
process has none. `telemetry.CountGauges` runs a gauge's query at most
once per `telemetry.CountEvery`, a minute; the exporter collects every
10 s and repeats the last answer in between. A failed query keeps the old
answer and logs one warning until a query succeeds again. A labelled
gauge has one series per value of its fixed set, 0 included.

- `account.accounts` by status (active, suspended) and
  `account.accounts.phone_verified`; deleted accounts and ShopNexus's own
  are left out.
- `catalog.listings` by status (processing, active, hidden, rejected,
  reserved, sold) and `catalog.listing_drafts`, the drafts not expired.
- `deal.deals` by status (negotiating, agreed, sold) and `deal.orders` by
  status (pending, shipped, completed, failed, cancelled).
- `chat.conversations` by kind (pair, support). Messages have no index on
  `created_at`, so there is no gauge of recent messages.
- `trust.reports.open`, `trust.report_cases.open` and `trust.reviews` by
  role (seller, buyer), deleted reviews left out.
- `support.tickets.open`.

Every api process exports the same numbers under its own
`service.instance.id`, so a panel takes `max` over instances. On the
development data (110k listings) all queries together take about 100 ms;
the listings count reads only the index `listings_seller`.

### Client events

Module `beacon` serves `POST /v1/client-events`, open to anyone: a batch of
up to 50 events from the website or the Android app. It turns them into
metrics and log lines and keeps no rows (decided 2026-11-23).
Its rate limit tier, `telemetry`, lets one client address send 2 batches a
second in bursts of 60 (see Rate limits). The website's server forwards the
buyer's address, so its beacons count per buyer.

- `error`, a script error the website did not catch: counter
  `client.errors` by app, and a log line `client error` with the page,
  message and stack. The app sends its crashes to Crashlytics, because its
  release build is minified and Crashlytics retraces it.
- `vital`, a Core Web Vital: `client.web_vital.duration` (LCP, INP, FCP and
  TTFB, in seconds) or `client.web_vital.cls`, by app and name. The page is
  not a label.
- `request`, an API call as the client timed it: `client.request.duration`
  by app, route, method, status and network. The server reads the route
  from the path through `httpx.Router.Route`, so a client cannot add
  label values; a path no route serves is `other`. A call with no answer,
  a 5xx or one of 3 s or more also gets a log line `client request` with
  the `Trace-Id` the response carried, which names the server's trace.
  The app sends a `traceparent` of its own on every call and reports its
  ids and start time; the server then exports the app's span itself
  (`telemetry.RecordClientSpan`, service `android`, the app's trace and
  span ids), so the call's trace has its root and shows the network time.
  The app exports no spans and Tempo takes none from outside.
- `search_impression` and `search_click`: counters
  `client.search.impressions` and `client.search.clicks` by app and
  position (1, 2, 3, 4-5, 6-10, 11-20, 21+), and log lines with the
  `search_id`. Every `GET /v1/search` response carries a fresh
  `search_id`, and its `search terms` line logs it beside the query, so
  Loki joins a click to its query.

### Ops dashboards

Grafana, Restate's UI, LiteLLM's UI, RustFS's console, Argo CD and pgweb
(app `db`, on the stack's database, added 2026-11-17) open from the
console on hosts of their own (decided 2026-11-05, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-05-ops-dashboards-design.md`).
Grafana takes a moderator; the others take an admin.

- `POST /v1/console/dashboard-tickets {app}` gives the console's session a
  ticket (`dt_` and 32 random bytes) that opens the app once within 60
  seconds, with `callback_url` on the app's host.
- `GET /_dashboard/callback?ticket=&rd=` runs on the app's host, which
  `DASHBOARD_HOSTS` (`grafana=grafana.x.vn,...`) maps to the app. One
  statement marks the ticket used and inserts the dashboard session, so a
  second callback with the same ticket finds it used. The cookie
  `console_dashboard` is host-only (`Path=/`, no `Domain`), `SameSite=Lax`
  so the redirect from the console carries it, and lasts what the console
  session has left. The redirect follows `rd` only when it starts with one
  `/` and holds no backslash or control character; otherwise it goes to `/`.
- `GET /v1/console/dashboard-auth` is Traefik's forwardAuth target. It reads
  the app from `X-Forwarded-Host` and answers 200 with `X-Console-Email` and
  `X-Console-Role` (Grafana's `Admin` or `Viewer` there), 302 to the console's
  `/dashboards/open` for a GET without a session, 401 for other methods and
  403 below the app's floor or on a host `DASHBOARD_HOSTS` does not name.
- `console_dashboard_tickets` and `console_dashboard_sessions` hold token
  hashes and the console session's ID, with `ON DELETE CASCADE`. A sign-out
  revokes the console session and deletes nothing, so every dashboard check
  joins the console session and its member with the same rules as
  `Authenticate` (`sessionLives` in `session_pg.go`). A new ticket deletes
  its session's expired tickets.
- `DASHBOARD_HOSTS` is required and may be empty, which refuses every
  dashboard. `config.DashboardApps` lists the apps it may name.
- Each app's floor is a row of `console_dashboards` (migration 0057, seeded
  with `domain.App.Floor`'s defaults: Grafana moderator, the others
  admin). The ticket and the forwardAuth check read the row on every
  request, so a floor an admin raises refuses a moderator's open
  dashboard session on its next request. `GET` and `PATCH
  /v1/console/settings/dashboards {items: [{app, floor}]}` are admin only;
  each change is a `dashboard.floor` console action on target
  `dashboard`. `GET /v1/console/me` carries the same list as
  `dashboards`, so a moderator's sidebar follows a changed floor.

### Object store

RustFS 1.0.4 through its S3 API, reached with minio-go
(`internal/platform/objectstore`). One bucket, `OBJECT_STORE_BUCKET`:
`uploads/<id>` is private and written only through presigned PUTs;
`media/` is public-read and holds processed photos. `Store.Prepare` creates
the bucket and the `media/*` read policy; `make run` calls it through
`cmd/objectstore-init`, and `objectstoretest.New` calls it once per test
process. The server reaches the store at `OBJECT_STORE_ENDPOINT` and signs
presigned URLs for `OBJECT_STORE_PUBLIC_ENDPOINT`, the address phones reach;
the signature covers the host, so a proxy cannot rewrite it. The bucket's
CORS lets the origins in `WEBSITE_ORIGINS` PUT, so the website's pages upload
from the browser; the same list says which origins may open `GET /v1/ws`
(see Stream). Configuration:
`OBJECT_STORE_ENDPOINT`, `OBJECT_STORE_PUBLIC_ENDPOINT`, `OBJECT_STORE_REGION`,
`OBJECT_STORE_ACCESS_KEY`, `OBJECT_STORE_SECRET_KEY`, `OBJECT_STORE_BUCKET`,
`WEBSITE_ORIGINS` and `MEDIA_PUBLIC_URL`, the base a public URL is built on. RustFS listens on
127.0.0.1:56490 in development and 56491 in tests; the test container keeps
`/data` on a tmpfs owned by uid 10001, the user the image runs as.

`Store.Get` stats the object before it returns the reader: minio's
`GetObject` sends nothing until the first read, so without the stat a
missing key surfaced only then. A missing key is `ErrNotFound`.

### Media

Module `media` owns uploads; catalog, chat and account (avatars) use
them. An `Upload` moves waiting → attached → ready or failed, or
waiting → expired; a video moves waiting → attached → consumed. A waiting
upload expires at `uploads.expires_at`: a day after it was created, later
once a listing draft extended it.

- `CreateUpload` (`POST /v1/uploads`) takes a content type and a size: JPEG,
  PNG or WebP up to 15 MB (the app converts HEIC), or MP4 or QuickTime video
  up to 50 MB. It returns a PUT URL valid for 15 minutes that signs both. Before inserting the row it sends
  `MediaExpire.Run(upload_id)` with a 24-hour delay, keyed by the upload ID.
  `MediaExpire` deletes an upload still waiting at its `expires_at`, and
  its object. While `expires_at` is still ahead (a draft extended it), it
  sends itself again for that moment, keyed `<upload id>/<expires_at Unix
  seconds>`, since the upload ID keyed the first run. A run less than a
  minute early sleeps out the rest instead, since a send keyed for its own
  moment would attach to it. A save that loses to another write reads the
  upload again and goes on, so the chain never ends while the upload waits.
- `Extend(owner, ids)` keeps photos for the owner's listing drafts
  (decided 2026-11-13), all or none: each is the owner's photo, waiting
  before `expires_at` and in the store as declared, otherwise
  `invalid_upload`. It sets `expires_at = max(expires_at, now + 30 days)`
  (`domain.ExtendFor`) and leaves the upload waiting, so `Attach` takes it
  for a listing. Nothing shortens an expiry, so no hold or release is
  needed: a photo two drafts name lives until the later one's end.
- `Gone(owner, ids)` names those of the owner's uploads that a holder took,
  that expired or wait past `expires_at`, and the IDs with no row.
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
  at once in the process (`processSlots`); the others wait for a slot. The
  decoding and encoding are `internal/media/imaging.Process`, which the seed
  pack builders call too (decided 2026-11-19). The photo's bytes never
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
- `Service.Photos(owner, holder, ids)` (decided 2026-11-20) does the same
  for photos a direct use case keeps, such as a review's: it attaches them
  as `photo` and calls `MediaProcess` for each at once, keyed by the upload
  ID, and returns their keys; a refused photo is `invalid_upload`.
- Another module's Restate flow processes its uploads with
  `mediasvc.ProcessAll(ctx, prefix, ids)`: one `MediaProcess` call per
  upload, keyed by the upload ID, all sent before it waits for the
  results. Catalog's `ListingPublish` and chat's `MessagePublish` use it.

Search reads photos through three more use cases (decided 2026-11-09):

- `QueryUpload(owner, id)` checks that an upload is the owner's, still
  waiting and before its expiry (`uploads.expires_at`, 24 hours after the
  upload unless a draft extended it); anything else is `invalid_upload`,
  422.
- `QueryImage(owner, id)` reads such an upload in the shape `ShapeQuery`: at
  most 768 px on its long side, upright, a JPEG without EXIF. It takes a
  processing slot, and nothing is stored or published.
- `ReadPhoto(key)` reads a processed photo under `media/`, for
  `ListingEmbed`.

Listing reads use four more (decided 2026-11-10 for the video read, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-video-read-design.md`):

- A video upload serves a video read only: `Attach`, `Square`,
  `QueryUpload` and `QueryImage` refuse it with `invalid_upload`.
  `HoldVideo(owner, holder, id)` gives it to a read: the owner's, waiting
  or already the holder's, before its expiry, in the store as declared. It
  records `shape = video`. `OpenVideo` reads the object for the read's
  flow. `Consume` deletes the object and sets status `consumed`; a second
  call changes nothing.
- `SavePhoto(owner, id, bytes, contentType, origin)` writes a photo the
  server made or copied to `uploads/<id>` and inserts a waiting upload of
  the owner, with its 24-hour `MediaExpire`: a video read's frame (`origin
  = frame`) or a link read's copy (`origin = link`). A listing attaches and
  processes it like a photo the app uploaded. The caller mints the ID
  before its step, so a retry writes the same object and inserts nothing.
  Origin `app` or a video type is `invalid_upload`.
- `PresignPhoto(id)` signs a GET of `uploads/<id>` valid for an hour
  (`Store.PresignGet`, for `OBJECT_STORE_PUBLIC_ENDPOINT`), for a read's
  view.
- `uploads.origin` is `app`, `frame` or `link` (decided 2026-11-11), or
  `listing` for a copy of a photo of the seller's own listing (decided
  2026-11-16, migration 0049). Its
  default `app` stays, for the replicas of the release before that still
  insert while a deploy rolls out.
  `MediaProcess`'s result carries `Copied` for origin `link`, and catalog
  keeps it with the photo's keys.

### Catalog

Module `catalog` holds listings, categories and regions. A listing is one
item at one price, C2C, cash on delivery. A seller's drafts are kept on
the server for their other devices (decided 2026-11-13, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-13-listing-drafts-sync-design.md`);
the posting rules apply only to the complete listing `POST /v1/listings`
receives, whose body is `ListingInput` (was `ListingDraft`; Go's
`domain.ListingInput`, was `domain.Draft`). The flows' `PostInput.Listing`
and `EditInput.Listing` keep the wire key `draft`, so old and new pods
agree during a deploy.

- `PUT /v1/listing-drafts/{id}` creates (201) or replaces (200) a draft;
  the app mints the ID, a UUIDv7, and the last write wins, with no
  revision. `GET /v1/listing-drafts` returns every draft in full, last
  written first, with no pages; `DELETE` answers 204, also for a draft
  that is gone or another account's. Another account's ID on a PUT is
  `draft_not_found`. `GET /v1/listing-drafts/{id}` (2026-11-19) reads one
  draft as the list does; another account's, an expired one or none is
  404 `draft_not_found`. It deletes nothing: an expired draft goes on
  the account's next list or PUT.
- An account keeps 20 drafts (`draft_limit_reached` on a 21st create),
  counted under `pg_advisory_xact_lock(hashtext('drafts:' || account))`
  with a share lock on the account's row; a deleted account's write is
  `session_revoked`, a suspended account still writes its drafts. A draft
  lives 30 days after its last write (`domain.DraftLife`), and the
  account's next list or PUT deletes it first, so it frees its place; no
  job and no notice.
- The server reads the typed columns (`title` up to 100 characters,
  `price`, `photos` up to 10 upload IDs) and keeps `body`, the app's JSON
  as a string of at most 32,768 characters laid out as `schema_version`
  says, without reading it. A repeated photo ID is kept once.
- A PUT drops the photos media's `Gone` names, extends the rest with
  `Extend`, then writes the row. Media and catalog share no transaction:
  an extension whose write then fails keeps a photo longer and nothing
  else. GET leaves gone photos out of its answer without writing, so a
  read never keeps a draft alive. Photo URLs are `PresignPhoto`, valid an
  hour; unposted photos never go under the public `media/`.

- A photo the server copied from the seller's listing on another site
  shows `copied: true` (decided 2026-11-11, spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-11-server-link-read-design.md`).
  Processing reads it from the upload's origin and stores it in the
  photo's keys in `listings.photos`, an edit keeps it, and every reader of
  the listing sees it. The server cannot tell a camera photo from a
  gallery one, so only copies are marked.
- A listing declares no marks (decided 2026-10-27, spec:
  `~/shopnexus/docs/superpowers/specs/2026-10-27-remove-declared-marks-design.md`). People
  judge wear on used goods differently, so a declared mark is not a
  reliable fact, and the buyer looks at the item at the door before
  paying. The condition (new, like new, used), the specs and the photos
  describe the item. Migration 0021 drops the `marks` column, and a draft
  that still sends `marks` is 400 `invalid_request`.
- A price is whole đồng, 10,000 to 1,000,000,000. The ceiling (decided
  2026-10-26) refuses a price typed with extra zeros and keeps a deal's
  amount arithmetic far from int64 overflow; migration 0009 adds it to the
  table's check.
- Status: processing → active or rejected; active ⇄ hidden; active or
  hidden → sold → active; any status → deleted; active ⇄ reserved →
  sold. Active and reserved are public. An edit that adds an upload, or an
  edit of a rejected listing, returns the listing to processing; an edit
  of a sold, deleted or reserved listing is `listing_state_conflict`.
- A console member hides a listing with `POST /v1/console/listings/{id}/hide`
  (`Service.Hold`, decided 2026-11-17): an active listing, or one its seller
  hid, goes hidden and `listings.held_at` is set (migration 0055). While it
  is held the seller's `unhide` is 409 `listing_held`, and a publish after
  an edit or an `unmark-sold` leaves it hidden. `…/unhide`
  (`Service.Lift`) clears the hold and puts a listing still hidden back on
  sale. Both write `listing.hide` or `listing.unhide` with `target_kind =
  listing` and the seller as `details.account_id`, through the `audit.Entry`
  that `Listings.Save` takes beside its events.
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
- `ListingPost` checks, in order: account suspended, 20 listings in
  processing, active, hidden or rejected, then the fields. The category and
  commune lookups reach the domain as `domain.Refs`, so one
  `invalid_listing` lists every field. It inserts the row under
  `pg_advisory_xact_lock(hashtext(seller_id))`, which holds the cap when
  posts race, then attaches the uploads to the listing. A cap reached at the insert refuses before any upload is taken;
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
  loads the listing and calls `mediasvc.ProcessAll`, which calls
  `MediaProcess` for each unprocessed upload with the upload ID as
  idempotency key, so two runs share one processing, and waits for all. All ready: active, the photo keys recorded and
  `listing.published` written. One failed: rejected, the reason kept on the
  photo. The save ignores `ErrConflict`: a listing changed meanwhile has a
  run of its own.
- The listing stores object keys; views build URLs from `MEDIA_PUBLIC_URL`
  at read time.
- A listing's web address is `/listings/{slug}` (decided 2026-11-20): the
  title without accents in hyphenated words, then `listings.code`, 8
  Crockford base32 characters (migration 0064). The code comes from SQL
  `listing_code(id, attempt)`, a hash of the ID; the insert takes the first
  attempt no listing holds, so a seed pack's listings get the same codes on
  every load. Only the code finds a listing (`IDsBySlug`), so an edited
  title keeps old addresses; the website redirects any other address,
  the ID included, to the current slug with a 301. Views carry `slug`; the
  API and the app keep using IDs.
- Another seller's listing is `listing_not_found`. `GET /v1/listings/{id}`
  sits behind `Identify`, which lets a request without an
  `Authorization` header through signed out and holds one with it to
  `Protect`'s rules; the seller sees every status but deleted, anyone
  else only active. Post and edit refuse a suspended account; hide, unhide,
  mark sold, unmark sold and delete do not.
- `PUT /v1/listings/{id}` replaces the whole listing with the body. The
  catalog spec named `PATCH`; the user chose `PUT` on 2026-10-26, since the
  app sends the whole form.
- A listing keeps its last price change after it first went on sale
  (decided 2026-11-20, migration 0066): `listings.previous_price` and
  `price_changed_at`, set by the edit that changes the price. The Listing
  view and the search card (`SearchItem`) carry `price_drop` while that
  change is a drop of at least 5 % (`domain.PriceDropMinPercent`) made less
  than 7 days ago (`domain.PriceDropShownFor`); the website shows "Vừa giảm"
  with the old price struck through. A later raise ends it. A raise less
  than 7 days old is not a price to drop from, so a drop after it counts
  from the price before the raise, and raising then lowering shows nothing.
  A price changed before the first publication was never shown and is not
  kept. Nothing was backfilled, since no price history existed. The fields
  are on the listing row, so search, the feeds and the seller lists read
  them with no extra query.
- The search card (`SearchItem`) carries `photo_thumb_urls`, the
  thumbnails of the listing's first five photos in order, the cover first
  (decided 2026-11-20), so the website can step through them on hover.
  The photos are on the listing row that `cards` already reads, so the
  list costs no query, and thumbnails keep it small. `cover_thumb_url`
  stays for the app.
- Each change writes its event in its transaction: `listing.published`,
  `listing.updated`, `listing.hidden`, `listing.unhidden`, `listing.sold`,
  `listing.unsold`, `listing.deleted`; search and recommendations will
  subscribe to them. `listing.published` carries the payload `first: true`
  on a listing's first publication (`Listing.Published` reports it, from
  `published_at` being unset); a listing that goes back through processing
  after an edit or a rejection publishes without it.
- `categories(code, parent_code, display_name, sort_order)` is loaded by
  migration 0007: 9 roots, each with subcategories coded `<root>.<slug>` and
  ending with `<root>.other` ("Khác"). A listing's `category_code` must name
  a subcategory, a row with a parent.
- Migration 0060 adds `categories.spec_labels`, the specs a listing under
  the subcategory must fill, in order (decided 2026-11-19): "Dòng máy",
  "Dung lượng", "Màu" and "Pin" for `phones.iphone`, none for the roots and
  the ".other" leaves. `GET /v1/categories` returns them. A post or an edit
  needs a spec with each label (case aside) and a value, else
  `invalid_listing` on `specs`; the seller may add specs of their own, 20
  in all. The seed pack loader passes no labels to `domain.NewListing`, so
  seeded listings may lack them.
- `POST /v1/category-guesses` (seller-only) and the three reads guess a
  draft's subcategory with `GuessCategory`. It embeds the title and the
  description with the text model and takes the nearest of the category
  texts: per subcategory in `domain.CategoryKeywords`, its "group › name",
  and that name with each of its keywords; the category texts are embedded
  on the first guess of a process and kept. The ".other" leaves have no
  keywords and are never guessed.
  On the 86 listings of the search sample set and the demo pack, 71 of the 77
  outside Khác come out right from the title and description, 69 from the
  title alone (`go test -tags eval -run TestGuessEval`, 2026-11-19). Each
  guess is one call to the text model, about 1 s through OpenRouter. With the
  text model off, the answer is null.
- `provinces` and `communes` (migration 0005) hold the 34 provinces and
  3,321 communes after the 2025 merger, from
  thanglequoc/vietnamese-provinces-database v5.2.0 (commit 8b78ba511871,
  MIT), display names as its `full_name`. A commune's `lat` and `lng` are
  `ST_PointOnSurface` of its boundary rounded to 4 decimals, computed once
  in a throwaway PostGIS container. The listing stores the commune and a
  point distance is measured from; see Search.
- `GET /v1/regions/nearest?lat=&lng=` is public and answers the commune
  nearest the point as a `Location`, for the app to fill in the area from
  the phone's position at sign-in and in settings (decided 2026-11-13). Past
  30 km from every commune's point it is 404 `no_commune_near`, the same
  bound a listing's GPS and a watch's point take. `catalogsvc.Locate` holds
  the lookup and the bound for both this endpoint and watches. The point
  goes in the query string and is neither stored nor logged; the access log
  writes only the route.
- `GET /v1/sellers/{id}/listings` is public: the seller's active and
  reserved listings, newest first, as search cards (see Screen).
  Discovery serves it and catalog's `SellerListings` builds the cards.
  `SellerCounts` gives screen and trust a
  seller's public listings (active or reserved), the listings under the
  cap and the sold ones; "Đã bán" counts
  every sold listing, sold through an order or marked by hand (decided
  2026-10-27).
- `GET /v1/sitemap`, `/v1/sitemap/listings` and `/v1/sitemap/sellers` are
  public and feed the website's sitemap (decided 2026-11-20). The first
  splits the public listings, and the sellers who have one, into parts of
  `sitemapPartSize` (10,000), newest first, each with the cursor its entries
  are below and its latest `updated_at`; a seller's is that of their latest
  changed public listing. The other two read one part by that cursor: a
  listing's slug or a seller's ID, with `updated_at`. Each call counts the
  parts again, so cursors move as listings come and go; the website caches
  all three for an hour, as their `Cache-Control` says.
- A sitemap listing carries `photos`, the public URLs of its processed
  photos in order, the cover first, for the website's image sitemap
  (decided 2026-11-22). The query reads the keys from `listings.photos`
  with `jsonb_array_elements … WITH ORDINALITY`; a part of 10,000 took
  34 ms on the dev database (80,642 active listings).
- `GET /v1/sitemap/areas` lists each root category and province with at
  least `sitemapAreaMin` (5) active listings, with the count and the
  latest `updated_at` among them, for the website's landing pages
  ("Điện thoại · Hà Nội", `GET /v1/search?category=&province=`)
  (decided 2026-11-22). Fewer than 5 makes a thin page. Active alone
  counts, since search shows only active listings. One `GROUP BY` over
  `listings` joined to `communes` took 47 ms on the dev database and gave
  430 pairs; the website caches it an hour, as the other sitemap calls.
- IndexNow (decided 2026-11-22): with `INDEXNOW_KEY` set and the first
  origin of `WEBSITE_ORIGINS` on https, `worker.ListingWatch` hears
  `listing.published`, `.updated`, `.hidden`, `.unhidden`, `.sold`,
  `.unsold` and `.deleted` and sends the listing to the Virtual Object
  `IndexNow` (one key, `listings`). Its state is the queue of listing IDs,
  each once. An `Add` to an empty queue schedules `Flush` a minute later
  (`IndexNowEvery`); `Flush` takes up to 10,000, schedules the next when
  some remain, reads their slugs (only listings that were ever published)
  and posts `https://<site>/listings/<slug>` to
  `https://api.indexnow.org/indexnow` with the key file at
  `https://<site>/<key>.txt`. A 429 or 5xx is retried 5 times from 10 s,
  doubling; another refusal or the last failure is logged at ERROR and the
  batch dropped. The listing operation never waits on it. A title edit
  submits the new slug only; the old address 301s to it. Reserving and
  releasing are not submitted. `GET /v1/sitemap/indexnow` gives the
  website the key (404 `indexnow_off` without one), and the website
  serves it at `/<key>.txt`. The endpoint is a constant, so tests build
  the client against a stub; locally the origin is http and nothing is
  submitted.

Search vectors (decided 2026-11-09, migration 0024):

- `listing_text_vectors` holds one 1024-dimension `halfvec` per listing, from
  `Listing.EmbeddingText`: the title, the category's name, the condition,
  each spec as "label: value" and the description, one per line, cut at
  2,000 characters. `text_hash` is a hash of the version and the text.
- `listing_photo_vectors` holds one 1152-dimension `halfvec` per processed
  photo, keyed by the photo's object key.
- `model_version` holds `EMBED_TEXT_VERSION` or `EMBED_IMAGE_VERSION`. A row
  of another version counts as missing, so a new version embeds again.
- Active, reserved, hidden and sold listings are embedded; processing,
  rejected and deleted ones are not. Vectors stay while a listing is hidden
  or sold, and stay when it is deleted, since deletion is a status and the
  row stays; the search's status filter leaves them out.
- `Summary` carries `CategoryCode` and `Condition`, which a
  similar-listings search boosts.
- `go run ./cmd/search-reindex -every 2s` sends `Refresh` for every listing
  that should have vectors, the backfill and the way to a new version.
  Listing i starts i × `-every` from now, so a backfill does not start every
  listing at once; `-every` has no default.

### Deal

Module `deal` holds deals: a buyer and a seller agreeing a price on one
listing. Spec: `~/shopnexus/docs/superpowers/specs/2026-10-26-server-deal-design.md`.

- One deal per buyer and listing, unique on `(listing_id, buyer_id)`.
  `POST /v1/deals` returns the caller's deal on the listing with 200, or
  starts one with 201. Starting needs an account that is not suspended, on
  someone else's active listing.
- A deal is `negotiating`, `agreed` or `sold`. While negotiating it holds
  at most one open price, the buyer's offer or the seller's counter.
  Several buyers may agree a price on one listing; agreeing reserves
  nothing, and an open price has no expiry (decided 2026-10-26).
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
- While the seller is in away mode (see Account profile and addresses),
  the buyer's `offer` and `accept` and the COD request are 409
  `seller_away`: each would bind the seller. The seller's counter and
  accept, `decline`, `withdraw`, `cancel`, chat and every step of an order
  already placed go on. `load` reads it through `accountsvc.Service.Away`
  after the suspension check; `DealRequestCod` reads it in the buyer's
  step, so a repeated key answers as the first request did.
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
  reads the listing through `catalogsvc.Service.Summary` (any status) and
  each deal on it. A hidden, sold or deleted listing drops the open price;
  a sold or deleted one also ends an agreement. A new listed price, or
  offers turned off, drops the open price. Each change is a system move
  (`listing_closed`, `price_changed`, `offers_closed`). A listing in
  processing or rejected after a photo edit is not closed. The deal stores
  the listed price at its open price's time to see a price change, so a
  repeated or late event changes nothing.
- Chat, a later module, is one conversation per buyer and seller holding
  the deals of that pair (decided 2026-10-26).
- A deal lives in its pair's conversation (decided 2026-11-15, app plan
  `2026-11-16-app-wire-chat-deals.md`). `Open` calls chat's `OpenPair`
  after its checks and stores the conversation on the deal
  (`deals.conversation_id`, migration 0047); `Deal` and `DealSummary` carry
  `conversation_id`. The checks for a deal (phone, block, listing) are the
  ones for opening a conversation, so chat checks nothing more. Migration
  0047 gave every earlier deal its pair's conversation, opening one with
  both members for a pair that had none, and indexed
  `deals.conversation_id` and `messages.deal_id`. Deals from before 0047
  show their earlier moves only in `Deal.moves`; no deal message was
  written for them. Deal imports chat, so chat mounts before deal, and
  chat still never calls deal.
- The conversation and the deal are two modules' rows, so they are not
  written in one transaction. `OpenPair` reports whether it opened the
  conversation; when the deal's insert then fails, `Open` calls chat's
  `DropPair`, which deletes the conversation and its members unless a
  foreign key (a message, a deal, a report, a ticket) holds it, as
  `MessageSend` deletes a message that media refused. A crash between the
  two leaves an empty conversation, which the inbox does not list and the
  next deal or message of the pair reuses.
- Every move a person makes (offer, counter, accept, decline, withdraw,
  cancel) and every order step, whoever made it, writes a deal message into
  that conversation (`Move.InChat`); a change of the listing
  (`price_changed`, `offers_closed`, `listing_closed`) writes none.
  `worker.ChatPost` subscribes to `deal.moved` and calls `PostMoves`, which
  reads the deal's moves among the event's IDs and calls chat's `PostMove`
  for each. The message takes the move's ID and time, so a retried step or
  a redelivered event writes no second message, and it lands in the thread
  at the move's place whenever the event arrives. A move by a side is sent
  by that side's account, a move by the system by ShopNexus; a move of
  any other party is logged and skipped. A move chat refuses
  (`chatdomain.ErrMisplacedMove`: the conversation is not the pair's, or
  the sender is neither a member nor ShopNexus) ends the invocation with
  a terminal error, since no retry would place it. `Deals.MovesOf` reads
  the moves of a deal by their IDs, for `PostMoves` and `Moves`.
  `dealsvc.MovedIDs` reads the IDs from the event, for notification too.
  `Deal.moves` stays as it was.

Orders, cash on delivery, live in module `deal` (spec:
`~/shopnexus/docs/superpowers/specs/2026-10-26-server-order-design.md`;
the flow was redesigned on 2026-11-12, spec
`~/shopnexus/docs/superpowers/specs/2026-11-12-order-flow-sync-design.md`,
plan `~/shopnexus/docs/superpowers/plans/2026-11-12-server-order-flow.md`).

- An order is `pending` → `shipped` → `completed` or `failed`, or
  `pending` → `cancelled`; a shipped order is cancelled only by the pickup
  timeout. Either side cancels a pending order with the deal's `cancel`,
  which needs no Restate. A completed order makes the deal `sold`; a
  failure or a cancel makes it negotiate again without its agreed price.
- The seller ships with a shipment, which replaced the waybill on
  2026-11-12. `linked`: the server makes it on the seller's account at
  GHTK or GHN (service `standard` or `express`, 1 to 20,000 g, pickup at
  `home` or at a `post_office`), picked up at one of the carrier's pickup
  points or at one of the seller's addresses, which the order copies.
  `manual`: the seller made it on a carrier's own app and types the code;
  the server reads it with the seller's account at that carrier, and a code
  the account does not know is 422 `shipment_not_found`. A carrier the
  seller has not linked is 409 `carrier_not_linked` (see Shipping). The carriers are `ghtk`, `ghn`, `spx`,
  `jt` and `viettel_post`, the ones ShopNexus can read; there is no
  "other". The fee is the carrier's, null while it has none. The
  shipment's status is the carrier's: `pending_pickup`, `in_transit`,
  `delivered`, `failed`. A parcel the carrier already reports failed is
  refused (`invalid_shipment`); one it reports delivered ships delivered.
- Who closes an order: the buyer with `confirm`, at any time while it is
  shipped, or the system 3 days after the carrier reports it delivered.
  The carrier's `failed` fails it at once and releases the listing. After
  the shipment the seller's only move is correcting a manual shipment's
  carrier and code while it is `pending_pickup` or `in_transit`.
- `POST /v1/orders/{order_id}/shipment` takes `{mode: manual, carrier,
  tracking_code}` or `{mode: linked, carrier, service, weight_grams, pickup,
  pickup_from}`; its
  `mode` is the discriminator of a `oneOf`, and every other rule is the domain's
  422 `invalid_shipment` with fields. `PUT` on the same path corrects a
  manual shipment; `POST /v1/orders/{order_id}/confirm` is the buyer's;
  `GET /v1/shipments/lookup?carrier=&code=` is the seller's preview. The
  order moves take the order's ID (decided 2026-11-10), and the domain
  move refuses an order that is not the deal's open one with
  `move_not_allowed`. An unknown order, or one the caller is neither side
  of, is `order_not_found` 404. `POST /v1/deals/{id}/cod` stays on the
  deal: it creates the order. `/ship`, `/deliver` and `/return` are gone.
- The `Deal` aggregate holds its latest order; `orders` keeps the earlier
  ones (migration 0011, with an index on `(deal_id, id)` for the latest
  order every deal read joins). `Deals.Save` writes deal, order, moves and
  `deal.moved` in one transaction. Each order step is a move:
  `cod_requested`, `shipped`, `shipment_updated` (a carrier's report or
  the seller's corrected code), `completed`, `failed`, `order_cancelled`;
  cancel reasons are `buyer`, `seller`, `seller_timeout`, `pickup_timeout`,
  `shipped_to_another` and `listing_closed`. Order changes have no event
  kind of their own: they ride on `deal.moved` as these moves, which
  notification reads. The order's code `SN-100001` comes from the sequence
  `order_codes`, so a use case reads the deal back after its save.
- A buyer has at most 3 orders pending or shipped; `Save` counts them
  under `pg_advisory_xact_lock` on the buyer and refuses the fourth with
  `cod_limit_reached`.
- A suspended account cannot ask for COD; a shipment, a corrected code, a
  confirmation and a cancel finish or take back an order already under
  way, and refuse no one for a suspension.
- The shipment and the confirmation change the listing, so they run as
  Restate flows, `DealShip` and `DealConfirm`, with the invocation key
  `<caller id>/<Idempotency-Key>`. `DealShip` checks a manual shipment
  whole, or a linked one's order and listing, reserves the listing, makes
  a linked shipment through shipping, then saves. A refusal of shipping's
  gives the listing back. When the save loses a race it releases the
  listing and calls off a linked shipment the order did not take. `DealConfirm` saves, then sells the listing.
- The COD request and the order's cancel run as the flows
  `DealRequestCod` and `DealCancelOrder` (decided 2026-11-19), so a
  repeated `Idempotency-Key` answers as the first request did: its
  refusal again, or the deal read anew. The key is optional on both; an
  empty one gives the invocation no key. `DealRequestCod` loads the deal,
  then reads the buyer's status and address in one step, makes the order
  under an ID from `restatex.NewID` (domain `RequestCod` takes the ID, as
  the timer after the save names it, and a replay must name the same
  one), saves, and sends `SellerTimeout`. `saveStep` returns
  `cod_limit_reached` as data, like `deal_changed`.
- A flow journals the deal it loaded and replays the domain move, which
  mints new move IDs. `Save` therefore treats a row that already has the
  write ID as an earlier try that committed and writes nothing more, where
  catalog's `Save` accepts either write ID.
- `DealShip` keeps the listing when its save loses to another try that
  shipped the same order (a second tap with a key of its own), and
  releases it otherwise. Once shipped, it calls `ListingChanged`, which
  cancels the listing's other pending orders with `shipped_to_another`.
  `ListingChanged` reads a listing reserved for an order that is not
  shipped yet as paused, so a shipment that loses to a cancel ends no
  other buyer's order. The deal worker cancels pending orders whose
  listing is hidden, sold off the app or deleted (`listing_closed`).
- The timers are delayed Restate sends to `OrderTimers`:
  `SellerTimeout` 3 days after the COD request, `PickupTimeout` 3 days
  after the shipment, `AutoComplete` 3 days after a delivered report, and
  `Poll` every `Waits.Poll` (`CARRIER_POLL_EVERY`, 2 h) while the order is
  shipped. Each
  names its order, reads the deal when it fires, does nothing when that
  order moved on, and never compares clocks; a save that loses a race
  reads again. A pickup timeout releases the listing, then calls a linked
  shipment off at its carrier; a carrier that already has the parcel, or
  no longer knows it, leaves the order cancelled all the same (decided
  2026-11-13, the order of the two reversed from part 1). The waits are `domain.OrderWaits`; tests pass
  short ones, and `service.New` refuses a zero wait. `DealRequestCod`
  sends its timer once its save committed.
- An order shows when its waiting timer fires as `due_at` (decided
  2026-11-20): `Order.DueAt` adds `Waits.Seller` to `requested_at`,
  `Waits.Pickup` to `shipped_at` while the parcel waits for pickup, and
  `Waits.Complete` to `delivered_at`; nothing is stored. The shipment keeps
  `picked_up_at` (0070), the first carrier status past `pending_pickup`
  but `failed`, cleared by a report of `pending_pickup` again, kept
  across a manual code's correction. 0070 took it from the order's first
  system `shipment_updated` step, or `shipped_at`.
- `ShipmentUpdates.Run(tracking code, status)` is the one entry point of
  a carrier's report: the dev endpoint, the carriers' webhooks and the
  poll call it. It moves every shipped order whose shipment has
  the code (two items in one parcel), changes nothing on a closed order,
  and answers `shipment_not_found` 404 when no order has the code.
- Deal makes, reads and cancels shipments through `service.Shipping`,
  module shipping's three calls with the seller's ID, and maps its
  refusals to its own errors of the same codes. With `CARRIER=fake` the
  server serves `POST /v1/dev/shipments/{code}/status {status}` to anyone,
  and `APP_ENV=prod` refuses it. With `none` (production until the GHTK
  partner contract) the dev endpoint is not registered, and the lookup and
  both shipment endpoints answer 503 `carrier_unavailable` for any order; a
  linked shipment gives back the listing it held. Confirmations, cancels,
  COD requests, the timers and `ShipmentUpdates` need no carrier. A manual
  shipment's carrier and code are read before the order is looked up, so a
  stack without a carrier refuses before it reads an order; an unlinked
  carrier waits for the order check, so a stranger reads `order_not_found`
  and the buyer `move_not_allowed`. The chart sets the carrier in each
  stack's values file and none in `values.yaml`.
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
- Migration 0040 renamed the order statuses, cancel reasons, moves and
  stored notices, gave every waybill a manual shipment (`in_transit`, or
  the order's end), dropped `waybill_source` and `carrier_name`, and
  dropped the shipment of the rows on carrier `other` (development data
  only). Unlike the migrations before it, it broke the release before's
  order reads and writes while it rolled out; that was accepted because
  the app was not wired to the server yet.
- A seller's open offers (decided 2026-11-20): `OpenOffers` lists the
  deals on the seller's listings negotiating with the buyer's price open,
  newest offer first by `(open_at, id)` on the partial index
  `deals_open_offers` (migration 0069). The cursor is a deal ID, and the
  next page lists the offers made before that deal's offer; a cursor deal
  whose offer was answered meanwhile starts the page from the newest again,
  and the client drops the rows it already shows. The service cuts the page
  from the stored rows, then leaves out an offer a change of the listing
  voided before the worker recorded it, so the cursor stays right.
  `CountOpenOffers` counts the stored ones.

### Shipping

Decided 2026-11-13; spec `2026-11-13-carrier-integration-design.md`.

- Module shipping owns the sellers' carrier accounts, the carrier
  clients and the carriers' webhooks. Deal imports it, as
  `deal/service.Shipping`; shipping never imports deal. The app wiring
  hands shipping three functions of deal's (`ShipmentUpdated`,
  `OpenShipmentsAt`, `QuoteFacts`) and deal's not-found error, closed over
  the deal service that is mounted after it.
- A seller links GHTK and GHN, at most one account each
  (`carrier_accounts`, migration 0043). GHTK takes the seller's email and
  password, traded at once for the shop's token through GHTK's B2C
  endpoint with ShopNexus's partner token; the password goes no further.
  GHN takes the seller's token and ShopId, linked when the ShopId is one
  of the token's shops. The token is sealed with AES-GCM under
  `CARRIER_TOKEN_KEY`, a key of its own that `make env` writes, with a
  fresh nonce per seal; it is never returned or logged. A token sealed
  under another environment's key does not open: the call fails without
  the token in its error, and linking again replaces it.
- Unlinking is refused with 409 `shipments_open` while a shipped order of
  the seller's is at that carrier. Deleting an account drops its carrier
  accounts (the account watch on `account.deleted`), and deal's scrub
  drops the seller's address copied as a pickup on their sales.
- A carrier's refusal of a request (a phone number, a weight, a pickup it
  will not serve) is `carrier.ErrRefused` with the carrier's words, which
  shipping answers as 422 `carrier_refused` with `details.reason`. A token
  that no longer opens is 409 `carrier_relink_needed`. Both are refusals,
  so a durable step that meets one ends the request instead of trying
  again. A GHN create refused for its `client_order_code` reads the order
  back by that code, so a retry finds the order the first try made.
- `provider/carrier` has one client per carrier, each called with the
  seller's `Account{Token, ShopCode}`: `Link`, `Pickups`, `Quote`,
  `Create`, `Lookup`, `Cancel`. GHTK's codes 3 and 4 map to `in_transit`,
  5 and 6 to `delivered`, 9, 20 and 21 to `failed`; the rest, the
  shipper's notices among them, move nothing. GHN's statuses map the same
  way. A cancel after pickup is `ErrNotCancellable`. GHTK's order and
  GHN's `client_order_code` are the ShopNexus order code, so a retried
  create finds the first one. GHN takes addresses as commune and province
  names (its two-level scheme, `is_new_to_address`), and the quote is
  GHN's preview, since its fee API still wants district IDs.
- `CARRIER` is `fake`, `none` or `live`. `live` needs `GHTK_API_URL`,
  `GHTK_PARTNER_TOKEN`, `GHTK_CLIENT_SOURCE`, `GHTK_WEBHOOK_SECRET`,
  `GHN_API_URL` and `GHN_WEBHOOK_SECRET`; `fake` and `none` refuse them,
  so a stack never half-points at a real carrier. `none` is an empty
  client map: every call answers `carrier_unavailable` before anything
  else. The fake of each carrier links any account but the password or
  token `wrong`, and its webhooks take the secret `dev`.
- The webhooks are `POST /v1/carriers/{ghtk|ghn}/webhook/{secret}`, for
  each carrier with a secret set. The secret in the path is the only
  check, compared in constant time; a wrong one is 404. GHTK posts a form
  or JSON, GHN JSON. Once the secret matches the answer is 200, also for a
  code no order has or a body that does not read, since GHTK sends again
  on anything else and the poll covers a lost report.
- The poll is a delayed send to deal's `OrderTimers.Poll`, which the
  shipment starts. It reads the order's current code from its carrier and
  moves the order through `ShipmentUpdates` when the status differs, then
  sends itself again until the order closes. A status the carrier does not
  map (`Parcel.Moved` false: a delay, a notice, a cancel) moves nothing, so
  the poll never takes a parcel back to `pending_pickup`. A correction of a
  manual code needs no poll of its own. The pickup timeout reads the
  carrier the same way first, and a parcel picked up since the last poll
  moves the order instead of cancelling it.
- The webhook operation declares no request body, so the request
  validator leaves the body to the handler, which answers 200 to one it
  cannot read.
- The order keeps the pickup for the seller; the buyer's view of the
  shipment leaves it out, since it may be a copy of the seller's address.

### Stream

`GET /v1/ws` tells an open app what changed for the caller, over a
WebSocket (decided 2026-11-10; it replaced the server-sent events of
2026-10-26, which lost every hint sent across a gap and made the app reload
everything after each reconnect and deploy).

- Each account has an update log, `account_updates`, with a `seq` that
  runs 1, 2, 3 with no hole. A repository calls `stream.Notify(ctx, tx,
  accounts, hint)` in the transaction of the change: for each account, in
  UUID order so two transactions cannot deadlock, it bumps the counter in
  `account_update_seqs` and inserts the update. The counter row stays
  locked until commit and rolls back with the transaction, so a rollback
  leaves no hole and seqs become visible in order: a reader of `seq >
  since` never skips one that is still committing. The deal repository
  notifies the buyer and the seller on each save; the chat repository the
  members a message is visible to, and the other member on a read; the
  notification repository the recipient.
- An update carries IDs only (`message`, `read`, `deal`, `notification`,
  `scam_warning`, `listing_read`); the app reloads what it shows. Assist's
  repository notifies the reader's account at each save of a read,
  and trust's repository the warned member at each save of a scam warning.
- `Notify` then rings a `Bell`. `PGBell` runs `pg_notify('stream', …)` in
  the same transaction, which Postgres delivers on commit to every
  instance. The log is the truth and the ring only says something new is
  there, so a Valkey or NATS bell can replace `PGBell` without touching the
  log, the protocol or the hub.
- `stream.Hub` listens through the bell on a connection of its own and
  queues each ring for the connections of its accounts, 16 at most. A full
  queue, or a `LISTEN` connection opened again after a loss, marks a
  connection behind, and the connection reads the log from the last seq it
  sent. Each connection also reads the log every minute, so a ring lost
  without a sign (a `LISTEN` connection that died in silence and has not
  failed yet) costs at most a minute. The hub prunes updates older than 30 days every hour, 10,000 at a
  time; `account_update_seqs` is never pruned.
- On connect, `GET /v1/ws?since=` sends the log after `since` in pages of
  500, or `reset` with the current seq when the log no longer holds what
  follows `since` or `since` is ahead of it, then `ready`. Without `since`
  it starts at the current seq. `GET /v1/updates?since=` reads the same log
  over REST for an app with no socket open.
- Typing comes up the socket, at most once per 3 seconds per connection and
  conversation. The connection asks chat's `Peer` (member, no block,
  cached 30 seconds) and rings the other member through the bell outside
  any transaction, with no update in the log. A refused frame is dropped
  in silence, so typing does not reveal a block.
- The access token lives 15 minutes. The connection closes with 4001 at
  the token's expiry unless the app sends `{"type":"auth","token":…}` with
  a newer one of the same account; a suspended account closes with 4003 at
  connect and at each `auth`. A frame that is not JSON text closes with
  1008; one over 4 KB with 1009, which the WebSocket library sends itself.
  A connection may send 10 frames at once and one a second after that;
  past it, it closes with 1008, so a client cannot turn typing frames into
  database queries. The reader hands frames to the main loop through a
  queue of 16 and drops a frame when the queue is full, so it keeps reading
  while the main loop waits on the database. It starts before the first
  catch-up. The server pings every 25 seconds and closes when a pong takes
  10.
- A browser cannot set Authorization on a WebSocket, so the website opens
  the socket with `new WebSocket(url, ["bearer", token])` (decided
  2026-11-19). `stream.SubprotocolToken`, before the auth middleware on
  `/v1/ws` only, moves the token of a request with no Authorization header
  that offers exactly `bearer, <token>` into the header, and the upgrade
  selects `bearer`, which a browser requires. The socket accepts an
  `Origin` from `WEBSITE_ORIGINS` (scheme and host) or the API's own host
  and refuses others with 403; the app sends no `Origin`. Tokens are never
  read from the query, which proxies log.
- The handler clears its request's read and write deadlines through
  `http.ResponseController` before the upgrade, since the server's
  timeouts would end the connection.
- `http.Server.Shutdown` waits for every request, and a socket never ends
  on its own, so the server registers `App.CloseStreams` with
  `RegisterOnShutdown`. `Hub.Close` makes every connection close with 1001,
  and the apps reconnect to another instance with their seq and lose
  nothing.
- `accounts.last_active_at` is written by the socket at connect, at close
  and every minute (the same tick that reads the log), and not when it is under 30 seconds old; it leaves
  `write_id` alone. The chat views and the seller page show it.
- `platform/stream` reaches the modules through `stream.Access`, which
  `app` adapts from account and chat.
- `POST /v1/dev/updates {kind, …ids}` (decided 2026-11-15) logs an update
  for the caller through `PGLog.Append`, the same append as `Notify` in a
  transaction of its own, and answers 201 with it, `seq` included. The
  app's live test uses it to check that an update reaches the socket.
  `app` mounts it behind the auth middleware when `APP_ENV` is not `prod`.
  The spec's `DevUpdate` is a `oneOf` with one branch per kind, so a
  missing ID, an ID the kind does not carry or an unknown kind is 400
  `invalid_request`; the handler also refuses a kind the log does not hold.
  The IDs need not name anything, since `account_updates` has no foreign
  key. It has no rate limit, because it is dev-only and writes only the
  caller's own log.

### Blocks and follows

Blocks live in module `account`, since both `chat` and `deal` check them. A block stands either way: neither person
may message the other, open a conversation with them or open a deal on
their listings (403 `blocked`). The blocker's inbox leaves the conversation
out. Running deals and orders stay as they are. Each block has a UUIDv7
`id`, so `GET /v1/blocks` pages like every list.

Follows live beside blocks (decided 2026-11-10, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-server-follows-reports-design.md`).

- `POST /v1/follows {account_id}`, `GET /v1/follows` and
  `DELETE /v1/follows/{account_id}` work as the block endpoints do, and
  one pair of generic handlers (`handler/tie.go`) serves both. A `Follow`
  row has the fields of a `Block` row plus `avatar_url`.
- Any account can be followed. A follower follows at most 1,000 accounts
  (`follow_limit_reached`), counted under `pg_advisory_xact_lock` on
  `'follows:' || follower`. A suspended follower gets 403
  `account_suspended`.
- A block deletes the follows between the two, both ways, in its
  transaction. While a block stands, following is 403 `blocked`, and
  unblocking restores nothing. A follow and a block of the same two
  accounts take one lock on the pair (`'pair:' || lo/hi`), so a follow
  written beside a block cannot outlive it.
- `repository.Follows` holds the follow statements; `Accounts.Block`
  deletes from `account_follows` itself.
- The seller view shows `seller.followers` to everyone and
  `viewer.following` to a signed-in reader. A follower hears when the seller
  first puts a listing on sale (see Notifications).

### Account profile and addresses

Decided 2026-10-27 (spec:
`~/shopnexus/docs/superpowers/specs/2026-10-27-server-app-alignment-design.md`).

- `PATCH /v1/accounts/me` takes any of `display_name`, `bio` (0 to 200
  characters, public, no content filter) and `avatar_upload_id` (`null`
  removes the avatar). It writes `account.updated`. A text rule broken is
  `invalid_account` with a field per rule.
- Away mode (tạm nghỉ, decided 2026-11-23): `PATCH /v1/accounts/me`
  takes `away_until`, a time after now and at most 30 days ahead
  (`invalid_account` with field `away_until` otherwise), or `null` to end
  it. It rides on the profile PATCH rather than an endpoint of its own,
  since it is one nullable field of the account, set the way the avatar
  is. `accounts.away_until` (migration 0073) keeps the end; a past end
  reads as not away, so nothing clears it. `Account`, `MyAccount`, the
  listing view's `SellerSummary`, the seller view's `SellerProfile` and
  chat's `Person` (the chat view, the inbox and every other view that
  shows a person that way) carry `away_until` only while it is in force.
  `Person` takes it from the accounts its view already reads in one
  query. Listings stay in search.
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
- `AddressDraft.is_default` true makes the address the default in the
  request that adds or edits it (decided 2026-11-17). The app had made it
  the default with a second call, and a failed second call left the new
  address without it. The flag is no field of `domain.AddressDraft`: the
  handler passes it to `CreateAddress` and `UpdateAddress` as its own
  argument, and they pass it to `Addresses.Insert` and `Addresses.Save` as
  `makeDefault`. Those move the default inside their own transaction,
  under the advisory lock, and refuse a deleted account with
  `session_revoked` (`lockLiving`); `Save` without `makeDefault` stays one
  statement. The old default loses the flag first, since the partial
  unique index is checked row by row; an edit that loses the `write_id`
  race moves nothing. false or absent leaves the default where
  it is. That is the rule of `POST /v1/addresses/{id}/default` too: no
  request takes the default away, and an address loses it only when
  another one gets it or it is deleted. The first address is the default
  whatever `is_default` says. A repeated `Idempotency-Key` returns the
  first request's address as it is now and moves no default.
- An address has a kind, `home`, `work` or `other` (the app's Nhà, Công ty
  and Khác), so the shipper knows when to deliver: an office takes parcels
  in office hours (decided 2026-11-15). The kind is required; the draft
  takes it as a plain string, so an unknown one is the domain's 422
  `invalid_address` with field `kind`, and the views return the
  `AddressKind` enum. A COD order copies it into its recipient
  (`orders.recipient_kind`), and a linked shipment passes it to GHTK and
  GHN as the order's `note` to the shipper: "Nhà riêng" for home, "Địa chỉ
  công ty: giao giờ hành chính" for work, nothing for other. GHTK's
  `deliver_work_shift` picks a morning, afternoon or evening slot, which is
  not office hours, so it stays unset. A seller's address copied as a
  pickup keeps no kind. Migration 0046 made every earlier address and
  order `home`.
- The address book has its own repository, `repository.Addresses`, beside
  `Accounts`. It checks the commune and reads its names from `communes`
  and `provinces` in SQL: catalog imports account, so account cannot call
  catalog. The same join gives `Address.province_code` (decided
  2026-11-19), so an edit form selects the province by code, where it
  matched the province's name before.

### Chat

Module `chat` holds conversations (spec:
`~/shopnexus/docs/superpowers/specs/2026-10-26-server-chat-design.md`).

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
  one message. One step reads what the send depends on (status, a phone
  when opening, listing, conversation, block) and the flow decides between steps; then
  steps open the pair's conversation, add the message and attach its
  photos to it in media (holder: the message), deleting the message again
  when media refuses the attach; a photo message then sends
  `MessagePublish`.
- A conversation is `pair`, the one chat of two people, or `support`, one
  ticket's chat with ShopNexus (decided 2026-11-13): the pair unique index
  covers `pair` only, so a person has any number of support chats. Support
  chats stay out of the inbox, its unread count and inbox search
  (`SupportUnread` counts them), get no scam check, cannot be archived or
  marked unread (409 `conversation_is_support`), and push with the title
  "ShopNexus". `MessageSend` lets a suspended person write in a support chat
  and refuses them everywhere else.
- A message may quote an earlier sent message of its conversation
  (`reply_to`, decided 2026-11-13); anything else is 422 `invalid_message`
  with field `reply_to`. `Message.reply_to` reads the quoted message at read
  time, its text cut to 200 characters; a page reads its quotes in one
  query.
- `Message.refs` are the ShopNexus links in a message's text, read when the
  message is: a listing, person, order, category or search page on one of
  `WEBSITE_ORIGINS`' hosts, or an app `shopnexus://` link, at most 5. Each
  kind is read once a page (catalog's `Summaries` and `Categories`,
  account's `GetMany`, and deal's `Order` through a function app wires,
  since deal mounts after chat). A listing must be public, a person not
  deleted, and an order shows only to its buyer, its seller and ShopNexus;
  any other link has no ref and stays text. Nothing is stored: the card
  reads the listing as it is now.
- A photo message is `processing` until `MessagePublish` has its photos
  through `MediaProcess`, then `sent` or `failed`. Only its sender sees it
  until it is sent.
- Each member has a read marker, which only a newer message moves, and an
  archive, each written by a statement of its own (`MarkRead` moves the
  marker only when the message is newer), so two devices never undo each other. The next sent message that becomes the last one
  clears both members' archives (a deal message that lands late, older
  than the last, clears none but still notifies the streams), so
  archived means `archived_at` is set. Unread counts the other member's
  sent messages after the marker. The inbox pages by the conversation's
  last sent message ID.
- A member can mark a conversation unread (decided 2026-11-10, spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-10-server-inbox-search-unread-design.md`).
  `POST /v1/conversations/{id}/unread` sets `conversation_members.marked_unread`,
  and the conversation then counts at least 1 in the inbox row's `unread`
  and in `/v1/me/unread`. Every read clears the flag in the same `UPDATE`
  that moves the marker (`RETURNING old.…, new.…`, Postgres 18), so the
  marker and the other member's "Đã xem" never move back. Setting or
  clearing the flag notifies the member's own stream with kind `read`, for
  their other devices; a marker move notifies the other member as before.
- Inbox search (same spec) is chat's `Search`, served by screen at
  `GET /v1/screens/inbox/search`. `q` is split by
  `pdb.simple('ascii_folding=true')`, so case and accents fold, đ to d.
  Every word but the last matches a whole word and the last matches the
  start of one. Threads match when the other member's name, or the title
  of a listing a message names, holds every word. That runs in SQL over
  the viewer's conversations without an index, at most 20, on the first
  page only. Messages use the ParadeDB index `messages_search` (migration
  0031) with one `pdb.regex` clause per word: `&&&` with an empty string
  matches nothing, and `pdb.phrase_prefix` stops at 50 expanded terms.
  The status filter, the viewer's conversations, the cursor and
  `ORDER BY id DESC` all run inside the index as a top-K scan. Archived
  conversations are searched; a conversation with someone the viewer
  blocks is not.
- Deal messages (decided 2026-11-15): a message the server writes for a
  move on a deal of the pair (see Deal). `Message.deal` is `{deal_id, move}`
  with deal's `Move` shape (kind, by, amount, at); the text is empty and
  there are no photos. Migration 0047 added `messages.deal_id`,
  `move_kind`, `move_by` and `move_amount`, and a CHECK keeps a deal message
  sent and without text or photos. Clients cannot send one: `MessageDraft`
  has no such field, so the spec refuses it with 400.
- A deal message is a sent message for everything that reads messages: it
  moves the conversation's last message (only forward, so a late one does
  not displace a newer text), clears both archives, notifies both members'
  streams with `message`, and counts as unread for every member who did not
  send it. A move by the system is sent by ShopNexus, which is no member,
  so it counts for both. It writes no `message.sent`: deal's own notice of
  the move already pushes, so a chat push would be a second one for the
  same move, and the scam check and support have nothing to read in it.
  The reply rate leaves deal messages out on both sides, so it still
  measures answers in chat. A deal opens the conversation before any
  text, so a buyer's first message often names no listing; such a
  conversation counts when it holds a deal where the seller sells.
  A deal message cannot be quoted (`reply_to` is `invalid_message`), so
  no empty quote reaches the app.
- A move across a block (decided 2026-11-15) still writes its deal
  message, since the block leaves running deals and orders as they are
  and both sides already read the move in `Deal.moves`. The message
  follows the rules of any message in a blocked conversation: the
  blocker's inbox and unread count leave the conversation out, and the
  blocked person's keep it.
- Quick replies (decided 2026-11-20): `GET` and `PUT /v1/me/quick-replies`
  hold a person's saved answers, which a seller's composer offers to put in
  the field. `PUT` replaces the list whole, in its order, and answers it
  trimmed; the spec caps it at 20 answers of 1 to 200 characters with
  something besides spaces. An account without a row (migration 0068)
  reads three defaults from `domain.DefaultQuickReplies`, and a read
  writes no row; an empty list stays empty. `PUT` takes a share lock on the
  account's row and refuses a tombstone with `session_revoked`, as
  notification settings do, and chat's worker deletes the row on
  `account.deleted`.

### Screen

Module `screen` serves one `GET` per view of the app (decided 2026-10-27,
spec: `~/shopnexus/docs/superpowers/specs/2026-10-27-server-app-alignment-design.md`). The
app reads a view in one call, and after a socket update it reloads the view
it shows in one call. Mobile and desktop read the same views; a desktop
page that shows two views calls both at once.

- `GET /v1/screens/home` (signed in or not): the category tree with the
  active listings counted under each root and in each subcategory, and the
  first page of `GET /v1/search?sort=nearby` with the same position and
  `radius_km`.
- Home without a position (decided 2026-11-15, app plan
  `2026-11-15-app-wire-discovery.md`): the view answers with an empty
  `nearby` and no `next_cursor`, where it used to be 400
  `position_required`. `radius_km` takes the search's rules: 1 to 200, 50
  km when absent, and `position_required` without a position. Catalog
  counts the active listings by subcategory in one query, with no
  exclusions, and a root's count is the sum of its subcategories, since a
  listing always takes a subcategory: `listings.category_code` references
  `categories`, and every post and edit checks the code is a leaf. A
  migration that moves a subcategory to another root moves its count with
  it, where the old count by `category_root` (the code's prefix) did not.
  `Counts` lost its `CountField`, as nothing counted by root any more.
- Dành cho bạn and Vì bạn đã xem gần đây (decided 2026-11-17, app plan
  `2026-11-17-app-finish.md`): Home's `for_you` is one page of
  `SearchItem` cards, 20 like the nearby feed's first page, and `viewed`
  is `{category, items}`, a catalog `Category` and up to 10 cards; neither
  pages. Discovery's `ForYou` reads both in one call from `screen.Home`, so
  the exclusions and the views are read once. The signals come from the 30
  UTC days that end today (`domain.ForYouSince`): the newest 100 listings
  the person opened (`listing_view_marks`, one mark per listing at its
  last day), the newest 100 they saved (`saved_listings.created_at`), and
  the entries of their search history searched in the window whose first
  category chip is a subcategory. The history holds the last 10 searches
  (`domain.MaxHistory`), so these are the last 10 searches that fall in
  the window, and an older search the trim dropped counts nothing. Each
  signal adds to its subcategory: a save 3, a search 2, an opened listing
  1 (`domain.ForYouBoosts`); a chip that names a root adds nothing. The
  bounds (`domain.MaxViewSignals`, `domain.MaxSaveSignals`) keep a heavy
  browser's Home at two reads of at most 100 rows each and one catalog
  `Summaries` call for at most 200 listings, which gives the subcategory of
  each. The feed is one catalog search over the active listings
  nationwide in `SortPointsNewest` order: each weighed subcategory is a
  `BoostCategory` boost at its weight, and the listings sort by boost
  points, then `published_at`, then ID, each descending. A listing outside
  the weighed subcategories has 0 points, so the newest listings fill the
  page after the weighed ones in the same query, and no ID repeats. A
  guest and an account without signals get no boost, which is the newest
  order. Every listing of one subcategory has the same points, so the
  first 20 of the order are among the newest 20 of each weighed
  subcategory and the newest 20 outside them. Catalog's `pointsNewest`
  reads those through the listings index, one `UNION ALL` branch each,
  and orders only them; the branches share one binding of the excluded
  IDs and build no BM25 should-clauses. It takes category boosts only. Both
  lists leave out what search leaves out (`exclusions`: the hides, and the
  accounts on either side of the viewer's blocks) and the viewer's own
  listings through `ExcludeSellers`, as a `like` search does. The cards
  come from catalog's `cards` and `withSaved`, so `saved` and
  `distance_km` behave as on search. `viewed` reads the same 100 marks: it
  takes their latest day, the subcategory most of that day's marks have,
  and on a tie the subcategory of the listing created last, the highest
  UUIDv7 ID (`domain.ViewedCategory`). Its items are that subcategory's
  active listings outside those 100 marks, newest first, with the feed's
  exclusions. It is absent signed out, without a mark in the window, or
  with no such listing. Migration 0054 adds `search_history.category_code`,
  the code of the first category chip beside its label, because a label
  names no single subcategory: "Khác" is under every root. The column is
  plain text with no foreign key or index, as `search_watches.category`
  is; a code no category has any more boosts no listing. Search takes the
  code from `domain.Chip.Code`, which the API leaves out, and entries from
  before the migration add nothing. The same migration replaces the index
  on `listing_view_marks (viewer_id)` with one on `(viewer_id, viewed_on,
  listing_id)`, so the read of the newest 100 marks stops at 100 rows.
  `screen.Home` still reads the position and the exclusions twice, once in
  the nearby search and once in `ForYou`: `nearby` is the first page of
  `Search` by the 2026-11-10 decision, and one discovery call for both
  would either copy the nearby query or split `Search` into phases. The
  repeat costs one commune lookup with `near_commune` and two indexed
  reads, the hides and the blocks.
- Distances on the listing and seller views (decided 2026-11-15, same
  plan): both views and `GET /v1/sellers/{id}/listings` take
  `near_lat`/`near_lng` or `near_commune` and check them as the search
  does. The listing view has `distance_km` beside `listing`, and none
  without a position. The distance sits beside the listing because it
  depends on the request: a `Listing` reads the same for every caller and
  every endpoint that returns it. The listing view reads the listing once,
  through `GetNear`, and measures from that row.
- The seller view's listings are search cards (decided 2026-11-15): the
  seller page is a browse surface like search. Its `listings` and
  `GET /v1/sellers/{id}/listings` are a `SearchItemList`, a page of
  `SearchItem` paged by listing ID as before. A card has `saved` for a
  signed-in reader and `distance_km` with a position, as on search;
  `SellerListingList` and its `distances` array are gone. Catalog's
  `SellerListings` builds the cards with `cards`, the builder search uses,
  from the domain listings of the page that `page.Of` kept, so the
  look-ahead row gets no card. Discovery's `SellerListings` marks the
  saves with the search's `withSaved` and serves the endpoint, because
  catalog cannot read the saves. A signed-in reader's cards leave out the
  listings they hid, as search and the nearby feed do: discovery passes
  the hides it reads for `exclusions` to catalog, which leaves them out in
  the `ListBySeller` query, so the pages keep their size. Catalog reads
  the seller's account only for an empty page, to answer
  `account_not_found`; the seller view has read it already. The seller's
  own list is `GET /v1/screens/my-listings` (see Tin của bạn and listing
  views).
- Screen owns no table. Each view is one method of its service that reads
  the other modules' services top to bottom; depguard forbids every other
  module to import it, so it adds no dependency between the others.
- `GET /v1/screens/listing/{id}` (signed in or not): the public listing,
  the seller's summary and, for a signed-in viewer who is not the seller,
  `viewer {saved, deal, conversation_id}`, where `deal` is deal's
  `DealSummary` of the viewer's deal. A listing that is not public is 404
  for everyone; its seller reads it through `GET /v1/listings/{id}`.
- `GET /v1/screens/sold-listing/{id}` (signed in or not, decided
  2026-11-20): a sold listing's title, slug, price, category, condition,
  place, photos and `sold_at`, so an old link still shows what sold and
  leads to similar listings. It has no seller, description, specs or
  viewer, and counts no view. Any other status is 404, a listing held by
  ShopNexus after the sale (hidden) included, and so is a sold listing
  whose seller deleted their account: erasure keeps it for the buyer's
  order, not for the public. It is a view of its own, not a 200 of the
  listing view, because the app reads that view's 404 as "maybe the
  seller's own listing" and falls back to `GET /v1/listings/{id}`.
- `GET /v1/screens/seller/{id}` (signed in or not): the seller's profile
  (account's public fields, catalog's counts, and the rating of the reviews
  page, and the follower count), the first page of the seller's public
  listings and of their reviews, and
  `viewer {blocked, following, conversation_id}`; `blocked` holds either
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
- The chat view (decided 2026-11-15) also has `blocked`, a block either
  way (account's `Blocked`), so the app can turn off the composer, and
  `other_read_message_id`, the other member's read marker, absent before
  they read anything, so the app can show "Đã xem". The marker is part of
  chat's `Conversation`, which `Get` builds from both member rows in one
  query (`Chats.Members`), so the view pays only for the block check. The marker shows whatever the block, as the `read` update already
  tells the other member of each move of it.
- `GET /v1/screens/profile-tasks` (decided 2026-11-13, spec
  `2026-11-13-profile-tasks-design.md`): the Hoàn thiện hồ sơ view.
  `tasks` holds four steps in a fixed order, `address`, `bio`, `phone` and
  `avatar`, each with `done`. `account` is the caller's `MyAccount` and
  `address` the default of their address book, null when the book is
  empty. A step is done when the book holds an address, the bio is not
  empty, the account has a phone identity, or it has an avatar. The app
  counts the steps and takes the first one not done as the next. eKYC and
  COD setup are not steps: COD needs only a phone, and someone who only
  buys never sets COD up.
- `GET /v1/screens/reputation` (decided 2026-11-14, spec
  `2026-11-14-reputation-screen-design.md`): the Uy tín của bạn view.
  Points, tiers and tier perks are out of the product, and the view holds
  figures the server already has. `rating` is trust's `Rating` with a null
  `average` without reviews. `sold` is catalog's `SellerCounts.Sold`,
  `joined_at` the account's `created_at`, `followers` account's
  `FollowerCount`. `figures_90d` is the caller as a buyer over deal's
  `domain.BuyerPeriod` (90 days): COD orders requested and the failed ones
  among them, from `dealsvc.BuyerOrders`. `caps` shows `listings`, the
  listings in a catalog `domain.OnSale` status (`SellerCounts.OnSale`)
  against `domain.MaxOnSale`, and `cod_orders`, the caller's pending and
  shipped orders as a buyer against deal's `domain.MaxOpenOrders`; the
  view reads both maxima from the owning domains.
- `reply` is chat's `ReplyRate`, computed on read with no table of its
  own. `ReplyThreads` takes the first sent message of each of the caller's
  pair conversations with a message in the last 90 days, through
  `messages_conversation`, and keeps those another person wrote about a
  listing of the caller's (joining `listings`, as `SearchThreads` does),
  with the caller's first sent message in each. `domain.ReplyRateOf`
  counts a thread whose first message is from the last 90 days and is
  either answered or more than 24 hours old; it is in time when the answer
  came within 24 hours. The rate is the percent in time, rounded down,
  with the count; null below 5 counted.
- `reply_time` on the listing view's `SellerSummary` and the seller view's
  `SellerProfile` (decided 2026-11-20) is chat's `ReplyTime`: how soon the
  seller usually answers people who write about their listings, as a bucket
  the client words (`minutes` up to 10 minutes, `hour`, `hours` up to 6,
  `day` up to 24), absent below 5 answers in 30 days or past a day. A view
  reads it from a table, not from the messages: chat's repository writes a
  row of `reply_samples` (migration 0067) in the transaction that makes a
  message sent, when the message ends a wait, and drops the sender's rows
  older than 30 days. A wait starts at the other member's first message
  after the seller's previous one, when that came at least an hour later
  (`domain.ReplyGap`), or at the conversation's first message; within the
  hour it is the same talk and starts none. The conversation counts when
  the other member named a listing of the seller's or a deal there has the
  seller selling, as for the reply rate, and deal messages count on
  neither side. The read is one indexed `percentile_disc(0.5)` over the
  seller's rows. Unanswered waits count nothing; the reply rate covers
  those. Migration 0067 fills the last 30 days by the same rule.
- `phone_verified` on the listing view's `SellerSummary` and the seller
  view's `SellerProfile` (decided 2026-11-20) is account's `HasPhone`: the
  seller signs in with a phone number, which the sign-in verified. The
  website shows "Đã xác minh SĐT" for it. The number stays out of both
  views.
- `GET /v1/me/unread` (decided 2026-11-19): `{unread, notifications}`,
  chat's unread total, as the inbox counts it, and notification's unread
  notices, so a badge needs one call. Screen serves it, since chat cannot
  import notification (notification's worker reads chat). The path and the
  `chat` tag stay as they were, so the app's generated chat client keeps
  `getUnread`.
  `offers` (2026-11-20) adds the buyers' open offers waiting on the caller
  as seller, deal's stored count, for the badge of the offers view.
- `GET /v1/screens/offers?cursor=&limit=` (decided 2026-11-20) is the
  seller's offers view: each open offer as its `Deal`, with the
  `offer_range` of the seller's counter, and the buyer as a `Person`. The
  answers are the deal's own moves; the view reads the page and its buyers
  in two calls.
- "Load more" uses the list endpoints, which return the same item shapes
  as the view: `/v1/sellers/{id}/listings` (`SearchItem` cards, like the
  seller view's `listings`), `/v1/sellers/{id}/reviews` and
  `/v1/conversations/{id}/messages`.
- Each piece of data has one read path: `GET /v1/conversations`,
  `GET /v1/conversations/{id}`, `GET /v1/sellers/{id}`,
  `GET /v1/me/orders` and `GET /v1/me/listings` were removed when the
  views came, and no resource carries fields for one screen.
- A view makes a fixed number of service calls per page, and the services
  read a page's people, communes and listings in one query each.

### Trust

Module `trust` holds reviews (spec:
`~/shopnexus/docs/superpowers/specs/2026-10-26-server-reviews-saved-design.md`).

- Two-way reviews (decided 2026-11-19, spec
  `2026-11-19-two-way-reviews-design.md`): both sides of an order review
  each other once it is `completed` or `failed`, 1 to 5 stars and up to 500
  characters of text. A review rates a person: `role` is the side its
  subject was on, `seller` (written by the buyer) or `buyer` (written by
  the seller), with `reviewer_id` and `subject_id`. One review per order
  and role. `PUT /v1/orders/{order_id}/review` takes the role from the
  caller's side of the order. Reviews written under the old rule, while
  `shipped`, stay as they are.
- The reviewer edits, deletes or posts it again within 7 days of the first
  submission (`created_at`); after that it is `review_locked`. A delete
  keeps the row with `deleted_at` and no text, so posting again cannot
  restart the 7 days (decided 2026-10-27). A suspended account cannot
  write one.
- Blind until both or 7 days: `visible_at` is written at insert, the
  order's `closed_at` plus `domain.BlindWindow`, or now when the other
  side's review exists, and `Insert` pulls the other side's `visible_at`
  down to the new review's creation in the same transaction, under an
  advisory lock on the order. A review is visible when `visible_at <= now`,
  so no timer writes data. A hidden review shows to its reviewer only:
  lists, ratings and the subject's order view skip it.
- The subject replies once, up to 500 characters, to a visible review
  (`POST /v1/reviews/{id}/reply`); anyone else gets `review_not_found`, a
  hidden review `review_hidden`, a second reply `review_replied`. A reply
  closes the review: `CanChange` is `review_closed` for the reviewer.
- Review writes follow the `write_id` rule: `Insert` refuses an order that
  has a review in the role, or a hidden review when the other side's
  exists, and `Save` a stale row, all `try_again`. `Save` writes
  `review.replied` with the first reply.
- `trust` reads the order through `dealsvc.Service.Order`, which answers
  false for an unknown order like the other facts reads; an unknown order
  or one the caller is no party to is trust's `order_not_found`.
  `GET /v1/accounts/{id}/reviews?role=` is public and returns a
  `ReviewList` of visible reviews without a summary;
  `GET /v1/sellers/{id}/reviews` stays as its `role=seller`. The rating
  comes from `Service.Rating` (decided 2026-11-08, so a view no longer gets
  the average twice).
- Review photos and helpful marks (decided 2026-11-20). `ReviewDraft.photos`
  takes up to 4 upload IDs in order; leaving the field out keeps the
  review's photos, so a client that never sends it (the app) changes none,
  and `[]` removes them. New ones go through media's `Photos`, which
  attaches them with the order as holder (a first review's ID is new on
  each try, so a retry must find its photos still free for it) and waits on
  `MediaProcess` for each at once; a refused photo is `invalid_upload`.
  `reviews.photos` keeps `{upload_id, photo_key, thumb_key}` (migration
  0067), and a delete drops them. `Review`, `OwnReview` and `TheirReview`
  carry `photos` with public URLs.
- `PUT /v1/reviews/{id}/helpful` marks a visible review helpful and
  `DELETE` takes the mark back; its reviewer and subject get `own_review`,
  a hidden or deleted review `review_not_found`. One row per mark in
  `review_helpful`. `Review.helpful` is `{count, marked}`, where `marked`
  is sent only to a reader who may mark it; the review lists read under
  `Identify` for it. A page counts its marks in one grouped query.
- `Service.Rating` is a person's average and count over their visible
  reviews in one role. The listing view, search's seller head and the
  console agent read the seller rating; the seller view and the reputation
  view read both, and the chat view gives a seller `buyer_rating`, the
  other member's rating as a buyer, for the offer card. The seller's public profile (name, avatar, bio,
  when they joined, rating, catalog's on-sale and sold counts) is screen's
  `SellerProfile`, which screen builds from account, catalog and trust
  (moved from trust on 2026-11-08).

Reports are trust's too (decided 2026-11-10, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-server-follows-reports-design.md`).

- `POST /v1/reports` takes exactly one of `listing_id` and `account_id`,
  a `reason` (`banned`, `fake`, `scam`, `other`) and an optional `note` of
  up to 500 characters after trimming. A broken rule is `invalid_report`;
  the caller's own account or listing is `self_report`.
- One table, `reports`. `account_id` is always set, to the seller for a
  listing report, so a later queue can gather every report about a person.
  `listing_id` is null for an account report.
- A reporter has one open report per target, kept by two partial unique
  indexes (one per kind of target) and `INSERT … ON CONFLICT`. Reporting the
  target again replaces the reason and note and answers 200; a new report
  answers 201.
- `DELETE /v1/reports/{id}` withdraws a report for "Hoàn tác". The row
  stays with `withdrawn_at`, and the next report of the target opens a new
  row. Someone else's report is `report_not_found`.
- `GET /v1/reports` (decided 2026-11-19) lists the caller's reports,
  newest first and paged by report ID over `reports_reporter (reporter_id,
  id)` (migration 0063), withdrawn and resolved ones included, so a client
  shows them and withdraws an open one. `Report.status` is `withdrawn` once
  `withdrawn_at` is set, else `resolved` while a `resolution_id` is set,
  else `open`; a reopened case clears the ID, and its reports read open
  again. The domain's `Report.Status` decides it, from the row's
  `resolution_id IS NOT NULL`.
- A suspended account cannot report. A deleted listing is
  `listing_not_found`; a hidden, reserved, sold or rejected one can be
  reported.
- An account report may carry `conversation_id` (decided 2026-11-10, spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-10-server-inbox-search-unread-design.md`),
  the reporter's conversation with that account, which trust checks through
  `chatsvc.ConversationWith`; another one, or one beside `listing_id`, is
  `invalid_report`. A block keeps the conversation, so the app blocks
  first and reports second. Reporting the account again replaces the
  conversation or clears it. Trust takes chat's service, so `app.go`
  mounts chat before trust.
- Console members work reports as cases (decided 2026-11-17, spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-16-console-agent-design.md`,
  Phase 3 without the agent). A case is the reports about one account,
  not withdrawn, that share one row of `report_resolutions` (migration
  0055); the reports with no `resolution_id` are the account's open case.
  A resolved report no longer counts as the reporter's open report of its
  target, so the partial unique indexes take `resolution_id IS NULL` and a
  new report opens the next case.
- `GET /v1/console/reports?status=open|resolved` lists cases: open ones by
  their latest report with `total`, resolved ones by `resolved_at`, both
  paging by the ID cursor of the row that sorts them (the latest report, or
  the resolution). `GET /v1/console/reports/{account_id}` is the open case
  or else the latest resolved one, with every listing reported in it, most
  reported first, and its reports newest first; an account with no report
  that counts is `report_case_not_found`. Module trust builds both views
  from account's `ConsoleAccountsOf` and catalog's `ReportedListings`.
- `…/dismiss` and `…/resolve` close the open case as `dismissed` or
  `actioned` (409 `report_case_not_open`); `…/reopen` marks the latest
  resolution `reopened_at` and opens its reports again (409
  `report_case_not_resolved`), except one whose reporter has reported the
  same target since, which stays with the reopened resolution. Each runs
  under `pg_advisory_xact_lock` on the account and writes `report.dismiss`,
  `report.resolve` or `report.reopen` (target the account,
  `details.resolution_id`) in its transaction. The ShopNexus account is
  `account_is_system`. Console mounts after trust and catalog, whose
  services its handler calls, as it calls support's.

Scam warnings are trust's too (decided 2026-11-11, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-11-server-scam-warning-design.md`).

- A signal is a fixed code: seven read from messages (`prepayment_request`,
  `off_app_contact`, `suspicious_link`, `sensitive_info_request`,
  `impersonation`, `fake_payment_proof`, `pressure`) and two computed
  from data (`new_account` under 7 days, `price_far_below` at least 30%
  under the median of 5 or more similar listings). The server writes each
  code's Vietnamese text; the API returns `{code, text}`.
- A `Classifier` picks the message codes of each side from a window of the
  last 20 messages with text, after `domain.Redact` replaced phones, bank
  and card numbers, emails and links. Redact reads each run of digits
  (single spaces, dots or dashes between them) as a whole: an amount keeps
  its unit (đ, vnd, vnđ up to 10 digits; k, tr up to 7) when the unit does
  not start a word, a run from + or of 10 or 11 digits from 0 is a phone,
  and any other run of 8 to 19 digits is a bank number. `LLMClassifier` asks LiteLLM's alias
  `scam-check` with the key `server-trust` under a strict schema of the
  seven codes. A trained model is another `Classifier`, built in the root
  package `trust` and passed by app as `trust.Settings.Classifier`.
- The worker hears `message.sent` and sends `ScamCheck.Run`, a Virtual
  Object keyed by conversation, for a message with text. Run's four steps
  plan, classify, gather facts and save, and return IDs and codes only.
  The plan journals the window's message IDs and the later steps read
  exactly those. A window whose IDs equal the newest check's, or a
  conversation with 50 checks in 24 hours, stops the run; a photo message
  that turns `sent` after a newer message changes the window and gets its
  check; a refusal or a classify step past 3 attempts and 10
  minutes skips it.
- `scam_warnings` holds one row per warned member, `scam_checks` one row
  per run with the window's message IDs, the classifier and the codes, and
  no text; joined with `messages`, dismissals and `scam`
  reports it is the training set. Each save deletes up to 1,000 checks
  older than 365 days.
- A warning needs a message code; fact signals join it then. Codes are
  only added. `dismissed` holds until a message code appears after the
  dismissal; a code a check saves after a dismissal that landed while the
  model ran is first seen a microsecond after the dismissal.
  `POST /v1/conversations/{id}/scam-warning/dismiss` checks membership
  through `chatsvc.Get`, so a member who blocks the other can dismiss, and
  dismisses under the row's lock (`SELECT … FOR UPDATE`), so it never
  answers `try_again`. No read returns a row by `suspect_id`, so the suspect never sees
  a warning about themselves.
- Each signal names the message it sits under (decided 2026-11-20):
  `message_id`, the suspect's newest message in the window of the check
  that found it, which the plan step journals for each side. A client
  shows the warning under that message, so the warned member sees which
  message it is about. Signals saved before carry none.
- Screen reads `trustsvc.ScamWarnings` once per page for the inbox, the
  inbox search and Chat; the stream update `scam_warning` names the
  conversation. `account.deleted` deletes the account's own warnings;
  those about it stay.

### Search

Search and the nearby feed (decided 2026-10-27, spec:
`~/shopnexus/docs/superpowers/specs/2026-10-27-server-search-design.md`).

- Catalog owns `listings` and its ParadeDB index (`listings_search`,
  migration 0019: title, description, `specs_text`, and the fields
  category code and root, price, condition, offers, status, `published_at`,
  `lat`, `lng`). It serves a structured `domain.Search`: the person's
  filters as `must`, phrases of which one must match, constant-score
  boosts, and a sort. The index is transactional, so a listing is
  searchable once its write commits. Only `active` listings show.
- Chat owns the second ParadeDB index, `messages_search`, for inbox search
  (see Chat).
- Discovery owns the understanding. With `q` it asks the model, through
  `internal/provider/llm` (an OpenAI-compatible chat completions call with
  a strict JSON schema, `LLM_BASE_URL`, `LLM_API_KEY`, `LLM_MODEL`, and
  `LLM_REASONING_EFFORT` sent as `reasoning_effort` when set), for
  boosts and demotes: phrase, category, price, condition, place, near_me,
  fresh, offers, each with up to three values in priority order. The model
  never writes a weight. Every value is resolved against real rows (the
  category tree, `provinces` and `communes` by name) or dropped, and one
  log line holds what the model asked for and what ran.
- A guest's `q` skips the model and the embedding (decided 2026-11-23): the
  raw words search through BM25 alone, on the folded fields below, with no
  chips and no photo list, and the cursor pins the pages as below. Their
  vector, typed without diacritics, brought listings that shared no word
  ("Dây da bò" for "xe dap di lam"). A guest's first page carries
  `preview_chips`: the chips of the cached answer for the same query, when
  a signed-in person searched it within the cache's 7 days. The search does
  not apply them; the clients show them locked beside Đăng nhập, and with
  no cached answer the card has its line of text alone. The preview calls
  no model. The model reads signed-in people's
  queries, so its cost follows accounts, and the full search is a reason to
  sign in. A guest does not read a cached answer either.
- A guest's address makes 2 searches with `q` a second, in bursts of 30, and
  gets 429 `rate_limited` past them (discovery's tier `guest-search`, see
  Rate limits); browsing, `like` and the category pages are not limited, since
  they reach no paid model and the website prefetches listing pages;
  the website forwards the visitor's address, which `TRUSTED_PROXIES`
  accepts. A signed-in person's queries reach the model at most 200 times a
  UTC day (`domain.ModelCallsPerDay`, one row per account in
  `search_model_uses`, cleared on erasure); a cached answer does not count,
  and past the cap the raw words search (decided 2026-11-23).
- The raw query is always a phrase at weight 0.5 unless the model named
  it, so a model that errs or takes longer than 4 seconds leaves the raw
  words to search, with no chips. A search waits for the model's answer
  (decided 2026-11-23: every person sees the full search while the app is
  new; a 600 ms wait that searched the raw words was tried and dropped).
  The call runs apart from the request and caches its answer when the
  person left, and searches of one query share one call. A page that
  searched the raw words ends its cursor with `r`, and the pages after it
  search the raw words too, even once the answer is cached.
- Weights are starting values in BM25 points (a matching short title
  scores about 1 to 7): category and price 1.5, condition and offers 0.75,
  freshness 1.0 (published within 3 days) and 0.5 (within 7), distance
  rings of 2, 5 and 15 km at 1.25, 0.75 and 0.4. A signal's second and third
  values count 0.5 and 0.33. A set of sample queries is what would measure
  them. A phrase matches the title at twice its weight, description and
  specs at its weight, and the title's words of 5 letters or more with one
  edit at half. The index holds each of the three twice (migration 0076,
  decided 2026-11-23): as written, and with diacritics folded away
  (`title_folded`, `description_folded`, `specs_folded`). The raw words, a
  guest's or a query the model did not read, match the folded fields, so
  "xe dap" finds "Xe đạp"; the model's rewrite matches the fields as
  written, since it has the diacritics.
- Distance and freshness score inside the index as range boxes on `lat`,
  `lng` and `published_at`. The nearby feed is the same query with no
  phrase: within 50 km, scored by the rings and freshness alone.
- `GET /v1/search?province=` keeps the listings whose commune is in that
  province (decided 2026-11-22), with every other filter and in `counts`;
  an unknown code is 400 `invalid_province`, and `watch` with it is
  `search_conflict`. The index holds no province: `Filters.Province` is
  `commune_code IN (SELECT code FROM communes WHERE province_code = …)`
  beside the index query, in BM25, the vector lists, the points sort and
  the counts. Browsing every Hà Nội listing newest first took 44 ms on the
  dev database against 6 ms without the filter, since ParadeDB's top-K scan
  stops working with a condition outside the index. A province column in
  the index would bring it back at the cost of a stored column every
  listing write sets and an index rebuild.
- The nearby feed is `GET /v1/search?sort=nearby` (decided 2026-11-10; it
  was `GET /v1/feed/nearby`, which ran the same query on its own path). It
  takes the person's filters and `radius_km`, and with `q`, `photo` or
  `like` it is `search_conflict` 400. Home's `nearby` is its first page, so
  Home's `next_cursor` pages on through search.
- The answer is cached in `search_understandings` by the lowercased query
  and the prompt version for 7 days; the write of a new answer deletes the
  expired rows. The table stores no account. A chip the person turns off
  (`off=<id>`) reuses the cached answer.
- A listing's point is the seller's GPS blurred, or the commune's point.
  The handler blurs it (a random offset of up to 500 m, then a 0.01° grid),
  so the exact point reaches no journal, row or log. A point farther than
  30 km from the commune's point is refused, since the communes table holds
  points and no boundaries. An edit without GPS keeps the point, a new
  commune replaces it, and no view or search item carries it.
- The searcher sends `near_lat` and `near_lng`, or `near_commune`; neither
  is stored or logged. `distance_km` is whole kilometres, truncated, 0
  meaning under 1 km. Results page by an offset cursor and stop at 500.
- Catalog owns the position (moved from discovery on 2026-11-15, when the
  listing and seller views took one): `catalogdomain.Near` is the request's
  point or commune, `catalogsvc.Origin` resolves it or answers
  `invalid_position`, and `catalogdomain.DistanceKm`, measured from the
  listing's own point, is every distance the API shows. Search, watches,
  the price guide and the views call these. A coordinate of exactly 0
  reads as absent, as it did before the move, since the query fields are
  not pointers; no point in Vietnam lies on the equator or the prime
  meridian.
- ParadeDB clones are about 13 MB, so the test database's tmpfs is 4 GB,
  and the slower index writes need `max_connections=400` in the test stack.

Hybrid search (decided 2026-11-09, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-09-server-photo-similar-search-design.md`):

- `GET /v1/search` takes `photo` (a waiting upload of the caller's) and
  `like` (a listing on sale or reserved). `photo` goes alone and needs a
  signed-in caller; `like` goes with `q`. Errors: `search_conflict` 400 for
  `photo` with `q` or `like`, `unauthenticated` 401, `listing_not_found` 404,
  `invalid_upload` 422 as media returns it, and `search_unavailable` 503 when
  the photo cannot be embedded.
- A hybrid search fuses lists by weighted RRF with k = 60: BM25 on the
  phrases, the text vectors, the photo vectors (a listing at its closest
  photo), and a boost list that ranks the other lists' listings by boost
  points alone. Each list takes at most 500 listings under the person's
  filters. Weights: `q` 1.0, 1.0, 0.5, 0.75; `like` 0.5, 1.0, 1.0, 0.5;
  `photo` the photo list alone at 1.0.
- A listing joins a vector list at `EMBED_TEXT_MIN_SIMILARITY` or
  `EMBED_PHOTO_MIN_SIMILARITY` cosine or more. When the photo list's query
  is the model's look, a text, the floor is `EMBED_LOOK_MIN_SIMILARITY`:
  SigLIP 2 scores a text against a matching photo near 0.1. Sorts other
  than relevance order the fused listings.
- One SQL statement holds the lists as CTEs (`bm25`, `txt`, `img`, `boost`,
  then `fused`). Its transaction sets `hnsw.iterative_scan = relaxed_order`,
  so a narrow filter still fills a vector list.
- `like` reads the source's text vector and the normalized mean of its photo
  vectors, its title as a phrase, and boosts for its subcategory and a price
  band of ±30 %; it leaves out the source and the viewer's own listings.
- The understanding gains `rewritten`, the query in correct Vietnamese for
  the text vector, and `visual`, at most about 12 English words for the
  photo vectors; `PromptVersion` is 2. A model that fails sends the raw
  query to the text vector and leaves the photo list out. An embedding model
  that fails leaves its list out; `q` then runs on BM25.
- `make search-eval` runs the sample set in `internal/discovery/eval` (42
  listings with generated photos, 46 queries: 30 `q`, 10 `photo`, 6 `like`)
  against TEI (bge-m3 then), ai and the LLM in `.env`. The baseline of 2026-11-10, with the
  starting weights, floors 0.45 (text) and 0.6 (photo),
  `bge-m3@5617a9f6` and `siglip2-so400m-patch16-384-fp32@29f74e67`:

  | Kind | Queries | recall@10 | nDCG@10 |
  |---|---|---|---|
  | `q` | 30 | 1.000 | 0.946 |
  | `photo` | 10 | 1.000 | 1.000 |
  | `like` | 6 | 1.000 | 1.000 |
  | all | 46 | 1.000 | 0.965 |

  The set is small, so recall@10 is easy to reach. The photo and `like`
  pages also list unrelated listings after the expected ones (a photo of
  white sneakers brings headphones and a washer), which the two metrics do
  not count; a precision measure and higher floors are the next step.

### Discovery

Module `discovery` holds saved listings, search and the nearby feed (see
Search).

- A person saves someone else's listing that is active or reserved, at
  most 500; the save keeps the price at that moment. Saving again changes
  nothing.
- `GET /v1/saved` lists the saves with each listing as it is now: those
  of listings still on sale first, then those sold, hidden or deleted,
  each group newest save first (decided 2026-11-20). The cursor stays the
  save's ID, and the query reads the cursor save's listing to know its
  group; a cursor whose save was removed counts as on sale. A listing
  hidden or deleted after the save shows its status alone, since its
  seller took it down. The price change since the save is the listing's
  `price` against `saved_price`; the client shows it.
- A saved listing carries its `category_code`, `condition` and commune name
  as a search item does (decided 2026-11-15), and a `SearchItem` carries
  `seller_id` and `category_code`, so the app files and groups both without
  reading each listing.
- Each save has a UUIDv7 `id`, so the list pages by it. The 500-save cap
  is counted under `pg_advisory_xact_lock` on the account.

- `search_text_vectors` caches the vector of a text the search embedded
  (the rewrite, the raw query when the model failed, or the visual
  description), keyed by the embedding model's version and the SHA-256 of
  the normalized text (decided 2026-11-23; it replaced
  `search_query_vectors`, which kept a row 7 days from its write and keyed
  it by the prompt version too). A vector never changes for its key, so a
  row leaves by use: 30 days after its write when no search read it again,
  180 days after its last read otherwise, and past 2,000,000 rows the rows
  read on the fewest days go first. A read counts at most once a day
  (`hits`, `last_used_at`), so most reads write nothing. The first search
  that writes a vector on a UTC day sends `SearchVectorSweep`, keyed by the
  day, which applies those rules. The table is unlogged: a crash empties
  it and searches embed again. A vector is about 2 KB, so 2,000,000 rows
  take about 4 GB.
- `search_photo_vectors` caches a search photo's vector until its upload
  expires; each write deletes the expired rows, and each read checks the
  upload again.
- No search photo or vector reaches a log or the journal.

Search history (decided 2026-11-10, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-server-account-erasure-feedback-search-history-design.md`):

- `Search` keeps the first page (no `cursor`) of a signed-in person's
  search with `q`, after the search has answered. A search with `photo` or
  `like`, a later page and a signed-out search keep nothing. A failed write
  is logged at WARN and the search still answers 200.
- An entry holds the query as typed (trimmed), the label of the first
  category chip and its code (see Screen, Dành cho bạn), and the first
  card's `cover_thumb_url`, all as text.
  `domain.HistoryKey` folds case, spaces and Vietnamese marks (đ as d), so
  "Xe Đạp" and "xe dap" are one entry. Searching it again gives the row a
  new UUIDv7 `id`, which moves it to the top; the same transaction deletes
  the rows past the 10 newest.
- `GET /v1/search/history` lists the 10 newest; `DELETE
  /v1/search/history` empties it and `DELETE /v1/search/history/{id}`
  forgets one, an unknown ID included; no endpoint restores either.

Hidden listings (decided 2026-11-11, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-11-server-search-watches-price-alerts-hidden-design.md`):

- `PUT /v1/hidden/{listing_id}` hides someone else's listing that exists and
  is not deleted (404 `listing_not_found`, 409 `own_listing`), and `DELETE`
  shows it again. Hiding again gives the row a new UUIDv7 `id`; the same
  transaction deletes the rows past the 1,000 newest, so the 1,001st hide
  shows the oldest again. The app has no screen of hidden listings, and no
  endpoint lists them.
- A signed-in search of any form, Home's nearby feed included, leaves out
  the hidden listings and the listings of every account on either side of
  the viewer's blocks (`accountsvc.BlockedWith`). Catalog's
  `Search.ExcludeListings` and `ExcludeSellers` carry them as `id <> ALL`
  and `seller_id <> ALL` in every list, in browsing and in counts;
  `ExcludeSeller` became `ExcludeSellers`. Saved listings, the seller page
  and the listing itself still show them.

Price alerts (decided 2026-11-11, same spec):

- The level is on the save, `saved_listings.alert_below`.
  `PUT /v1/saved/{listing_id}/alert {below}` saves the listing first, with
  the save's refusals, then sets the level and clears `alerted_price`.
  `DELETE` removes the level and keeps the save.
- A save without its own level uses its saved price as the level.
  Discovery writes `saved.price_dropped` by this rule alone, and
  notification's `price_cuts` switch stops only the push (see
  Notifications). `/v1/me/price-cut-alerts` and `saved_settings`, a switch
  that also stopped the notice, were removed on 2026-11-14.
- A save tells at each new low below its level: below the level and below
  `alerted_price`, the price it last told. A price at or above the level
  clears `alerted_price`. `domain.PriceStep` holds the rule;
  `Saves.PriceMoved` runs it in SQL on the row's own columns, so an UPDATE
  that waited for another reads the newest row.
- `DiscoveryListingWatch` hears `listing.updated` and `listing.published`,
  reads the listing's current price through catalog's `Summary`, and skips a
  listing that is not active. A late or reordered event weighs the price as
  it is when the handler runs. `PriceMoved` writes `saved.price_dropped`
  (subject the save, payload `price`) for each save it tells; a retried step
  finds `alerted_price` at the price and writes none.

Search watches (decided 2026-11-11, same spec):

- `POST /v1/search/watches` follows a `q` with its filters (category,
  price range, conditions, offers only) and a place. An account has one
  watch per `domain.HistoryKey` and at most 20 (`watch_limit_reached`,
  counted under `pg_advisory_xact_lock` on `'watches:' || account`). The
  same words again take the new filters, keep the ID and set `new_count` to
  0, with 200; a new watch is 201. `GET` lists them, `DELETE /{id}` forgets
  one, and `/mute` and `/unmute` set `alert`.
- A watch keeps a commune and no point: `near_commune` as given, or the
  commune nearest a GPS point (`catalogsvc.Locate`), refused past 30 km
  with `invalid_position`. `radius_km` is measured from the commune's
  point.
- The watch stores the phrases the model read when it was made, the raw
  query among them, so a sweep calls no model.
- `listing_arrivals` logs each first publication (`listing.published` with
  `first`) by `seq`, written by discovery's listing watch after the commit.
  `published_at` is the time the publish flow started, before the photos
  were processed, so a sweep that read by it would miss listings. The insert
  deletes arrivals older than 2 days.
- Restate service `SearchWatchSweep.Run(watch)` reads up to 500 arrivals
  past the watch's `last_arrival` that are at least a minute old, so an
  insert that took a lower `seq` has committed. Catalog's `Search` keeps the
  ones under the filters that hold every word of one phrase
  (`Phrase.AllWords`: each word in the title, description or specs, so "xe
  đạp" does not match "xe máy"), less the watcher's own, hidden and blocked
  listings. `Swept` moves `last_arrival` and adds the matches to
  `new_count`, at most 99, when `last_arrival` is still where the sweep read
  it; with matches and the alert on it writes `search_watch.matched`
  (payload `count`, the new `new_count`).
- A full batch sends the next run at once, any other 30 minutes later, and
  a watch gone ends the chain. The first run is sent with the watch's ID as
  idempotency key before the row exists, as an upload's expiry is. A watch
  made again after a delete has a new ID and a chain of its own.
- `GET /v1/search?watch=<id>` runs the watch's query and filters as a full
  hybrid search. Only `cursor`, `limit`, `sort` and `off` go with it
  (`search_conflict`), another account's watch is `watch_not_found`, and the
  first page sets `new_count` to 0. `SearchView.watch_id` names the viewer's
  watch of a `q`.
- `Watchers` matches a listing not yet posted against the watches, for
  the price guide. SQL takes the watches of active accounts whose category
  is none, the draft's or its root and whose conditions are none or hold
  the draft's, muted ones too, without the seller's own or a blocked pair
  either way, oldest first, at most 5,000. Go keeps those whose area holds
  the draft's commune point (the box, then the distance, as the sweep's
  search measures it) and one of whose phrases (the raw query without
  phrases) `catalogdomain.HoldsAllWords` finds in the draft's words. At
  most 200 rows, each the watch's price bounds and `offers_only`, nothing
  that names the buyer; the app counts them at its price.
- The draft's title, description and specs are split by catalog's `Words`,
  which runs them through the index's own tokenizer
  (`pdb.unicode_words`), so "15.6" or "1.200.000đ" stays one word as in
  the index; the phrase is split as the sweep splits it.
  `TestHoldsAllWordsAgreesWithTheIndex` runs both against the index.

### Assist

Module `assist` holds what AI does for the person using the app: it reads
a seller's video, one photo of theirs, or their listing on Chợ Tốt into a
listing draft, through one resource (decided 2026-11-11, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-11-listing-reads-design.md`;
the video read was first, 2026-11-10, spec
`~/shopnexus/docs/superpowers/specs/2026-11-10-video-read-design.md`).
Spam and scam detection is moderation and goes to trust; the two share
`provider/llm`.

- `POST /v1/listing-reads` takes one of `{video, transcript?}`, `{photo}`
  and `{link}` with an `Idempotency-Key`, and answers 202 with the read.
  Each source has its own Restate service, `VideoRead`, `PhotoRead` and
  `LinkRead`, whose handlers read top to bottom; all three keep
  `domain.ListingRead` in table `listing_reads`, which has each source's
  own columns. The caps are 30 video reads, 60 photo reads and 30 link
  reads in 24 hours, each counted under `pg_advisory_xact_lock` on the
  account and the source, all `read_limit_reached` 429. A body naming none
  or two sources is `invalid_request` 400. `GET /v1/listing-reads/{id}`
  builds one view for every source and is `listing_read_not_found` 404 for
  another account's read or one past `expires_at`. Each save notifies the
  account with `listing_read`.
- `GET /v1/listing-reads/{id}/events` is a server-sent event stream
  (decided 2026-11-19): event `read` with the read as the GET answers it,
  event `text` with the title and description each time the model has
  written more (at most every 100 ms), and `read` again when the read ends.
  The text travels on `platform/relay`, Postgres channel `relay`, whose
  message is the whole text so far, so a lost message costs nothing the
  next one does not carry. The handler extends the write deadline for each
  event, sends a comment every 15 s, and closes 30 s after the read's
  2-minute deadline at the latest.
- Migration 0039 copied the rows of `video_reads` and `link_reads` and
  their updates into `listing_reads` and `listing_read`; those tables and
  `account_updates.video_read_id` and `link_read_id` stay, since the
  release before writes them while the migration runs, until a later
  release drops them.

The video read:

- `{video}` takes one of the caller's video uploads. Restate service
  `VideoRead.Start`, key `<account id>/<Idempotency-Key>`, refuses a
  suspended account and one without a phone (`phone_required`, as a listing
  post does: a read exists to post a listing, and accounts made of an email
  alone would otherwise reach the model), then inserts the row. The insert's transaction takes
  a share lock on the caller's `accounts` row (a tombstone is
  `session_revoked`) and `pg_advisory_xact_lock` on the account, and counts
  the account's video reads of the last 24 hours. `Start`
  then holds the video (another holder is `invalid_upload`, and the row is
  deleted again; a hold that loses a race to another start's save runs
  again and then sees that holder) and sends `Expire` with a 24-hour delay and `Run` one way,
  both keyed by the read's ID.
- `Run` has two model steps, and each saves the row. Frames: module ai's
  `Frames` gets the video's object store key from media's `VideoKey`, and each frame it keeps becomes a waiting photo
  upload of the seller (`SaveFrame`), so the draft posts with
  `POST /v1/listings` like photos the app uploaded. Fill: the phone's
  transcript when the read has one, else module ai's `Transcribe` of the same key, and the LLM in one
  step, so the transcript never reaches the journal; the LLM gets the
  frames at 768 px and the transcript with where it came from, and streams
  JSON in `domain.ReplySchema` (decided 2026-11-20; plain text read as no
  item too often): `subject`, `found`, `title`, `description` and `reason`,
  a Vietnamese sentence for the seller when `found` is false. The text
  reaches the relay as the model writes it (`domain.PartialText`). A photo
  or video reply with `found` false is asked once more.
- The category is catalog's `GuessCategory` of the title and description;
  a guess that fails leaves it empty for the seller to pick.
  `domain.VideoDraft` squeezes the title and cuts it at 70 characters and
  the description at 1,500. The draft is `used`, with no specs, no price
  and `allow_offers` false; the seller fills the category's `spec_labels`,
  the price and the rest. Its photos are the sharpest frame, then the
  others in time order, 6 at most. `found` false or an empty title fails
  the read with `no_item` and the model's `reason` as `failure_reason`. An
  answer that is not the JSON is retried like a model error. The
  description restates the seller's words only, and the prompt forbids
  naming a mark the seller did not name (`app/mohinh.md`, section 1).
  `PromptVersion` is 6.
- The reads do not price the draft (decided 2026-11-19); the seller types
  the price, and `POST /v1/price-guides` still prices a draft on request.
- A refusal from `Frames` fails the read: `unreadable` (also for a key with
  no object), `video_too_short` (under 3 s) or `video_too_long` (over 35 s);
  any other refusal is the request's fault, `unavailable`, and logged. A
  video with no audio track reads without speech. `Transcribe`'s too long or
  any other refusal also reads the video without speech, since the frames
  step already read it, and is logged. One from the LLM
  (`llm.ErrRefused`, any 4xx but 402 and 429) is `unavailable` at once. An
  `unavailable` from the model keeps a `failure_reason`: the status and
  message the model's error gave, or that it timed out or answered
  something other than the JSON. The run has a 2-minute deadline from
  `created_at`. Each model step runs with `restate.WithMaxRetryDuration` set
  to the time left and at most 3 attempts, since the SDK retries a step
  every 2 s at most; a frames or fill step that runs out, or would start
  after the deadline, saves `failed` with `unavailable`. Every end consumes
  the video before it
  saves the end, so a read that shows it ended holds no video.
- The read's `photos` are its frames with `at_seconds`. Frame URLs
  are presigned GETs valid for an hour; a frame posted in a listing loses
  its original under `uploads/` when the listing's photos are processed, so
  its URL stops working. `Expire`, sent with the read's ID and its video's,
  consumes the video of a run that never ended, or of a read whose row is
  already gone (an account deleted between `Start` and `Run`), and deletes
  the row after 24 hours; frames not posted expire with
  their uploads.
- The phone's transcript is at most 1,000 characters (the schema's
  `maxLength`) and is taken as the seller's words. The use case keeps it,
  trimmed, in `listing_read_transcripts` under the account and the
  `Idempotency-Key` before it calls `VideoRead.Start`; the insert moves it
  into `listing_reads.transcript`, and every insert first deletes waiting
  ones older than 10 minutes. Once `Start` answers, the use case deletes
  its own waiting one, so a retry's or a refused start's does not stay. The
  save that ends the read sets `listing_reads.transcript` to NULL. A repeated key
  answers the first read with the first transcript. No transcript is ever
  logged, returned or in a journaled value. The fill's log line
  holds the prompt version, the frame count, the transcript's length in
  characters and `transcript_from` (`phone`, `server` or `none`), whether there was an
  item, the category, the count of photos kept, the description's length
  and the time taken.
- Configuration: `ASSIST_LLM_BASE_URL`, `ASSIST_LLM_API_KEY`,
  `ASSIST_LLM_MODEL`, `ASSIST_LLM_REASONING_EFFORT` (sent as
  `reasoning_effort` when set), and `ASSIST_CALL_TIMEOUT` for one call, ai's calls
  included. In the cluster the key is LiteLLM's `server-assist`. The
  price guide embeds text with the search key, `EMBED_SEARCH_API_KEY`, and
  photos with module ai.

The photo read (decided 2026-11-11, same spec as listing reads) reads one
photo, the app's cover, and costs one image and no speech to text:

- `PhotoRead.Start` refuses a suspended account and one without a phone,
  inserts the row under the photo cap, then checks the photo with media's
  `QueryUpload` (the caller's, a photo, waiting, before its expiry); a
  refusal is `invalid_upload` 422 and deletes the row again. The read
  holds nothing: the listing posts the photo.
- `Run` has one step. It sends the photo at 768 px with
  `photoSystemPrompt` (`PhotoPromptVersion` 5) and streams the JSON reply;
  `domain.PhotoDraft` cleans it as the video read does, keeps the photo as
  the draft's one photo and cuts the description at 300 characters. The description says what the photo shows and never names a
  mark or a fault, or anything a photo cannot show. A photo no longer
  waiting at the fill (posted, expired) is `unreadable` without the model.
  `Expire` deletes the row after 24 hours. The log line has no title or
  description, only the description's length.

The link read reads the seller's own listing on Chợ Tốt into a
listing draft (decided 2026-11-11, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-11-server-link-read-design.md`).
The server reads only Chợ Tốt, through its public gateway, and takes the
seller's word that the listing is theirs: the app says "Chỉ dán tin của
bạn", and nothing compares the listing's owner with the account.

- `provider/chotot` reads `GET {CHOTOT_GATEWAY_URL}/v1/public/ad-listing/{list_id}`
  and decodes the subject, body, price, category name, photo URLs and
  parameters, and nothing else. The seller's name, phone, address,
  coordinates and the parameters that place them are never decoded. A
  listing that is absent or not `active` is `ErrNotFound`; 5xx, 403, 429, a
  redirect or a timeout is `ErrUnavailable`; an answer without a subject or
  photos is `ErrFormat`, logged as an error. A photo is read only from the
  scheme and host of `CHOTOT_CDN_URL` (another URL is `ErrOffCDN`), without
  redirects, at most 15 MB, and only when its magic bytes say JPEG, PNG or
  WebP; a 5xx, 403 or 429 from the CDN is `ErrUnavailable`.
- `domain.ParseLink` takes `chotot.com` and its subdomains, finds the
  listing ID in the last path segment, drops the query and the fragment,
  and rewrites the link to `https://www.chotot.com/<id>.htm`. Text that is
  not an http or https URL is `not_a_link` 422; another site, or no listing
  ID, is `unsupported_source` 422.
- `POST /v1/link-previews {url}` answers at once with the listing's title,
  price, first photo on the CDN and photo count, for the card under the
  paste field. It stores nothing and needs no phone; it is POST so the link
  stays out of access logs. Chợ Tốt down is `source_unavailable` 503. Its
  rate limit tier is `costly`.
- `{link}` takes what the seller pasted, at most 2,048 characters;
  `not_a_link` and `unsupported_source` answer before the account checks.
  Restate service `LinkRead.Start` refuses a suspended account and one
  without a phone, then inserts the row under the link cap, and sends
  `Expire` with a 24-hour delay and `Run`. The link is not fetched until
  `Run`.
- `Run` has two steps under the video read's rules (a 2-minute deadline,
  at most 3 attempts a step, a save after each).
  Read: the gateway and the model in one step, so the listing's text never
  enters the journal; the model gets the subject, the parameters, Chợ Tốt's
  category and the body, without photos, and streams the text
  (`LinkPromptVersion` 4). `domain.LinkDraft` cleans it and guesses the
  category as the video read does; the price is the listing's own, kept
  between 10,000 and 1,000,000,000 đồng; `StripPhones` takes out what still
  looks like a Vietnamese phone number and sets `contact_removed`. While the
  text streams, `domain.LinkText` also hides a number still being written
  at its end. Copy: the first 10
  photos, each saved with `SavePhoto(origin = link)` under an ID minted
  before the step; they are the read's `photos` and the draft's. A photo the CDN does not have or that is not an image
  is skipped; one it does not answer (5xx, 403, 429, a timeout) gets two
  tries and is then skipped. With none copied, a photo that did not come
  sends the step round again, photos off `CHOTOT_CDN_URL` fail the read
  `unavailable` with an error log, and otherwise the read is
  `listing_not_found`. The step logs the counts copied and skipped.
- A failed read says `listing_not_found`, `no_item` or `unavailable`.
  `GET /v1/listing-reads/{id}` shows its `link` (site, URL, photo count),
  with presigned photo URLs valid for an hour. `Expire` deletes the row after 24 hours;
  copies not posted expire with their uploads.
- The read step's log line holds the prompt version, the list ID, the
  photo count, the body's length, Chợ Tốt's category, whether there was an
  item, the category, `contact_removed`, the price and the time taken. No
  subject, body, title, description or link is logged.
- Configuration: `CHOTOT_GATEWAY_URL` and `CHOTOT_CDN_URL`, with the
  video read's `ASSIST_LLM_*` and `ASSIST_CALL_TIMEOUT`.

The price guide (decided 2026-11-13, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-13-price-guide-design.md`)
prices any draft for the app's "Giá" step, with or without a read:

- `POST /v1/price-guides` takes the draft's title, category, condition,
  specs, description, commune and up to 3 photos, answers 200 and stores
  nothing. It has no cap of its own; its rate limit tier is `costly`. Checks, in order: `account_suspended` 403,
  `invalid_listing` 422 for a category that is not a subcategory or an
  unknown commune, and `invalid_upload` 422 for a photo that is not a
  waiting upload of the caller's. The photos are checked before any model
  runs.
- `draftSample` in `estimate.go` is the one path from a draft to a price
  sample: the embedding text from catalog, the photos' normalized mean
  vector, catalog's `PriceSample`. The reads' `marketEstimate` calls it
  inside a Restate step and the guide calls it directly, so a read's range
  and the guide's agree for the same draft. A model that fails leaves its
  list out, as before.
- Catalog's sample carries each listing's ID, its whole days from
  publication to sale (at least 1, 0 on sale) and its point.
- `domain.Suggest` gives "Giá thị trường" at the range's median, with the
  median days of every sale in the sample, and "Bán nhanh" at the median
  price and days of the sales that took at most that median, left out
  unless it is below the market price. Under 3 sales there are no
  suggestions; under 5 listings there is no range either.
- The guide lists the first 12 of the sample in vector order, with `km`
  from the draft's commune point; a listing gone since the sample, or one
  of a seller blocked either way, is left out (their prices still count). `sold_prices` and `live_prices` are the sample's, ascending.
  `watchers` comes from discovery (see Search watches).
- A sample that fails is logged and gives a guide without a comparison;
  the watchers still answer. The log line holds the category, condition,
  photo count, sample size, sales, estimate source, suggestion and
  watcher counts and the time taken, and no title, description or specs.

### Module ai

Module ai (decided 2026-11-18, plan:
`~/shopnexus/docs/superpowers/plans/2026-11-18-ai-into-server.md`) runs the
self-hosted models on CPU: Gipformer 1.5 with Silero VAD for speech to text
(`internal/ai/speech`, sherpa-onnx), SigLIP 2 so400m-patch16-384 in fp32 for
photo vectors (`internal/ai/embed`, onnxruntime) and ffmpeg for video frames
(`internal/ai/frames`). The code came from the `ai` repo without its HTTP
API.

- The Restate service `ai` has three handlers. `Transcribe` takes
  `{"key"}`, an object store key, and answers `{"text"}`. `Frames` takes
  `{"key"}` and answers `{"duration", "frames"}`, at most 8 frames of
  `at_seconds`, `sharpness` and a base64 `jpeg`. `EmbedImages` takes
  `{"inputs"}`, at most `AI_EMBED_MAX_INPUTS` JPEG, PNG or WebP data URLs,
  and answers `{"vectors"}`. ai copies the object to a temp file, since
  ffmpeg needs to seek in an mp4 whose index is at the end.
- Every error ends the call as a terminal error, so Restate does not retry
  it, with a code per error of `ai/domain`: 415 unreadable, 410 no object at
  the key, 422 too short, 413 too long, 400 any other refusal (a bad image,
  too many inputs, photo vectors off), 500 a model or ffmpeg failure. The
  handlers journal nothing; a retried invocation runs the model again.
- Callers use `ai/service.Client`, which posts to Restate's ingress
  (`RESTATE_INGRESS_URL`, `/ai/<Handler>`) in every role and turns the code
  back into the `ai/domain` error. A 500 or an ingress error, such as
  service ai not registered yet, comes back as a plain error for the caller
  to retry. `ai/service.Photos` is the `embed.Model` catalog, discovery and
  assist take for photos.
- `SERVER_ROLE` picks what a process runs. `all`, the default and compose's,
  runs every module and module ai. `api`, the cluster's Deployment
  `server`, loads no model and binds no service `ai`. `ai`, the cluster's
  `server-ai`, loads the models and serves only service `ai` and, on
  `HTTP_ADDR`, `/healthz` and `/readyz`; it opens no database.
- Configuration, read in every role: `AI_STT_MODELS` and `AI_EMBED_MODELS`,
  one `name=dir` each (an empty `AI_EMBED_MODELS` loads no SigLIP 2 and
  turns photo vectors off), `AI_VAD_MODEL`, `AI_NUM_THREADS`,
  `AI_MAX_CONCURRENT`, `AI_MAX_AUDIO_SECONDS`, `AI_EMBED_MAX_INPUTS`,
  `AI_EMBED_MAX_CONCURRENT`, `AI_EMBED_MAX_IMAGE_MB`,
  `AI_FRAMES_MAX_CONCURRENT`, `AI_FRAMES_MAX_SECONDS` and
  `AI_FRAMES_MIN_SHARPNESS`. `EMBED_IMAGE_VERSION` names the stored
  vectors' model.
- The models (about 5 GB) are not in the image. `scripts/fetch-models.sh
  <dir>`, shipped as `/fetch-models.sh`, downloads each file that is missing
  or fails its sha256, and skips SigLIP 2 with `FETCH_EMBED=0`; the stacks
  run it into `/models`, and `make models` into `tmp/models` for `make run`.
- The binary links `libsherpa-onnx-c-api.so` and `libonnxruntime.so` through
  cgo. A build from source finds them through the runpath into the Go
  module cache; the image copies them to `/usr/local/lib` and runs
  `ldconfig`. onnxruntime_go opens `libonnxruntime.so` by name and gets the
  copy already loaded.
- Model tests read `AI_TEST_MODELS` (default `/models`) and `AI_TEST_WAVS`
  and skip when the files are missing. Other tests give app fake engines
  through `app.WithAIEngines` and `ai/aitest.Fake`.

### Support

Module `support` keeps tickets, what people ask ShopNexus (spec:
`~/shopnexus/docs/superpowers/specs/2026-11-13-support-tickets-design.md`,
decided 2026-11-13). It replaced the feedback of 2026-11-10: migration 0044
turned each feedback row into an open ticket of kind `other` and dropped
the table, `POST /v1/feedback` and `GET /v1/console/feedback`.

- Every ticket is its own chat between the person and the system account
  ShopNexus, `accountdomain.ShopNexusID`, which 0044 made with no identity
  so nobody signs in to it. The chat is a chat of module chat with kind
  `support`; support stores no messages of its own.
- Three kinds: `not_received` (the buyer's report of an order that is
  `shipped`, or `completed` by the system less than 7 days ago, read from
  `orders.completed_by`; one ticket per order), `carrier_request` (a carrier
  name of 1 to 50 characters, folded into `carrier_key` to count requests
  for one carrier) and `other` (needs a first message, a text or a photo).
  `TicketOpen` takes the first message as a message does, `text` and
  `photos`.
- `POST /v1/support/tickets` runs the Restate flow `TicketOpen` under
  `<account>/<Idempotency-Key>`. One step reads the account (a suspended
  one may still open a ticket), the order's ticket and the order through
  deal; then steps open the support chat, insert the ticket under
  `pg_advisory_xact_lock` on the account (at most 10 in 24 hours, 429
  `too_many_tickets`), send the person's first message through chat's
  `MessageSend` with `OpensTicket` so it may carry no text, and record it as
  `first_message_id`, which the app draws as the ticket's card; Hỗ trợ's
  list carries that message as `first_message`, from chat's `Threads`. The first
  step also counts the day's tickets, so a request past the limit opens no
  chat; one that passes it in a race is refused at the insert and leaves an
  empty support chat, which no list shows. A retried insert step finds its
  own row and goes on to send the first message.
- The ticket is `open` or `resolved`. Support's worker follows
  `message.sent`: the person's message reopens a resolved ticket (event
  `reopened`) and sets `last_from = person`, which the console shows as
  "Chờ trả lời"; ShopNexus's message sets `shopnexus`. `account.deleted`
  resolves the account's open tickets.
- Members answer as ShopNexus: support calls `chat.Send` with
  `ShopNexusID` as the sender, and `ticket_replies` records the member of
  each message, with no member for an automatic one. Resolving a
  `carrier_request` sends "Đã ghi nhận hãng <name>." before the status
  moves, keyed by the resolving, so a failed note is sent again by the next
  "Đã xử lý" and goes out once; for a deleted account the ticket resolves
  without it. A member opening a ticket in the console reads its chat for
  ShopNexus, so the person sees "Đã xem". Reply, resolve and
  reopen write `ticket.reply`, `ticket.resolve` and `ticket.reopen` to the
  audit log with `target_kind = ticket`.
- The console's routes are console's (`/v1/console/support/*`), calling
  `supportsvc.Service`, as feedback did. Support mounts after chat and deal,
  and before console and screen; screen's chat screen adds the ticket of a
  support chat (`ChatScreen.ticket`), since chat does not import support.
- The ShopNexus account cannot be suspended, unsuspended, erased, blocked
  or reported (409 `account_is_system`), and the console's account list
  leaves it out.

### Console agent

Console members ask an agent about a ticket or an account (UC-MOD-08,
decided 2026-11-16, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-16-console-agent-design.md`).
The agent loop is its own service, `shopnexus/agent`, on the internal
network (ADR-0003). The server is the agent's only way in and its only
source of data: the console calls the server, the server starts each run
on the agent, and the agent reads through the server's MCP. Since
2026-11-18 a turn is a run the member watches on a server-sent event
stream, with pause, steering, clarifying questions, numbered sources,
documents and skills (spec:
`~/shopnexus/docs/superpowers/specs/2026-11-18-console-ai-synthesis-design.md`).
Since 2026-11-19 the agent keeps no state: the server stores every
conversation, turn, event, tool call, document and skill, and the agent
streams each run segment back (spec:
`~/shopnexus/agent/docs/superpowers/specs/2026-11-19-stateless-runtime-design.md`).

- Module `agent` has `handler` (the console's agent endpoints and the AI
  settings), `mcp` (the MCP server, its tools and a run's control),
  `service` (the conversations and runs, the run client to the agent
  service, the read record and the AI settings over LiteLLM) and
  `repository` (`console_agent_reads`, the `ai_*` tables and the
  `agent_*` run tables). `mcp` is
  a layer of its own in depguard: it imports a module's service and
  domain, as a handler does, and nothing imports it but the module's
  `Mount`. `Mount` takes `module.Deps`, `Settings`, console's moderator
  and admin guards and service, and the services of support, account,
  catalog, deal, shipping and trust. `console.Mount` returns
  `console.Mounted`, the service, `Moderator` (`FromConsole` then
  `Require(..., moderator)`) and `Admin` (the same with the admin floor),
  so a module outside console can serve a console route.
- Configuration, all required: `AGENT_BASE_URL`, `AGENT_SERVICE_TOKEN`,
  `AGENT_MCP_ADDR`, `AGENT_MCP_TOKEN`, `LITELLM_URL` (LiteLLM's base URL
  without `/v1`) and `LITELLM_MASTER_KEY`. Each agent token has at least
  32 characters; `cmd/envgen` writes both, 32 random bytes in URL-safe
  base64, and the agent's environment takes the same two. No log line
  carries a token or the master key.

#### The console's agent endpoints

Behind `FromConsole` and a moderator's or an admin's session. A record has
one conversation, which every member shares and the first `POST` for it
makes; a conversation without a record is its creator's, and another
member gets 404 for it. A list takes a subject, a subject kind alone, a
mentioned record or nothing; a new conversation takes both or neither (422
`agent_subject_invalid`). `mention_kind` and `mention_id` go together and
without a subject (400 `invalid_request`), and list the caller's own
conversations in which a question mentioned that record: the console's
chats about a ticket are such conversations, each with no subject. A
member renames or deletes their own conversation, an admin any; a shared
one is an admin's (403 `agent_forbidden`). A delete takes the turns, events and documents with it
and is refused while a turn is unfinished (409 `agent_turn_in_progress`).

| Console | Answer |
|---|---|
| `GET /v1/console/agent/conversations?subject_kind&subject_id&mention_kind&mention_id` | `{items}`, newest activity first, at most 50 |
| `POST /v1/console/agent/conversations` | the conversation, found or made |
| `GET /v1/console/agent/conversations/{id}` | the conversation with its turns, oldest first, each the turn view |
| `POST /v1/console/agent/conversations/{id}/turns {text, agent_id?, attachments?, skill_id?}` | 202 `{conversation_id, turn_id}` |
| `GET /v1/console/agent/turns/{id}` | the turn view: status, answer, blocks, sources, outcome, error code, continuable, agent, usage, steers, questions while it waits, `last_seq` |
| `GET /v1/console/agent/turns/{id}/events` | `text/event-stream` |
| `POST …/turns/{id}/pause`, `stop`, `continue` | 202 `{turn_id}` |
| `POST …/turns/{id}/end` | 200 `{turn_id, status: cancelled}` |
| `POST …/turns/{id}/steer {text}` | 202 `{steer_id, turn_id, resumed}` |
| `POST …/turns/{id}/input {answers}` | 202 `{turn_id}`; `answers` maps a question's id to an option's id or a list of them |
| `GET /v1/console/agent/tool-calls/{id}` | the call's input, output, attempts and timing |
| `GET /v1/console/agent/documents/{id}` | the document with every version, newest first |
| `GET`, `POST /v1/console/agent/skills`, `GET`, `PATCH`, `DELETE …/skills/{id}` | the skills every member shares; a change by a moderator to another member's skill is 403 |
| `GET /v1/console/agents` | the console agents turned on, from `ai_agents`, the Console assistant marked `default` |

A question names a console agent by `agent_id`, the Console assistant
(`domain.ConsoleAssistant`, seeded by migration 0056) when it names none.
The server reads the agent from `ai_agents`: an unknown one or an app
agent is 404 `ai_agent_not_found`, one turned off 409 `ai_agent_disabled`.
Then it checks the month's cap (429 `agent_budget`), the rate limit of 30
questions and updates a minute per member, kept in each replica's memory
(429 `agent_rate_limited`), and the input guard: a question empty once
zero-width and control characters and HTML tags are taken out is 400
`invalid_request`, one over 2000 characters 422 `agent_input_rejected`.
`attachments` are the records the member mentioned (`@`) in the question,
`{kind, id, label?}`: at most 10, a kind of `ticket`, `account`, `order`,
`listing`, `report_case` or `document`, a label of 120 characters at most
once trimmed, or 400 `invalid_request`. The turn keeps them in
`agent_turns.attachments` (migration 0065), each record once, and the turn
view returns them.
The turn opens `running` under the conversation's row lock; a closed
conversation is 409 `agent_conversation_closed`, one with an unfinished
turn 409 `agent_turn_in_progress`. The run goes out with
`agent_config {id, name, alias, instructions, instructions_version, tools,
limits}`, so the server stays the only source of a console agent's
configuration. `limits` are `deadline_s` (10 to 300, 60 by default),
`max_model_calls`, `context_window`, `tool_timeout_s`, `tool_attempts` and
`max_tokens`.

#### Runs

Tables (migration 0061): `agent_conversations`, `agent_turns` (one
unfinished turn per conversation by a partial unique index; `state` is the
agent's checkpoint, read only for `pending_ask` and `sources`),
`agent_turn_inputs` (pause, stop, steer and answers, by an identity
`seq`), `agent_tool_calls`, `agent_turn_events`, `agent_documents`,
`agent_document_versions` and `agent_skills`. Columns that would be SQL
keywords are `input_text`, `provider_name`, `tool_input`, `tool_output`,
`result_content`, `tool_timing`, `event_type`, `occurred_at`,
`version_no`, `skill_name` and `skill_variables`.

- A segment starts with `POST {AGENT_BASE_URL}/runs` and
  `Authorization: Bearer <AGENT_SERVICE_TOKEN>`: the turn, the member who
  acts and their role, `mode` `new` (the question, the subject, the skill
  and the last 20 terminal turns as history) or `resume` (the last
  checkpoint, the unread steers and the answers text), the conversation's
  documents and `control_after`, the turn's newest input seq. Both modes
  send `agent_config`. The server waits 10 s for the status; anything but
  200 is 503 `agent_unavailable` and takes the new turn away, or puts a
  resumed turn back as it was.
- Both modes also send `mentions`, `[{kind, id}]`: the records of every
  turn's attachments in the conversation, the current turn's first, then
  the other turns' from the newest, each once, at most 20. Labels stay on
  the server; the agent's prompt takes only values the server owns. A GIN index
  (`jsonb_path_ops`) on `agent_turns.attachments` serves the list by
  mention.
- On 200 the server reads the NDJSON lines in the background on the same
  replica, until 60 s past the segment's deadline, each in its own
  transaction and only while the turn is `running`: `event` takes the
  turn's next `event_seq` and rings `pg_notify('agent_turn', turn_id)`
  (`steer.read` marks its steer read); `checkpoint` stores `state`;
  `tool_call` upserts by `(turn_id, tool_use_id)` and never moves a
  finished call back; `document` inserts version 1 or the next version;
  `end` sets the status, the answer, the usage and `wrote`, and writes its
  closing event in the same transaction. A stream that ends without `end`
  closes the turn `failed` with `agent_stuck`.
- Pause and stop need a `running` turn and go in `agent_turn_inputs`, as
  does a steer to a running turn. The agent polls
  `GET /agent/turns/{id}/control?after=<seq>` on the MCP listener with
  `AGENT_MCP_TOKEN` and gets `{items: [{seq, kind, steer_id?, text?}],
  running}`; a turn that is gone or ended answers `running: false`.
- Continue (a paused turn, or a failed or timed out one that is the
  conversation's latest, wrote something and failed with a continuable
  code), answers and a steer to a paused turn set the turn `running`
  under its lock with a new `run_started_at`, then start a `resume`
  segment and write `turn.resumed {by}`. End cancels a paused or waiting
  turn with `turn.finished`.
- `Mounted.Run` keeps one `LISTEN agent_turn` per replica, which rings the
  open streams of that turn, and closes turns still `running` 30 s past
  `run_started_at + deadline_ms` as `agent_stuck`, at start and every
  minute.

The event stream replays `agent_turn_events` after `Last-Event-ID` (or
`after` in the query, for a page that opens the stream itself), then reads
the table again at each ring; every 15 s without one it sends
`: keep-alive` and reads the table. Each event is `id: <seq>`,
`event: <type>` and `data: {seq, type, at, …}`; the schema `AgentRunEvent`
lists the types. The stream ends after `turn.finished`, `turn.paused` or
`clarify`, or 10 s after the turn stopped running with nothing new; after
Continue, answers or an update to a paused turn the console opens it again
from its last seq. The handler flushes at each blank line, lifts the HTTP
server's 30-second write deadline with `http.ResponseController`, and
sends `Cache-Control: no-cache` and `X-Accel-Buffering: no`. A member who
closes the tab ends the stream; the run goes on.

The admin's tool, guardrail and usage figures read `agent_tool_calls`,
the `guardrail` events and `agent_turns` over the range's UTC days. A
run's own failures (budget, timeout, a model or the MCP server down) reach
the console as `turn.finished`. No request or answer body is logged, since
a turn quotes people.

#### The record pages

`GET /v1/console/orders/{id}` and `GET /v1/console/listings/{id}` are the
console's order and listing pages, behind the moderator guard. Module
agent serves them because it holds the read record and the services the
MCP tools read through; `service.Order` and `service.Listing` read the
facts `order_get` and `listing_get` read, through the same calls, and add:

- to an order, the buyer's and seller's `ConsoleAccount`, the seller's
  sold listings (catalog's `SellerCounts`) and trust's `Rating`, the
  category's name, and the tickets about the order: support's
  `TicketsOf` the buyer, kept where the ticket's order is this one;
- to a listing, the seller's counts, the member who hid it (the latest
  `listing.hide` in the listing's console actions, when no
  `listing.unhide` is later and the listing is hidden), the reports about
  it in trust's `ReportCase` of the seller, whose id is the case's
  (`report_case_id`), and catalog's `MarketPrice` as `price_guide`, left
  out for an empty sample.

The tickets' last lines and the reports are what people wrote, so a page
that shows any records the read in `console_agent_reads` under the tool
name `console_order` (the tickets) or `console_listing` (the seller, the
reports and their reporters). ShopNexus keeps no carrier events and no
reason for a hold, so neither page has them. An unknown order is 404
`order_not_found`, an unknown or deleted listing 404 `listing_not_found`.

#### The MCP server

- Streamable HTTP at `/mcp`, official Go SDK
  (`github.com/modelcontextprotocol/go-sdk` v1.8.0), on a listener of its
  own at `AGENT_MCP_ADDR`, never on `:8080` and never behind the ingress.
  `agent.Mount` opens the listener, so an address in use stops the start;
  `app.New` runs it on the app's `sync.WaitGroup` and `App.Close` shuts it
  down, waiting 10 seconds at most for calls in flight. It is stateless:
  every POST is one request on a session of its own, answered as JSON.
- Trust: a call carries `Authorization: Bearer <AGENT_MCP_TOKEN>`,
  compared in constant time, and `X-Console-Member`. The middleware reads
  the member through `consolesvc.Service.ActiveMember` on every call, so a
  member disabled between two tool calls gets nothing from the second;
  an unknown id, a disabled member or a wrong token is 401 before any MCP
  handling, logged as `security: agent MCP call refused` without the
  token. The member and role go in the context, and each tool takes them
  from there. Every member role may use every tool of this phase; a tool
  for admins alone will check the role itself.
- The tools only read, each a thin adapter over a module's `Service`; no
  tool reads a table. Each declares `readOnlyHint`, an input and an output
  schema (inferred from its Go types, a UUID as a string), and returns the
  ids the agent cites as sources. The SDK sends a result as structured
  content and as its JSON in one text block, which the agent reads; ids
  sit under keys `id`, `*_id`, `ids` or `*_ids`, which is how the agent
  finds a source's ids, and the tool's title is the chip's label. The
  agent drops a tool without `readOnlyHint`. A refusal reaches the agent
  as a tool error with its code; any other failure as `internal error`,
  logged.

- Tools that read one record take its id. List tools take `cursor` and
  `limit` (1 to 50, default 20) and answer `{items, next_cursor}`; the
  agent passes `next_cursor` back as `cursor` for the next page. A value
  from a fixed set (a ticket's status or kind, a case's status, a party, a
  pane, an entity) has an enum in the input schema, read from the owning
  module's constants.

| Tool | Reads | Records a read |
|---|---|---|
| `count` | the module's count by status of one entity, `accounts`, `listings`, `orders`, `tickets` or `report_cases`, from `since` on or for all time, with the total | no |
| `lookup` | which records have an id: account's `ConsoleAccount`, trust's `ReportCase`, deal's `Order`, catalog's `Summary` and support's `Detail`, all at once; each match is a kind, a few words and the tool that reads it | no |
| `ticket_get` | support's `Detail`: the ticket, its person, its order, the first page of its chat, the person's other tickets | ticket, its person |
| `ticket_messages` | support's `Messages`, a page of a ticket's chat | the ticket |
| `tickets_list` | support's `Queue`: open or resolved tickets, of one kind or all, each with its chat's last line | the tickets and their persons, when there are any |
| `tickets_of_account` | support's `TicketsOf`: the newest 50 tickets the account opened or that are on orders it sold, each with its chat's last line | the account and the tickets, when there are any |
| `account_get` | account's `ConsoleAccount` and `Identities` (phone and email as flags), catalog's `SellerCounts`, deal's `BuyerOrders`, the suspensions among the latest 50 console actions | no |
| `accounts_list` | console's `ListAccounts`: newest accounts, those suspended now with their total, or the one account an id or a phone number names | no |
| `orders_of_account` | deal's `ListOrders` as buyer or seller; no recipient | no |
| `listings_of_account` | catalog's `ListMine`, on sale or sold | no |
| `console_actions` | console's `ListActions` by the record acted on, by the member, or all, without the members' IPs | no |
| `order_get` | deal's `Order` and, for a shipment on the seller's carrier account, the carrier's status now through shipping's `Lookup`; no pickup and no recipient | no |
| `listing_get` | catalog's `Summary`, then `Get` as its seller, so a hidden or sold listing shows | no |
| `price_guide` | catalog's `MarketPrice`: the listing's price, the sample's median and size | no |
| `report_cases_list` | trust's `ReportCases`, open with their total or resolved | no |
| `report_case_get` | trust's `ReportCase`: the account's open case or its latest resolved one, with every report and its note | the account, the reports and their reporters |

  `count` reads a `CountByStatus` (`CountOrders` in deal,
  `CountReportCases` in trust) that each module answers with one
  `GROUP BY status` query through `postgres.Counts`. Accounts count by
  `created_at` and leave out ShopNexus's own account, listings by
  `created_at` with deleted ones, orders by `requested_at`, tickets by
  `opened_at`; open cases are accounts with an open report made since,
  resolved cases those resolved since. A new entity is one entry in the
  tool's `counters` and a count in its module; a new kind for `lookup` is
  one entry in `finders`.
- The spec's `account_get` also counts reports, and `order_get` gives the
  carrier's events and proof of delivery. `account_get` leaves reports to
  `report_case_get`, and ShopNexus keeps no carrier events or proof, so
  `order_get` says so in its description. `price_guide` takes a listing,
  since a sample needs the listing's vectors.
- A not_received ticket keeps its order's seller, `tickets.seller_id`
  (migration 0050, filled from `orders`), which `TicketOpen` takes from the
  order it reads, so support finds the tickets of a seller without reading
  deal's tables.

#### The read record

Every tool that returns personal data (what a person wrote, messages)
records the read in `console_agent_reads` before it answers (NFR-PRIV-02):
the member, the tool, the subject's kind (`ticket` or `account`) and the
ids it read, at the service's clock. A read is kept 90 days. The sweep
runs at start and every hour in the MCP server's `Run`, as the events
cleanup does in the bus, and deletes 10,000 rows at a time. Listing the
reads for admins comes with the console's audit pages.

### AI settings

Admins set the AI providers, the models, the agents, the router's rotation
and the budgets under `/v1/console/settings/*` (decided 2026-11-18, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-18-console-ai-synthesis-design.md`).
Module agent serves them behind console's `Admin` guard and drives
LiteLLM's admin API with `LITELLM_URL` and `LITELLM_MASTER_KEY`
(`provider/litellm.Admin`). LiteLLM's database holds providers,
deployments, router settings and budgets (`store_model_in_db`); the
server's database holds the tags and the agents. Every change is a row in
`console_actions`: a change to `ai_*` rows writes it in the same
transaction, and a change made in LiteLLM writes it after LiteLLM took it.
A provider and a model have no UUID of their own, so their `target_id` is
`audit.NamedID`, the first 16 bytes of SHA-256 over kind and name as a
version 8 UUID, and `details` carries the name. LiteLLM down is 503
`ai_gateway_unavailable`; a request LiteLLM refuses is 422
`ai_gateway_refused`, its detail logged.

- A provider is a LiteLLM credential: its id is the credential name, its
  key and base URL are the credential's values, and `credential_info`
  holds `custom_llm_provider` (the kind, LiteLLM's route such as `openai`),
  `base_url`, `weight`, `order`, `mode` and `synced_at`. The key never
  comes back; the list shows LiteLLM's masked copy.
- The deployments the server writes carry `model_info.shopnexus` (their
  role), `provider` and `model`. A `catalog` deployment, model name
  `<provider>/<model>`, puts a model in a provider's list. An `alias`
  deployment serves an agent's alias: one per provider whose list has the
  agent's model, with that provider's weight and order and the catalog's
  price. A `lister`, model name `<provider>/*` with the route `<kind>/*`,
  belongs to a provider in sync mode; LiteLLM's `/model/info` expands it
  into one row per model the provider lists (the live list with
  `litellm_settings.check_provider_endpoint`, else LiteLLM's known list).
- A model is named as the stack calls it without LiteLLM's route
  (`claude-haiku-5-5`, `baai/bge-m3`). The model list joins the catalog
  deployments, the aliases LiteLLM's config wrote before the server
  managed them, `ai_model_tags` and the agents. A model id may hold a
  slash, and kin-openapi's router matches no encoded slash in a path, so
  the model goes in the body (`PUT …/models/tags`) or the query
  (`DELETE …/providers/{id}/models?model=`).
- Picking a model for an agent rewrites the alias's deployments: it adds
  one per provider of the model, then deletes the old ones, so the alias
  always has a deployment and the code that calls it needs no deploy. A
  model no provider serves is 404 `ai_model_not_found`, and one without a
  tag the agent requires is 422 `ai_model_lacks_tags`. A provider's new
  weight or order, and a model added to its list, rewrite the aliases on
  it. Removing the last provider of an agent's model is 409
  `ai_provider_in_use`.
- Sync mode: `Mounted.Run` calls `SyncProviders` at start and every 6
  hours, beside the read sweep. It adds a catalog deployment for each
  model a provider's lister lists and its list lacks, with no tags, and
  drops a model's second catalog deployment, which two replicas syncing
  at once can leave. Switching a provider to sync syncs it at once.
  Every provider has a lister, made with the provider, so `GET
  …/providers/{id}/models` lists what the provider offers in either mode,
  each model marked `added` when its catalog deployment exists; price and
  `context_tokens` (`max_input_tokens`) come from LiteLLM's model info.
- A provider turned off (`enabled: false` in `credential_info`) keeps its
  catalog and serves no alias: the aliases on it are rewritten without
  it, and turning it on writes it back. Turning off an agent's model's
  only provider is 409 `ai_provider_in_use`.
- `GET …/providers/health` counts each provider's requests of the last 24
  hours in `/spend/logs/v2`, by the deployment that served them: share of
  all, share that succeeded, median `request_duration_ms` and the last
  failure's message. LiteLLM keeps cooldowns in its router and shows none,
  so `cooling_until` is never set.
- Rotation is LiteLLM's router settings, read from `/router/settings` and
  written through `/config/update`: `routing_strategy` (`simple-shuffle`
  picks by weight; a lower order is tried first and a higher one is the
  fallback), `num_retries`, `allowed_fails`, `cooldown_time` and
  `timeout`.
- Budgets: an app agent's monthly budget is `max_budget` with
  `budget_duration: 30d` on each of its keys (`ai_agents.key_aliases`); a
  console agent's is `model_max_budget[alias]` on the agent service's key
  `agent`, which also gains a new console agent's alias in its `models`.
  Spent figures are the keys' `spend` and `model_spend`;
  `GET …/spend?range=7d|30d|90d` reads `/user/daily/activity/aggregated`
  and maps each model group to the agent with that alias, and each
  member's spend from `/customer/daily/activity/aggregated` (the agent
  passes `user=<member id>`) with their questions from the agent's
  `GET /stats/turns`.
- Members' daily budgets: `ai_budget_settings` holds one per role and
  `ai_member_budgets` a member's own. Each is a LiteLLM budget of one day,
  `console-member-moderator`, `console-member-admin` and
  `console-member-<member id>`; a change saves them, makes the moderators'
  budget the `end_user_budget_id` of key `agent`, and puts every member's
  end user on the budget that applies (`/customer/update`, else
  `/customer/new`). A member made later is put on theirs before their
  first turn, once per process. A role's budget of 0 or null is none.
- The month's cap (`ai_budget_settings.monthly_cap_usd`, with `alert_pct`
  and `on_cap`) is on the spend of every agent since the first of the UTC
  month. `GET …/budgets` sets `alerting` at `alert_pct` of the cap and
  `capped` at the cap. With `on_cap: stop_agents` a question to a console
  agent past the cap is 429 `agent_budget`; the month's spend is read at
  most once a minute, and a LiteLLM that cannot be read stops nothing. App
  agents keep their own key budgets. No console notice is sent; the
  console shows `alerting`.
- `GET …/agents/usage`, `…/tools` and `…/guardrails` take
  `range=7d|30d|90d` (7d by default). Usage counts a console agent's turns
  from the agent's `GET /stats/turns` and an app agent's requests to its
  alias in LiteLLM; tools and guardrails are the agent's `GET
  /stats/tools` and `/stats/guardrails`, each tool with its title from
  `domain.ToolTitles`, which the MCP server's tools also read.

| Table | Holds |
|---|---|
| `ai_tags` | the eight tags: types Chat and Embedding; capabilities Vision, Tool use, Long context, Reasoning, Structured output and Multilingual |
| `ai_model_tags` | `(model_id, tag_name)`, seeded for `claude-sonnet-5-5`, `claude-haiku-5-5` and `baai/bge-m3` |
| `ai_agents` | each agent: kind (`app`, `console`), `display_name`, `model_alias`, icon, purpose, required tags, model, instructions and their version, tools, limits, monthly budget, LiteLLM key aliases, enabled, who changed it and `write_id` |
| `ai_agent_versions` | each version of an agent's instructions, who wrote it and when |
| `ai_budget_settings` | one row: the month's cap, `alert_pct`, `on_cap`, and the daily budget of a moderator and of an admin |
| `ai_member_budgets` | a member's own daily budget |

Migration 0056 seeds the agents: Console assistant (`console-agent`,
console, every MCP tool), Scam check (`scam-check`), Listing photo reader
(`listing-read`), Search (`search`) and Search embeddings (`embed-text`),
the four app agents with their keys of `infra/stack/litellm/keys.txt`. An
app agent takes a model, limits and a budget; any other change is 422
`ai_agent_app_fixed`. Only a console agent has instructions and tools,
which a CHECK holds. New instructions raise `instructions_version` and add
a row to `ai_agent_versions`.

### Erasure

A person deletes their account with `DELETE /v1/accounts/me` (decided
2026-11-10, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-10-server-account-erasure-feedback-search-history-design.md`).

- Module `erasure` serves the route because deal imports account: account
  cannot ask deal about orders. Its one use case checks the sign-in's age
  (428 `reauth_required` after 15 minutes, `domain.RecentAuth`), then asks
  `dealsvc.Service.OpenOrders` (409 `orders_open` while an order of the
  caller's, either side, is requested or shipped), then calls
  `accountsvc.Service.Erase`. A suspension in force refuses it (403
  `account_suspended`), so a deletion cannot shed a suspension. Depguard
  forbids every other module to import erasure.
- `Erase` keeps the `accounts` row as a tombstone, since orders, reviews,
  messages, conversations and reports point to it: `status = 'deleted'`,
  `deleted_at` set, no name, bio or avatar (a CHECK holds it). In the same
  transaction it deletes the email codes the account asked for or that went
  to its email, that email's send quota, the credentials with their
  identities, the addresses, the blocks and follows either way, revokes
  every session and writes `account.deleted`. A phone or email of the
  account then signs up as a new account. A conflict reads again once, then `try_again`.
- Each module clears its own rows from a worker on `account.deleted`, and
  each handler does nothing the second time: catalog deletes the listings
  that are not sold (a processing one too; its publish flow stops on a
  listing that left processing) and the drafts; deal ends the account's deals with
  `PartyLeft` (a requested order cancelled by the system with
  `listing_closed`, an agreement dropped, an open price taken back);
  discovery deletes the saves, the search history, the hidden listings, the
  search watches and the record of which listings the account opened
  (the view counts stay); notification deletes the notices, push tokens and
  settings; assist deletes the reads and the waiting transcripts; chat
  deletes the quick replies.
  Media does nothing: a waiting upload, a video read's frames and a link
  read's copies among them, expires
  within a day anyway, and a draft's photo within 30 days of the draft's
  last write, in the private `uploads/` that no URL reaches once the
  drafts are gone; a read's video is consumed when the read ends, and
  the avatar object stays in the store, as a removed avatar does.
- Deal's `DealAccountWatch.Deleted` also sends `Scrub` with a delay of 365
  days (`domain.KeepRecipientsFor`), which blanks the recipient name, phone
  and street of every order the account bought; the commune stays. Every
  order of a deleted account is closed by then, so each closed at least a
  year earlier, as the app's text promises.
- Accepted race: a buyer requests COD between the open-order check and the
  erase. Deal's worker cancels that order on `account.deleted`, and the
  buyer is told as for any cancellation. A shipped order cannot appear in
  the window, since only the seller ships.
- `accountsvc.Service.Status` replaced `Suspended`: the status in force now,
  `active`, `suspended` or `deleted`. Every write answers
  `status.Refusal()`, so a deleted caller's access token, valid for up to 15
  minutes, gets 401 `session_revoked`, the answer its revoked session gives
  at refresh. 409 `account_deleted` is about the other person only: a reply
  in a conversation with a deleted account, and a follow of one. A deal or
  COD request to a deleted seller needs no check, since the seller's
  listings are deleted.
- Writes that keep rows of the caller (an address, a block, a save, a
  search history entry) take a share lock on the caller's `accounts` row in
  their transaction and refuse a tombstone (`session_revoked`; a history
  entry is skipped). A write that passed its checks before `Erase` and
  commits after it waits for `Erase` and then sees the tombstone, so no new
  row outlives the deletion or discovery's worker. Refresh revokes a
  deleted account's session with `session_revoked`, and a sign-in that
  found the identity before `Erase` deleted it answers `try_again`; the
  retry finds no identity and signs up a new account.
  A listing view's mark is the exception: it takes a shared advisory lock
  on the viewer instead, and discovery's `Clear` the exclusive one (see
  Tin của bạn and listing views).
- Others see a deleted person as `status: deleted` on `Account` and the
  console account, and as `deleted: true` on chat's `Person` and a review's
  `buyer`; the reviews it wrote still count toward the seller's rating.
  `GET /v1/screens/seller/{id}` is 404 `account_not_found`.

### Notifications

Module `notification` tells people of their deals, orders, listings,
reviews and account, in a list, by push and by mail (decided 2026-10-27, spec:
`~/shopnexus/docs/superpowers/specs/2026-10-27-server-notification-design.md`). No module
calls it except screen, for the unread counts of Home and
`GET /v1/me/unread`; depguard holds that.

- It hears events: `deal.moved` (whose payload now lists the IDs of the
  moves the save wrote), `message.sent` (event ID = message ID, so a
  repeated write adds none), `listing.published`, `listing.rejected`,
  `review.posted` (event ID = review ID, first submission only),
  `review.replied` (event ID = the reply's write ID),
  `account.suspended` (payload `until`) and `account.unsuspended`.
- The Restate service `NotificationWorker` has a handler per kind. Each
  reads the facts through the source module's service in one step
  (`dealsvc.Moves`, `chatsvc.MessageFacts`, `trustsvc.ReviewFacts`,
  catalog's `Summary`), decides in notification's domain who hears what,
  writes each notice in one step, then pushes. A move the deal lacks, as
  when a replayed save minted other IDs, tells no one. A step returns zero
  facts when the source answers not found, and the handler tells no one
  when the facts' ID is zero.
- Review notices (2026-11-19): `ReviewPosted` tells the subject once the
  review shows. A hidden review's handler sleeps in Restate until its
  `visible_at`, then reads the review again. The second side's review
  reveals the first, so its handler tells both subjects; the first one's
  handler sees `RevealedBy` and tells no one. `ReviewReplied` tells the
  reviewer. Both open the order; a review notice from before, with only
  the seller, still opens `seller_reviews`.
- A notice stores its type and `domain.Params`, JSON with only the keys
  its type reads; the server writes the text in the reader's language, carrier labels
  included, when it lists and when it pushes. Deal and order notices collapse on
  `deal:<id>`, listing notices on `listing:<id>`: a new one replaces the
  recipient's unread notice of the key and moves it up, and a notice
  already read stays as it is. An event older than the recipient's newest
  notice of the key, read or unread, writes nothing and pushes nothing, so
  a late delivery can neither replace newer text nor come back as a new
  unread notice; the upsert's guard on `bumped_at` covers the race between
  two events of one key. A redelivered event writes nothing (the row
  records its `event_id`). A recipient's
  notices older than 90 days are deleted when they get a new one.
- A notice's push has the Android tag and FCM collapse key
  `notification:<id>` (decided 2026-11-17). An unread notice keeps its ID
  when a newer event of its collapse key rewrites it, so the new push
  replaces the notice's tray item, and the app removes the item by the
  notice's ID when the notice is read in the app.
- An order notice opens its own order (decided 2026-11-17): a deal can
  have an earlier order besides its latest, and the app opens an order by
  its ID. Each step of an order in `deal_moves` names its order
  (`order_id`, NULL on a move on the price), `dealsvc.Moves` reads the
  deal's latest order and the orders the moves name, and each move tells
  of its own order's ID, carrier, tracking code and cancel reason. The
  notice keeps `order_id` in its params and targets
  `{kind: order, id: <order id>}`; the push carries the same. A step that
  names no order (written by the release before during a deploy, or one
  0052 could not match) tells of the deal's latest order, as every step
  did before, and its notice opens the deal (`{kind: deal}`). The journaled
  `DealFacts` keep the latest order's carrier, code and reason at the top
  for those steps, so a `DealMoved` journal entry from before replays with
  its data. Migration 0052 matched the steps and the order notices from
  before by their times: a step takes the one order of its deal whose
  request, shipment or end it happened at, and a notice the order of the
  one step of its deal, kind and `bumped_at`. An order notice that matched
  none keeps no order and opens its deal.
- A followed seller's new listing (decided 2026-11-10): on a
  `listing.published` with `first`, `ListingPublished` tells the seller as
  before, reads the seller's name, then reads the followers 500 per step
  (`accountsvc.Followers`) and sends `NotificationWorker.TellFollower` once
  per follower with the idempotency key `<event id>/<follower id>`. Each
  follower's notice is an invocation of its own with a short journal, but
  `ListingPublished` journals one send per follower and one step per page:
  a seller with 30,000 followers gives it about 30,000 entries, all replayed
  on a retry. A chain of page invocations would bound it when sellers get
  that large. `TellFollower` reads the listing again and tells no
  one when it is no longer `active`; otherwise it writes `followed_listing`
  ("{seller} vừa đăng tin mới: {title}", opening the listing) collapsing on
  `seller:<id>`, so a run of listings leaves one unread notice, and pushes
  it.
- Search watches and price alerts (decided 2026-11-11):
  `search_watch.matched` writes `watch_matched` ("Có {count} tin mới cho"
  and the query in curly quotes, 99+ from 99), whose target kind is
  `search_watch`, collapsing on `watch:<id>`. `saved.price_dropped` writes `price_dropped` ("{title}
  giảm còn {price}, lúc lưu {saved price}"), opening the listing,
  collapsing on `saved:<listing id>`. The worker reads discovery's
  `WatchFacts` and `PriceDropFacts` and tells no one when the watch or the
  save is gone.
- Chat messages are pushed and never listed, collapsing on
  `conversation:<id>`, with the sender's name and the first 100 characters
  (chat's `MessageFacts` gives the whole text; notification cuts it).
  A support chat's message pushes only to the ticket's owner, titled
  ShopNexus, with `type` `support_message` and target kind `ticket` and the
  ticket's ID, so the app opens the ticket without looking it up (decided
  2026-11-17). The owner's message to ShopNexus pushes nothing; the
  console's queue shows it.
- A push token belongs to a sign-in session (`push_tokens.session_id`).
  Putting a token takes it from every other session that had it, so a
  phone signed in to a second account stops getting the first account's
  pushes.
  Before a push the worker asks account which of the recipient's sessions are
  active and deletes the others' tokens, which covers sign-out, revocation,
  refresh reuse, expiry and 30 days idle with one read. Each token is sent in a step of
  its own, so a retried send repeats no other: FCM's `UNREGISTERED` or
  `INVALID_ARGUMENT` deletes the token, 429, 5xx and transport errors retry
  for up to an hour, and another error is logged and dropped. The bus
  delivers an event once per ID, so a notice pushes once.
- `internal/provider/push` holds `Sender`: FCM HTTP v1, signed by a
  service account (`FCM_CREDENTIALS` in base64, `FIREBASE_PROJECT_ID`) through
  `golang.org/x/oauth2/google`, or a log sender when `FCM_CREDENTIALS` is
  empty. With `APP_ENV=prod` the server refuses to start on the log
  sender.
- The website takes pushes too (decided 2026-11-20): a browser puts its
  FCM token like a phone. One message serves both: its `webpush` block
  carries the same tag, so a newer push replaces the browser's
  notification, and `Urgency: high`. The site's service worker shows the
  notification and opens the page from `target_kind` and `target_id` on
  its own origin, so the server needs no site address and sets no
  `fcm_options.link`, which FCM accepts only over https.
- A suspended account still gets notices, so it learns of the suspension.
- `POST /v1/notifications/read-all` returns the IDs it marked read, and
  `POST /v1/notifications/unread {ids}` (1 to 500) makes read notices unread
  again, for one notice and for the undo of read-all (decided 2026-11-10).
  Read-all returns every ID it changed, so an undo of more than 500 is
  sent in chunks.
  Of the read notices that share a collapse key only the newest becomes
  unread, and none when the key has an unread notice, since the unique
  index allows one. A notice of the key that turns unread between the
  check and the write raises a unique violation, and the statement runs
  once more.
- Settings (decided 2026-11-14, spec:
  `~/shopnexus/docs/superpowers/specs/2026-11-14-notification-settings-design.md`):
  `GET` and `PUT /v1/me/notification-settings` hold six push switches,
  `messages`, `offers`, `orders`, `price_cuts`, `followed` and `reviews`,
  the mail switch `order_emails` (migration 0078, see Mail below),
  and quiet hours `{on, from, to}`, the times as HH:MM in Vietnam time.
  They replaced discovery's `/v1/me/price-cut-alerts`. A switch that is
  off stops the push only, and the notice is still listed; a chat message
  is never listed, so with `messages` off it reaches no phone. Support
  replies are chat messages and fall under `messages` too.
  Each notice group in `types` names its switch, and `domain.SwitchOf`
  reads it; `followed_listing` has a group of its own for `followed`.
  `watch_matched` keeps its watch's bell, and `listing_live`,
  `listing_rejected`, `suspended` and `unsuspended` always push.
  `TestEveryTypeIsDescribed` reads the `Type` constants from the package's
  source and fails on a type `types` lacks.
- While quiet hours are on and Vietnam's clock is in `[from, to)`, no push
  goes out, and a push held back is dropped. The window may cross
  midnight; `from == to` is 400 `invalid_quiet_hours`, a time that is not
  HH:MM is the spec's 400 `invalid_request`. An account without a row
  reads every switch on and quiet hours on from 22:00 to 07:00, and a read
  writes no row. `PUT` replaces the whole document and returns it. It
  takes a share lock on the account's row and refuses a tombstone with
  `session_revoked`, as the other writes that keep rows of the caller do.
- `deliver` reads the recipient's settings and the worker's clock
  (`Worker.Now`) in its token step, after the notice is written; a push
  held back reads no tokens. E2E's `putToken` also turns quiet
  hours off, so the tests that wait for a push pass at night.
- Mail (decided 2026-11-23): order notices (the `orders` group: COD
  request, shipped, completed, failed, cancelled) are mailed while
  `order_emails` is on, which it is by default; account notices
  (`suspended`, `unsuspended`) are always mailed. Every other notice is
  push only: it matters for minutes, and mailing it would teach Gmail to
  file the domain's mail, codes included, as spam. Mail ignores the push
  switches and quiet hours. After a notice's push, `tell` skips a type
  that is never mailed without a step (`domain.Mailed`); otherwise it reads
  the recipient's settings and email identity in one step and sends in
  another, keyed `<event id>/<recipient>/<notice index>`; an account without an email
  identity (phone only, or erased) gets no mail. A refusal is logged and
  dropped, other failures retry for an hour. `domain.Mail` writes the
  mail as a `mailview.Letter` in the recipient's language.
- Mail layout and languages (decided 2026-11-23, design in
  `.superdesign/`): `internal/platform/mailview` renders a `Letter` (a
  mascot drawing, title, lead, then the blocks a mail uses: code, stars,
  listing, steps, rows, note, one button, a footer link) with
  `html/template` into HTML with tables and inline styles, plus a
  plain-text part. The logo and the drawings are PNGs embedded in the
  binary and sent inline (`cid:`), so mail needs no image host. One
  `mailview.Mailer` (the email sender and the first of `WEBSITE_ORIGINS`,
  which links go to) is built in `app` and handed to account and
  notification. An order notice's mail facts (cover, price, fee) ride on
  `Notice.Order`, which is not stored. Text lives in `<tag>.json` catalogs
  next to the code that writes it (`mailview/locales`,
  `notification/domain/mail`, `account/service/mail`), loaded by
  `platform/locale`, which refuses a catalog whose languages have
  different keys; adding a language is a file per catalog and a tag in
  `locale.Supported`. Supported: vi (default) and en. A sign-in records
  the `Accept-Language` it came with as `accounts.locale` (migration
  0079), unless the header names no supported language; notice mail reads
  it with the address through `accountsvc.Mailbox`, and a code mail
  follows its own request's header. `locale.Money` and `locale.Date`
  write amounts and dates per language.
- Notice text in languages (decided 2026-11-23): `GET /v1/notifications`
  writes each notice in its request's `Accept-Language`; a push is
  written in the recipient's `accounts.locale`, which `deliver` reads in
  its token step with the tokens. Push titles, notice text, chat push
  bodies and mail share `notification/domain/text/<tag>.json`; a notice
  stores only its type and params, so a stored notice reads in any
  language. A name kept in params for someone without one stays the
  Vietnamese placeholder. Advertising mail, if it ever comes, starts off (Decree
  91/2020 wants consent first).
- Migration 0045 copied each `saved_settings` row with the switch off into
  `notification_settings` as `price_cuts = false`, the rest at the
  defaults, and dropped `saved_settings`.

### Order screens and shipping settings

Decided 2026-11-16, app plan `2026-11-16-app-wire-orders.md`.

- `GET /v1/screens/order/{order_id}` reads the selected order through
  deal's `OrderFor` and the current deal through `Get`. `OrderFor` checks
  party membership and uses the canonical `Order` builder, including its
  recipient and pickup redaction. A historical selection keeps its own
  order ID and status when `deal.order` names a later purchase. Every
  canonical `Order` response carries the stored `completed_by` on completion.
- The screen reads the counterpart as a public `Person`, and for either
  side trust's `OrderReviews`: `own_review`, the caller's review, and
  `their_review`, the other side's review of the caller. For the buyer it
  also reads support's `OfOrder`; the seller's response omits `ticket`.
  `own_review` has state `none`, `active` or `deleted`, nullable original
  dates and ID, `change_until` at seven days from the first submission,
  `visible_at`, and the subject's `reply`. `locked` calls
  `Review.CanChange`, whose boundary is strictly after those seven days,
  and which a reply closes. Stars and text appear only in the active state.
  `their_review` is null without one or once deleted; `hidden` carries only
  `visible_at`, `visible` its stars, text, reply and `can_reply`. Trust
  reads the order's review rows in one query. Screen checks the order's
  ownership before that call, and the HTTP API has no GET review route.
- `GET /v1/screens/orders?role=buying|selling&cursor=&limit=` is the Đơn
  hàng list, newest order first and paged by order ID through deal's
  `ListOrders`. It replaced `GET /v1/me/orders` and its `OrderList` schema,
  whose only caller was the app. A row is `{order, counterpart,
  own_review}`: `order` is deal's `OrderItem` with the canonical `Order`
  redaction, `counterpart` the public `Person`, and `own_review`, on both
  sides' rows, the order view's `OwnReview`. A page costs four calls:
  `ListOrders`, then account's `GetMany`, trust's `OrderReviews` and the
  side's count at the same time (`errgroup`). `OrderReviews` reads the page's reviews in one
  query, `Reviews.OfOrders`. The order view calls `OrderReviews` with its
  one order. `ListOrders` reads listings and
  communes for the rows `page.Of` kept, as catalog's `SellerListings`
  does. Each page also counts over all of the caller's orders on its side
  (decided 2026-11-19), so the list needs no paging for them: `cod_orders`
  for buying, the `Cap` of the checkout and the reputation view
  (`BuyerOrders.Open` against `domain.MaxOpenOrders`), and
  `awaiting_shipment` for selling, the pending orders, which wait for the
  seller's shipment (`AwaitingShipment`, one count over `orders_seller`).
  The other side's field is absent. Deal's `OrderItem` holds the other party's ID in `CounterpartID`,
  which is not in the schema, as `AccountView` holds `LastActiveAt`.
  `GET /v1/me/deals` and the orders view parse `role` through deal's
  `domain.ListRole`.
- `POST /v1/orders/{order_id}/cancel` returns the canonical `Deal`, as
  shipment and confirmation do. `CancelPendingOrder` checks that the ID
  still names the aggregate's current pending order, then calls `Cancel`.
  `Save` checks the deal's write ID in the same transaction that writes the
  cancellation, so a stale request cannot cancel a replacement order.
  Unknown and foreign orders are `order_not_found`; a historical or closed
  one is `move_not_allowed`; a concurrent write is `deal_changed`. The
  existing deal-agreement cancellation route stays available.
- Shipping owns `shipping_settings` (migration 0048), one row per caller
  with an address reference and a separate contact phone. `PUT
  /v1/me/shipping-settings` replaces `{pickup: null | {address_id, phone}}`
  and normalizes the phone through `contact.ParsePhone`; this phone has no
  identity verification. A null pickup deletes the row. The save holds a
  share lock on the account, refuses a tombstone with `session_revoked`,
  and checks ownership while holding a key-share lock on the address.
  The address foreign key cascades deletion of the whole preference,
  including its phone. Editing an address changes subsequent resolutions;
  shipment snapshots retain their earlier values.
- `GET /v1/screens/cod-setup` composes `MyAccount`, owned addresses,
  shipping settings and linked carrier accounts. The existing carrier
  list, link, unlink and pickup routes stay available. Linking GHTK or GHN
  uses an existing carrier account; the API creates no carrier account and
  asserts no verified holder or earlier account provenance.
- Linked `pickup_from` and the quote query accept `saved_pickup: true`,
  exclusively with respect to the address and carrier-point selectors.
  Shipping resolves the saved reference through account's service, using
  the current display name, saved contact phone and current owned address.
  Deal resolves a saved pickup in a journaled step after checking the seller,
  selected pending order and listing, and before reserving the listing. A
  retry with the same caller and `Idempotency-Key` reuses that invocation's
  snapshot and outcome after the preference or address is removed. Quotes
  read the current preference on every request. An absent preference on a
  new valid shipment request is `pickup_not_configured`; omitting all
  selectors keeps the carrier's default.
- `GET /v1/screens/cod-checkout/{deal_id}?address_id=` is buyer-only and
  returns the `Deal`, public seller, selected or default owned address,
  COD cap `{used,max}` and `shipping_estimate`. The estimate is null on
  every call, whatever `CARRIER` is and whether the seller linked a
  carrier, until a pre-order estimator is defined: a quote needs the
  order's weight and the seller's carrier, which the checkout does not
  have (checked 2026-11-19). An empty book gives a null address;
  an explicitly missing or foreign address is `address_not_found` and a
  nonbuyer or foreign deal is `deal_not_found`. The read writes nothing
  and calls no carrier. `httpx.Bind` supports optional pointer query fields
  so an explicit all-zero UUID remains distinct from an omitted address.
- Stream kind `ticket` carries `conversation_id` to the ticket owner only.
  Ticket insertion, initial-message attachment, resolve and reopen call
  `stream.Notify` inside their transactions, including the asynchronous
  reopen on a person's new message and account-deletion resolution.
  `ticket_message_receipts` (migration 0048) records each handled message
  ID in the transaction that applies its metadata and status change. A
  person's message reopens a resolved ticket only when its event time is
  strictly later than `resolved_at`, at PostgreSQL's microsecond precision,
  and the owner is still present. The transaction holds the ticket lock
  and, when reopening, a share lock on the owner's account. A deleted owner
  remains resolved even for a message with a later timestamp. A late first
  delivery at or before resolution updates its receipt and any newer
  message metadata without a status event or hint.
- The resolution-time check also covers messages processed before migration
  0048 whose Restate step was not journaled. The migration creates an empty
  receipt table and does not pre-consume historical messages; an unprocessed
  message after the recorded resolution can still reopen the ticket.
- Initial-message attachment reads the sent message's `created_at` and
  applies its metadata through the same transaction helper as `MessageSent`.
  It records the receipt, first-message ID, any allowed reopen and owner-only
  hint together. The queue therefore uses the actual send time when
  `TicketOpen` waited before sending. An initial attachment after a newer
  reply keeps that reply's time and sender. Existing message and read updates
  still carry chat content and unread invalidation.

### Tin của bạn and listing views

Decided 2026-11-16, app plan `2026-11-16-app-wire-sell.md`.

- `GET /v1/screens/my-listings?pane=on_sale|sold&cursor=&limit=` is Tin
  của bạn. It replaced `GET /v1/me/listings` and its `ListingList`
  schema, whose only caller was the app; `cmd/devseed` reads the view
  now. Each item is `{listing, views, views_7d, askers, saves, order}`:
  `listing` is the canonical `Listing` as its seller reads it, and the
  sold pane's items carry `sold_at` and no buyer. `week` holds the views,
  askers and saves of the last seven days over the seller's listings on
  sale, whichever pane and page the call reads, and every response has
  it.
- A page costs four or five calls, whatever it holds. Catalog's
  `ListMine` reads the page. The first page of the on-sale pane with no
  page after it holds every listing on sale; any other page also reads
  their IDs through catalog's `OnSale` (at most `MaxOnSale`). Then, for
  those IDs and the page's together, merged through a set: discovery's
  `ListingStats`, deal's `ListingActivity` and chat's `ListingAskers`, at
  the same time.
- The week is the seven UTC days that end today, the days views are
  stored by (discovery's `domain.ViewWeek`). The screen reads the clock
  once (`Deps.Now`), takes the week's start, a UTC midnight, and passes
  it to every read: `views_7d` and the week's views, saves and askers all
  count from it.
- An asker is a person with a deal on the listing or who wrote to the
  seller about it in their pair conversation, counted once per listing
  (product decision 2026-11-16). The screen merges deal's buyers with
  chat's senders and keeps each person's earlier time; the week counts
  those whose first contact was in the last seven days. Chat reads the
  messages that name the listing through `messages_listing` (migration
  0049), sent by someone other than the seller in a pair conversation of
  the seller's, deal messages left out. Blocked and deleted people still
  count. The app's "Bán cho ai?" sheet is gone, and the view carries the
  count alone.
- `order` is the order the seller acts on: the shipped one, which holds
  the listing, else the pending order requested first, whose seller
  timeout ends first. It carries `deal_id`, `order_id` and `status`.
  `Deals.OnListings` reads one row per deal, its open order through a
  `LATERAL ... LIMIT 1` with the same pick; `orders_open_per_deal`
  already holds a deal to one open order, and a deal whose order was
  cancelled and asked for again lists its buyer once.
- A view is a signed-in reader other than the seller opening
  `GET /v1/screens/listing/{id}`, or reporting the open of a prefetched
  one; `GET /v1/listings/{id}` is the seller's
  management read and counts nothing, and guests are not counted. It
  counts once per reader, listing and UTC day (`domain.ViewDay` in
  discovery). Discovery owns the counts, beside the saves:
  `listing_view_marks` keeps each person's last day per listing, and
  `listing_view_days` the views per listing and day. `Views.Record`
  upserts the mark only when its day moves forward and, in the same
  statement, adds one to the day's row when it did; a second open that
  day, a retry or a concurrent open finds the mark at that day and adds
  nothing. The statement itself keeps out the listing's seller, a
  listing that is not active or reserved and a deleted account, whatever
  the caller checked.
- Recording a view locks no `accounts` row (decided 2026-11-16, after the
  branch's review), where the writes of Erasure take a share lock: a
  listing open is a hot read, and the share lock would make every open
  write the account's tuple and wait on `TouchActive`. A shared advisory
  lock on the viewer (`hashtext('views:' || viewer)`) takes its place,
  and `Views.Clear` takes it exclusively before it deletes. A record in
  flight when the account is deleted holds the lock, so the erasure's
  `Clear` waits for it to commit and deletes its mark; a record that
  starts after `Clear` reads the deleted account and writes nothing.
  `TestViewsAndErasureRace` stops a record inside its statement and runs
  the deletion and `Clear` across it. The lock and the record go as one
  pgx batch: one round trip in one implicit transaction.
- The listing view counts after its reads and in the request, and a
  failed count is logged at WARN and leaves the answer as it is.
- A prefetch counts no view (decided 2026-11-20, for the website's
  speculation rules). The browser fetches a page it expects the person to
  open with `Sec-Purpose: prefetch` (`prefetch;prerender` for a
  prerender), and the website hands the header on to the calls that page
  makes. `httpx.Prefetch(r)` reads it, true when an item of the list is
  the token `prefetch`, and the listing view gets it as
  `ListingRead.Prefetch`: the same answer, no `Viewed`. A prefetched page
  the person opens reports the open with `PUT /v1/viewed/{listing_id}`,
  which runs `Viewed` as the view would have. The other reads the listing page makes
  (search by `like`, the seller's listings and reviews, the account and
  the badges) write nothing; search remembers only a query.
- Saves are the saves that stand: the week counts those made since its
  start.
- A mark names a person and a listing, so discovery's `AccountDeleted`
  deletes the person's marks and keeps the day counts. A mark is kept
  until then; it holds one row per person and listing, whatever the
  number of opens.
- `POST /v1/listings/{id}/photo-copies` serves Đăng lại. It takes an
  `Idempotency-Key` and runs the Restate service `ListingPhotoCopy`
  through `restatex.Caller`, keyed `<seller id>/<listing id>/<key>`, so a
  repeated key returns the first run's uploads and the same key on
  another listing runs apart (decided 2026-11-16, after the branch's
  review, which found that a retried tap made a second set). A step reads
  the listing, refusing another seller's or a deleted one with
  `listing_not_found`. Then one step per processed photo, one at a time,
  reads it with media's `ReadPhoto` and saves it with `SavePhoto` under
  an ID `restatex.NewID` minted before the step, so a retried step writes
  the same object and inserts nothing; the bytes never enter the journal.
  A copy's origin is `listing`, or `link` for a photo first copied from
  another site, so the new listing marks it copied. `Catalog.CopyPhotos`
  then signs each copy with `PresignPhoto`, fresh on a repeated key, and
  answers 201 with `{items: [{upload_id, url}]}`, the `ListingDraftPhoto`
  shape, in the listing's order. A listing attaches and processes a copy
  like a photo the app put, and a draft keeps it. Any status but deleted
  may be copied, and a photo not processed yet is left out. Copies nobody
  posts expire after 24 hours with their `MediaExpire`.
- The cap in `catalog.yaml` reads "processing, active, hidden, rejected
  or reserved", the statuses `domain.OnSale` counts.

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
  from the test stack, the deployment registered in `TestMain`, phone tokens
  signed by `firebasetest`, Google tokens signed by `googletest`, mails
  read from an inbox sender and pushes captured by a fake sender. PNV's keys
  stay down for the run, so a PNV token there sees the 503. The four OpenAPI conformance tests from plan 1
  run unchanged on top of it. `e2e/helpers_test.go` holds the HTTP helpers
  every module's tests use; `auth_helpers_test.go` signs a test account in.

Every test and subtest calls `t.Parallel()`; paralleltest enforces it. A test
that sets process environment variables or installs the global OTel tracer
provider runs serially and says why in its `//nolint:paralleltest` comment.
The httpx package installs an SDK tracer provider in `TestMain`, so only the
tests that read recorded spans swap the global.

### Development data

Seed packs carry development data (decided 2026-11-19, spec:
`~/shopnexus/docs/superpowers/specs/2026-11-19-seed-packs-design.md`). A pack is
a directory of JSONL, JPEG and float16 `.npy` files; `internal/seedpack`
documents the format and reads and writes it. `go run ./cmd/seedpack` builds
packs (`build <source>`, one `seedpack.Builder` per source, listed in
`cmd/seedpack/main.go`), fills their vectors (`embed`), checks them offline
(`verify`), writes them to a stack (`load`) and deletes one pack's rows
(`wipe`). `load`, `wipe` and `embed` refuse any `APP_ENV` but `dev`.

`internal/seedpack/loader` writes through the modules' entities and
repositories: `Credentials.SignUp` with a phone identity, uploads stored as
processed with their objects at `media/<upload>.jpg` and
`media/<upload>_thumb.jpg`, listings built with `NewListing` and `Published`,
and each sold item as a deal agreed, a COD order shipped by GHTK, delivered and
confirmed, the listing held for the order and sold to it. It writes no events,
except the `review.posted` that `Reviews.Insert` writes. Every row's ID is
`seedpack.ID(pack, kind, key, at)`: a UUIDv7 at the row's own date whose
random bits hash the pack's name and the row's key, so a second load skips
what is there and lists ordered by ID keep the pack's dates. Order and move
IDs are the deal entity's own UUIDv7s at load time. `wipe` deletes the pack's
accounts and every row that refers to them, following the schema's foreign
keys, then their uploads' objects. Reviews go through `loader.ReviewWriter`;
today it keeps the buyer's review of the seller and drops the seller's review
of the buyer and the replies.

`go run ./cmd/devseed` loads the demo pack, `cmd/devseed/data/demo` (12
people, 44 listings, 4 sold items, 5 follows, 2.1 MB), which
`seedpack build fixtures` made from the app's fixtures and which is
committed. Then it signs each person in with `Account.SignIn`, built with a
verifier that takes the phone number as its own token, and plays its
scenario through the HTTP API with their tokens: a GHTK link on the fake
carrier for each seller, saves, a follow, a chat, one offer, and one COD
order the seller has shipped. A second run writes only what is missing. The
demo's numbers are +84900199001 to +84900199012; to sign in as one on the
app, add it as a Firebase Auth test number.

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
- depguard: a test-support package (`accounttest`, `catalogtest`,
  `mediatest`, `objectstoretest`, `pgtest`, `embedtest`) is imported only by
  tests and by other test-support packages, whose files match
  `**/internal/**/*test/*.go`. `catalogtest` builds catalog on `embedtest`'s
  fakes.
- depguard: a module's domain, repository and service import no layer above
  them, a handler, a worker or module agent's `mcp` imports no repository,
  nothing imports a worker or `mcp`, and production code reaches another
  module only through its `service`. An error a client sees, such
  as `account_not_found`, lives in `domain` for that reason. A worker's
  tests may build its module's repositories.
- The module and layer rules of depguard sit between the
  `# BEGIN depguardgen` and `# END depguardgen` lines of `.golangci.yml`.
  `make depguard` (`go run ./cmd/depguardgen -root .`) writes them from the
  modules and layer packages under `internal/`, naming only packages that
  exist; the policy itself is in `cmd/depguardgen/main.go`. Run it after
  adding a module or a layer package: the test in `cmd/depguardgen` fails
  while the rules are stale, so `make test` catches it.
- interfacebloat skips a module's `Service` in `service/service.go` and the
  store in `repository/repository.go`: a `Service` lists every use case, and
  a module that keeps its tables in one store lists every query there.

### Restate in practice

Plan 2 settled these details on Restate 1.8.0 and Go SDK v1.1.0.

- Restate runs with `network_mode: host` on 127.0.0.1 ports: 56580 ingress,
  56570 admin and 56560 node in development, one higher in tests. The
  development machine's ufw drops container-to-host traffic.
- At start the server serves its services on `RESTATE_LISTEN_ADDR` and
  registers `RESTATE_ENDPOINT_URL` through the admin API with
  `use_http_11`, because the endpoint is a standard library HTTP/1.1 server.
- `RESTATE_REGISTER` says whether the server registers itself. `make run`
  and the tests set `true`. The k3d cluster in the umbrella's `infra/` sets
  `false`: there restate-operator's `RestateDeployment` runs the server,
  gives each version its own Service, registers it, and keeps an old version
  until its invocations finish, so an invocation never replays on other
  code. Decided 2026-11-04.
- The Restate server signs every request to the endpoint with the Ed25519
  key in `.restate/identity.pem`
  (`worker.invoker.request-identity-private-key-pem-file`), and the endpoint
  runs only requests signed with it: its public half is
  `RESTATE_IDENTITY_KEY`, required with `APP_ENV=prod`. `make run` and
  `make test` create the key file, and tests read the public key from it.
- `RESTATE_SERVICE_PREFIX` (`config.RestateConfig.ServicePrefix`) goes before
  every service name. Test packages share one Restate server and a name
  routes to one endpoint, so `restatetest.Config` gives every registration a
  fresh prefix.
- Plan 2 first judged inside a step, and a retried step then applied its
  change again (finding I1 of the plan 2 review). The read, decide, save
  split of Durable flows replaced that on 2026-10-21.
- Restate answers a repeated `Idempotency-Key` with the first result,
  whatever the new request carries. Sign-in therefore puts the verified
  number in the key, and a replay never revives a session that was signed
  out since.

