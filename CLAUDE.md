# server2

Read `ARCHITECTURE.md` before changing code. It records design decisions and
overrides the older spec in `~/shopnexus/specs/` where they disagree. Record
new decisions there, not in Claude's memory.

## Working rules

- Use the newest release of every component (Go, Postgres, Restate, libraries).
  Look the version up before pinning it; a release candidate is fine in
  development.
- Rename modules, entities, tables, functions and any other name, or
  restructure, whenever a better name or shape exists. Report the change;
  refactor effort, call sites included, is not a reason to keep a worse name.
- Design decisions go to the user one question at a time, with a check
  question at choices that touch security or data. Implementation of an
  approved plan runs without stopping to ask.
- Scratch files, logs and test output go in `tmp/` (git-ignored), never `/tmp`.
- A use case, Restate handler or HTTP handler stays one function that reads
  top to bottom. Do not split it into phase methods or small helpers called
  once: each hop is one more place a reader has to trace. The old
  `origin/cleancode` branch did this (`price()`, `reserve()`, `persist()` on a
  per-run struct), and the user did not like it there either. A helper is
  fine when several places call it.

## Commands

- `make run`: start the development stack and the server. The first run
  writes `.env` with fresh keys; migrations apply every run.
- `make test`: every test against the test stack, which it starts, then
  `golangci-lint run` (config in `.golangci.yml`; it includes `go vet` and
  gofumpt). After one `make test` the stack stays up, so
  `go test ./internal/auth/` runs a single package. `golangci-lint fmt`
  applies gofumpt.
- `make down`: stop both stacks.
