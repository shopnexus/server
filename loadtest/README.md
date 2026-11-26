# Load test

k6 scripts that act like ShopNexus users against the API. They report latency
and errors per route, and each run passes or fails against the thresholds in
`lib/thresholds.js`. Watch the Server dashboard's Saturation row while a run is
going: it shows which resource fills first.

## Scenarios

| Scenario | VU model | What one iteration does |
|---|---|---|
| `guest` | visits started per second (`GUEST_RATE`) | home, a search in words or a category with filters, a sort and up to five pages, a listing and half the time the listings like it, sometimes its seller |
| `member` | visits started per second (`MEMBER_RATE`) | home and unread counts, a category browse, a listing marked viewed; sometimes a save that it undoes, the inbox or the saved list |
| `trade` | visits started per second (`TRADE_RATE`) | with the seller's app open, a buyer opens a deal on a load-test listing and offers, writes; the seller reads, replies and declines; times how long the seller's socket takes to hear of the offer and the message |
| `socket` | open apps (`SOCKETS`) | opens `/v1/ws`, waits for `ready`, renews the access token over the socket, closes after about `SOCKET_MINUTES` and opens again |

A search's sort, filters and pages follow the shares in `journeys/browse.js`.
Every sort but relevance reads each list of a hybrid search at full depth,
the costliest search. Half the guest searches are a head term from
`data/queries.json`, the rest a term with a word added or typed without
diacritics (`lib/queries.js`), so the runs do not search the same twenty
terms over and over.

`guest`, `member` and `trade` start visits at a fixed rate whether or not earlier
visits have finished, the way real arrivals behave. When the server slows
down, visits pile up instead of the load easing off. `socket` keeps one VU per
open app, so its VU count equals the number of signed-in users online.

Every request carries the route as `api/openapi.yaml` writes it
(`/v1/screens/listing/{id}`) in the `name` tag, which matches the server's
`http_route`. Requests are also tagged with `journey`.

## What a run costs

Nothing: no scenario reaches a paid model.

- A guest's words go through BM25 alone. A signed-in person's words go
  through the model and the embedding API, so `member` never searches in
  words; it browses categories and the listings like one, which read the
  stored vectors.
- `trade` chats in words, and every message with text is sent to the scam
  classifier, a paid call. Run it only against a stack with
  `TRUST_SCAM_CHECKS=off`: in `server/.env` for `make run`. For the compose
  stack, recreate the server with a file that sets it and recreate it
  without the file after the run:

  ```sh
  printf 'services:\n  server-dev:\n    environment:\n      TRUST_SCAM_CHECKS: "off"\n' > server/tmp/scam-off.yml
  docker compose -f docker-compose.yml -f server/tmp/scam-off.yml up -d --no-deps server-dev
  ```
- Posting a listing embeds its text. `cmd/loadsessions` puts the trade
  sellers' listings on sale straight into the database instead.
- No scenario uploads a photo, searches by photo, reads a video or asks for
  a price guide.

`setup()` searches for the terms in `data/queries.json` and collects the
listing and seller ids the scenarios open, so the scripts work on any seeded
stack.

## Running

The default run is a one-minute smoke test with one guest visit a second
against `make run`'s server:

```sh
make run                 # in another terminal
make loadtest
```

Signed-in scenarios need sessions. Each signed-in VU uses the session whose
number matches its own VU number, because a refresh token stops working once it
has been used: two VUs sharing a session would sign each other out.
`cmd/loadsessions` signs in `SESSIONS` sessions spread over `ACCOUNTS` accounts
(numbers `+8490998xxxx`), and works on a dev stack only. Run it again before
every run, because k6 rotates the refresh tokens:

```sh
make loadtest-sessions SESSIONS=2000
make loadtest MEMBER_RATE=2 SOCKETS=200
```

If the file holds fewer sessions than the run can have VUs, `setup()` stops
the run and prints the `-sessions` number it needs.

`trade` needs trades too: `TRADES` of them, each a buyer of its own
(`+8490997xxxx`) and a session of one of `SELLERS` sellers (`+8490996xxxx`),
on one of the seller's `LISTINGS` listings. The first run puts those listings
on sale and later runs reuse them. Deals and messages stay among these
accounts, so no real person hears of them. The listings show in search
while on sale, so hide them after the run; the next run puts them on sale
again:

```sh
make loadtest-sessions SESSIONS=500 TRADES=500
make loadtest PROFILE=load GUEST_RATE=10 MEMBER_RATE=2 TRADE_RATE=1
go run ./cmd/loadsessions -park
```

As with sessions, a VU takes the trade whose number matches its own, so the
file needs as many trades as the run can have VUs.

Against the umbrella's compose dev stack, run k6 on its network and point it
at the server container:

```sh
docker exec -w /src shopnexus-server-dev-1 go run ./cmd/loadsessions -sessions 2000 > loadtest/sessions.json
make loadtest LOADTEST_NETWORK=shopnexus_dev BASE_URL=http://server-dev:8080 MEMBER_RATE=2 SOCKETS=200
```

On the dev cluster, run k6 in a pod of `shopnexus-dev` with this folder
copied in, against `BASE_URL=http://server-api:8080` with
`SPREAD_ADDRESSES=true` (Traefik's pod network is a trusted proxy), and
`K6_OTLP=lgtm:4318`. The server image holds `/loadsessions`: run it with
`kubectl exec` in a server pod, which has the database and keys. Turn scam
checks off with `server.scamChecks: "off"` in `values-dev.yaml` for a
`trade` run, and back on after. Reset and read `pg_stat_statements` as the
`postgres` user in the `postgres-1` pod, database `app`.

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8080` | the API; `/v1/ws` uses the same host |
| `PROFILE` | `smoke` | the load shape, from `lib/profiles.js` |
| `GUEST_RATE` | `1` | guest visits started per second at the peak |
| `MEMBER_RATE` | `0` | signed-in visits started per second at the peak |
| `TRADE_RATE` | `0` | trade visits started per second at the peak; needs `TRUST_SCAM_CHECKS=off` on the stack |
| `SOCKETS` | `0` | open sockets at the peak |
| `TIME_SCALE` | `1` | multiplies the length of every stage; `0.1` runs the profile in a tenth of the time |
| `THINK_SCALE` | `1` | multiplies the pauses between steps; `0` removes them |
| `SOCKET_MINUTES` | `10` | how long a socket stays open, give or take 20% |
| `SESSIONS_FILE` | `./sessions.json` | the output of `cmd/loadsessions`, relative to `loadtest/` |
| `SPREAD_ADDRESSES` | `false` | gives each VU its own address in 198.18.0.0/15 through `X-Forwarded-For` |
| `LOADTEST_NETWORK` | `host` | the Docker network `make loadtest` runs k6 in |
| `PGSTATS_URL` | unset | a superuser's URL of the stack's database: `make loadtest` resets `pg_stat_statements` before the run and writes its report to `results/` after |
| `K6_OTLP` | unset | host:port of an OTLP/HTTP receiver, such as the stack's lgtm: k6 sends its metrics there with the prefix `k6_`, beside the server's |

## Profiles

Each profile scales the peak settings over time:

| Profile | Shape | Question it answers |
|---|---|---|
| `smoke` | 15 s up to the peak, 1 min at it | do the scripts and the stack work? |
| `load` | 5 min up, 20 min at the peak, 2 min down | does the stack hold the target? |
| `stress` | steps of 0.5× up to 3× the peak, 5 min each | at what load does it break, and what breaks first? |
| `spike` | 0.2× to 3× in 30 s, held 5 min | does it survive a flash sale or a push sent to everyone? |
| `soak` | 4 h at the peak | do memory, tables or connections grow over time? |

## Sizing a CCU target

A visit lasts about 30 s for a guest and 45 s for a member at `THINK_SCALE=1`
(`lib/scenarios.js`). The number of people browsing at the same time equals
the visit rate multiplied by the visit length. For 100,000 concurrent users,
of whom 60,000 are guests and 40,000 are signed in with the app open:

- `GUEST_RATE` = 60,000 / 30 = 2,000 visits/s, which is about 7,000 requests/s
- `SOCKETS` = 40,000
- `MEMBER_RATE` = 40,000 / 45 ≈ 900 visits/s if every signed-in user is
  browsing; set it to the share you expect

The 60/40 split is an assumption. Take the real numbers from the Users
dashboard once there is traffic.

## Limits

- The server rate-limits each client address, for example 50 GETs/s per
  server process (`internal/platform/httpx/ratelimit.go`). From one machine,
  every VU has the same address, and the run ends up measuring the limiter. To
  avoid that, set `SPREAD_ADDRESSES=true` and run k6 from an address the
  server's `TRUSTED_PROXIES` includes. In the cluster that is a pod in the
  stack's namespace (the pod network 10.42.0.0/16), calling
  `http://server-api:8080`. With `make run`, it is `TRUSTED_PROXIES=127.0.0.1/32`
  in `server/.env`. Without a trusted address, the server ignores the header.
- One k6 process holds a few tens of thousands of VUs. For 100,000, split the
  run across machines with `--execution-segment`. `exec.vu.idInTest` stays
  unique across segments, so the session file still maps one session to each
  VU.
- k6 running on the same node as the stack takes CPU from it. On the
  single-node dev cluster, the numbers are lower than they would be on
  separate machines.
- Signed-in search calls the model through Bifrost, and the rate limits of
  the caller `server-search` (the Search agent's callers in the console's AI
  settings) cap it. Each signed-in search in a member run is a paid model call.
- No scenario signs in: phone tokens come from Firebase and email codes are
  sent by mail. Only `trade` writes messages and moves on deals, among the
  load test's own accounts.

## Reading a run

- k6's summary gives each route's latency, and the targets in
  `lib/thresholds.js` cover the routes people wait on most one by one, so a
  slow search does not hide behind fast listing screens. A `stress` run
  stops a minute after it first misses a target: the level it reached is the
  answer.
- With `PGSTATS_URL`, `results/pgstats-*.txt` lists the statements that took
  the database's time during the run, by total and by mean. With `make run`:
  `PGSTATS_URL=postgres://server:server@localhost:56432/server`.
- With `K6_OTLP`, k6's metrics land in the stack's Prometheus beside the
  Server dashboard's Saturation row: `K6_OTLP=localhost:56318` with
  `make run`, `K6_OTLP=lgtm-dev:4318` on the compose network.

## Data size

Load alone misses the limit search meets first: the vector and BM25 indexes
must fit in Postgres's memory, and they grow with the listings. Before
trusting a result for more listings than the stack holds, load more with
seedpack (500,000, then 1,000,000) and run `stress` again. Run once right
after Postgres restarts too: a cold index reads from disk and is ten to fifty
times slower than a warm one.

