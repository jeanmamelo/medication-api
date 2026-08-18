# AGENTS.md

Instructions for AI coding agents (Claude Code, Codex, Cursor, etc.) working in this
repository. See [`docs/api-contract.md`](docs/api-contract.md) for the public HTTP
contract and [`README.md`](README.md) for human-facing setup docs.

## Project overview

REST API for medication records, written in Go against PostgreSQL. Single module, no
external web framework — routing is `net/http`'s `ServeMux` with Go 1.22+ method+path
patterns.

## Setup commands

```bash
go build ./...                 # build everything
go test ./...                  # run all tests
go test ./internal/medication/ # run tests for one package
go test ./internal/httpapi/ -run TestHealthz  # run a single test
go vet ./...

docker compose up --build      # run API + Postgres locally; the migrate service
                                # applies migrations before the API container starts

# Apply migrations against an already-running database:
DATABASE_URL='postgres://user:password@localhost:5432/medications' go run ./cmd/migrate
```

There is no separate lint config and no CI workflow yet — `go build ./...`, `go vet
./...`, and `go test ./...` are the only checks, and an agent should run all three
before considering a change finished.

## Architecture

Standard Go layout, dependency direction flows inward toward `internal/medication`:

- `cmd/api` — process entrypoint. Loads `Config`, opens the `pgxpool.Pool`, wires
  `postgres.Repository` → `medication.Service` → `httpapi.NewRouterWithLogger`, and runs
  the HTTP server with signal-based graceful shutdown.
- `cmd/migrate` — standalone binary that applies `migrations/*.up.sql` in version order
  inside a transaction each, tracked in a `schema_migrations` table, guarded by a
  Postgres advisory lock (`pg_advisory_lock`) so concurrent deploys can't double-apply.
  This is the binary copied into the production image and run as a pre-deploy step
  (see README).
- `internal/medication` — domain package: the `Medication` type, `Service` (business
  logic: validation, trimming, pagination bounds), and the `Repository`/`IDGenerator`
  interfaces it depends on. Has no knowledge of HTTP or SQL. `RandomUUIDGenerator`
  produces UUIDv4 without external dependencies.
- `internal/postgres` — implements `medication.Repository` against pgx. Depends on a
  small `pool` interface (not `*pgxpool.Pool` directly) so it's mockable in tests. Maps
  `pgx.ErrNoRows` / zero `RowsAffected` to `medication.ErrNotFound`, and unique-key
  violations to `medication.ErrConflict`.
- `internal/httpapi` — the transport layer: routes, JSON encode/decode, request
  validation, and error mapping (`writeServiceError` translates domain errors to HTTP
  status codes). Also owns cross-cutting middleware applied once at the edge:
  `withRequestID` (correlation IDs), `requestObservability` (structured request logging
  + in-process `Metrics` counters exposed at `/metrics` in Prometheus text format), and
  `securityHeaders` (CSP, nosniff, frame-deny, no-referrer, no-store). `MedicationService`
  and `Readiness` are interfaces defined here and satisfied by `medication.Service` and
  `postgres.Repository`, keeping this package decoupled from concrete implementations.
- `internal/config` — reads and validates env vars once at startup (`Load()`), fails
  fast on invalid config. `APP_ENV` controls log format: `dev` → text logs, `hom`/`prod`
  → JSON logs.
- `migrations/` — plain numbered SQL files (`NNNNNN_description.{up,down}.sql`),
  embedded into the `migrate` binary via `migrations/embed.go` (`//go:embed *.sql`). New
  migrations go here; version numbers are parsed from the filename and must be unique.

### Request flow

`net/http` → `withRequestID` → `requestObservability` → `securityHeaders` → `ServeMux`
→ handler → `MedicationService` (interface, impl: `medication.Service`) →
`medication.Repository` (interface, impl: `postgres.Repository`) → Postgres.

## Code style and conventions

- Handlers depend on interfaces (`MedicationService`, `Readiness`, `Repository`,
  `IDGenerator`), not concrete types — this is what makes each layer testable in
  isolation with hand-written fakes (see `*_test.go` files for the pattern).
- `UpdateInput` fields are pointers so an absent JSON field is distinguishable from an
  explicit empty value in PATCH requests. `Update` merges fields server-side in a single
  statement (`COALESCE` in the SQL), so concurrent partial updates of different fields
  cannot overwrite each other.
- Domain errors (`medication.ErrNotFound`, `medication.ErrConflict`,
  `medication.ErrInvalidPagination`, `medication.ValidationError`) are defined in
  `internal/medication` and translated to HTTP responses only in `internal/httpapi`;
  don't leak `pgx` or SQL errors past `internal/postgres`.
- No comments explaining *what* code does — names should do that. A comment is only
  worth adding for a non-obvious *why* (a hidden constraint, a workaround, a subtle
  invariant), matching the existing style throughout this codebase.
- Table-driven tests are the default for anything with more than two cases (see
  `TestMedicationEndpointsMapServiceErrors` in `router_test.go` for the pattern).

## Testing instructions

- Run `go build ./...`, `go vet ./...`, and `go test ./...` before calling a change
  done; there is no separate CI pipeline enforcing this yet, so an agent is the only
  gate.
- Prefer adding a test alongside any behavior change rather than only testing manually.
- `internal/httpapi` tests use hand-written stubs (`medicationServiceStub`) instead of
  a mocking library; `internal/postgres` tests use a hand-written `fakePool` /
  `fakeRow` / `fakeRows` implementing the narrow `pool` interface. Follow the existing
  pattern rather than introducing a new one.

## Security considerations

This API stores medication records, so treat correctness around access and error
disclosure as a first-class concern, not a nice-to-have:

- Never let a response reveal whether a resource ID exists when the caller has no
  legitimate need to know (e.g. `DELETE` is idempotent and always returns `204`,
  regardless of prior existence — see `docs/api-contract.md`).
- Internal/unexpected errors (`writeServiceError`'s default case) must never leak
  driver-level or implementation detail (SQL, pgx errors, stack traces) to the client;
  log the real error server-side with the request ID and return a generic
  `internal_error` message instead.
- Validate all untrusted input at the transport boundary (`internal/httpapi`) — request
  body size is bounded, JSON decoding rejects unknown fields, pagination bounds are
  enforced centrally via `medication.ValidatePagination`.
- Keep `securityHeaders` (CSP, nosniff, frame-deny, no-referrer, no-store) applied to
  every response; don't add routes that bypass the shared middleware chain in
  `NewRouterWithLogger`.

## PR / commit conventions

- Commit subjects use a Conventional-Commits-style prefix (`feat:`, `fix:`, `test:`,
  `docs:`, `chore:`), imperative mood, under ~70 characters; see `git log` for examples.
  Body lines explain *why*, not a restatement of the diff.
- Keep commits scoped to one logical change; when a set of edits mixes an unrelated
  feature with a fix, split them into separate commits rather than bundling everything
  into one.
- Update `docs/api-contract.md` alongside any change to `internal/httpapi`
  request/response handling — it documents the public HTTP contract and must not drift
  from the implementation.
