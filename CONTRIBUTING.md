# Contributing

Thanks for helping. Bug reports, fixes, docs and new ideas are all welcome.

## Before you start

- **Read the docs page of the part you change** at [shopnexus.github.io/docs](https://shopnexus.github.io/docs/). It says how each flow works and why.
- **Some things ShopNexus will not do.** It holds nobody's money: every deal is cash on delivery, with no wallet, no escrow and no dispute desk. A change that needs the platform to hold or move money will not be merged.
- **For anything larger than a fix, open an issue first** so we can agree on the shape before you write the code.

## Set up

You need Go 1.27 and Docker.

```bash
make models # fetch the AI models (about 5 GB) once
make run    # start the dev stack and the server; the first run writes .env
make test   # every test against the test stack, then golangci-lint
```

After `make test` the test stack stays up, so `go test ./internal/<module>/...` runs one module. `make down` stops both stacks.

## Shape of a change

- **The API spec comes first.** A feature starts in `api/` (OpenAPI): endpoints, schemas, errors. The data model and the code follow.
- **Docs change with the code.** When a change alters a flow, a rule or a limit, update its English page in the [docs repo](https://github.com/shopnexus/docs) in the same piece of work. Limits the requirements quote come from the code, so you do not copy numbers by hand.
- **Tests run on real stores.** There are no in-memory fakes: tests run on Postgres and Restate from the test stack.
- **Conventions are in [AGENTS.md](AGENTS.md).** It is written for coding agents and people alike: module layout, writes, flows, errors and tests. Read the sections your change touches.

## Pull requests

- Keep one change per pull request, with a short title that says what it does.
- `make test` and `go vet ./...` pass locally, and CI passes on the pull request.
- By contributing you agree that your work is released under [AGPL-3.0](LICENSE).

## Security

Please do not report a vulnerability in a public issue. Use [a private security advisory](https://github.com/shopnexus/server/security/advisories/new) instead.
