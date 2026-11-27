# ShopNexus Server

The open-source backend of ShopNexus, a C2C marketplace for used goods in Vietnam. It is one Go binary on Postgres, with [Restate](https://restate.dev) for the flows that must survive a crash.

![Home for a guest](https://raw.githubusercontent.com/shopnexus/.github/main/profile/media/home-demo.gif)

ShopNexus holds nobody's money. Every deal is cash on delivery: the buyer looks at the item at the door before paying the shipper. There is no wallet, no escrow and no dispute desk.

## Documentation

How the system works, flow by flow (architecture, API and operations), is at **[shopnexus.github.io/docs](https://shopnexus.github.io/docs/)**.

## Quick start

You need Go 1.27 and Docker.

```bash
make models # fetch the AI models (about 5 GB) once
make run    # start the dev stack and the server; the first run writes .env
make test   # every test, then golangci-lint
make down   # stop everything
```

## Contributing

Issues and pull requests are welcome. Read [AGENTS.md](AGENTS.md) for the code conventions before you start, and make sure `make test` passes.

## License

[AGPL-3.0](LICENSE). Code from before 2026-11-23 was released under MIT; see [NOTICE](NOTICE).
