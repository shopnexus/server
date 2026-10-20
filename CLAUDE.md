# server2

Read `ARCHITECTURE.md` before changing code. It records design decisions and
overrides the older spec in `~/shopnexus/specs/` where they disagree. Record
new decisions there, not in Claude's memory.

## Working rules

- Use the newest release of every component (Go, Postgres, Restate, libraries).
  Look the version up before pinning it; a release candidate is fine in
  development.
- Rename modules, entities and tables, or restructure, whenever the design
  calls for it. Report the change; refactor effort is not a reason to keep a
  worse name.
- Design decisions go to the user one question at a time, with a check
  question at choices that touch security or data. Implementation of an
  approved plan runs without stopping to ask.
- Scratch files, logs and test output go in `tmp/` (git-ignored), never `/tmp`.

## Commands

- `make up`, `make down`: development stack.
- `make migrate`, `make run`: apply migrations, start the server.
- `make test-up` once, then `make test` (unit, integration, e2e) and `make lint`.
