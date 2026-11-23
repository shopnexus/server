# Load test

k6 scripts that act like ShopNexus users against the API. They report latency
and errors per route, and each run passes or fails against the thresholds in
`lib/thresholds.js`. Watch the Server dashboard's Saturation row while a run is
going: it shows which resource fills first.

## Scenarios

| Scenario | VU model | What one iteration does |
|---|---|---|
| `guest` | visits started per second (`GUEST_RATE`) | home, a search or a category, a listing, sometimes its seller |
| `member` | visits started per second (`MEMBER_RATE`) | home and unread counts, a search, a listing marked viewed; sometimes a save that it undoes, the inbox or the saved list |
| `socket` | open apps (`SOCKETS`) | opens `/v1/ws`, waits for `ready`, renews the access token over the socket, closes after about `SOCKET_MINUTES` and opens again |

`guest` and `member` start visits at a fixed rate whether or not earlier
visits have finished, the way real arrivals behave. When the server slows
down, visits pile up instead of the load easing off. `socket` keeps one VU per
open app, so its VU count equals the number of signed-in users online.

Every request carries the route as `api/openapi.yaml` writes it
(`/v1/screens/listing/{id}`) in the `name` tag, which matches the server's
`http_route`. Requests are also tagged with `journey`.

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

Against the umbrella's compose dev stack, run k6 on its network and point it
at the server container:

```sh
docker exec -w /src shopnexus-server-dev-1 go run ./cmd/loadsessions -sessions 2000 > loadtest/sessions.json
make loadtest LOADTEST_NETWORK=shopnexus_dev BASE_URL=http://server-dev:8080 MEMBER_RATE=2 SOCKETS=200
```

## Settings

| Variable | Default | Meaning |
|---|---|---|
| `BASE_URL` | `http://127.0.0.1:8080` | the API; `/v1/ws` uses the same host |
| `PROFILE` | `smoke` | the load shape, from `lib/profiles.js` |
| `GUEST_RATE` | `1` | guest visits started per second at the peak |
| `MEMBER_RATE` | `0` | signed-in visits started per second at the peak |
| `SOCKETS` | `0` | open sockets at the peak |
| `TIME_SCALE` | `1` | multiplies the length of every stage; `0.1` runs the profile in a tenth of the time |
| `THINK_SCALE` | `1` | multiplies the pauses between steps; `0` removes them |
| `SOCKET_MINUTES` | `10` | how long a socket stays open, give or take 20% |
| `SESSIONS_FILE` | `./sessions.json` | the output of `cmd/loadsessions`, relative to `loadtest/` |
| `SPREAD_ADDRESSES` | `false` | gives each VU its own address in 198.18.0.0/15 through `X-Forwarded-For` |
| `LOADTEST_NETWORK` | `host` | the Docker network `make loadtest` runs k6 in |

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
- Signed-in search calls the model through LiteLLM, and LiteLLM's key limits
  (`infra/stack/values-prod.yaml`) cap it. Each signed-in search in a member run is a paid model call.
- No scenario signs in: phone tokens come from Firebase and email codes are
  sent by mail. None writes chat messages or makes deals, because those notify
  real sellers and start scam checks.
