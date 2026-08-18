# Medication API

REST API for medication records, built with Go and PostgreSQL.

## Live deployment

A live instance is deployed on Render. Interactive API docs:
[medication-api-1j1b.onrender.com/docs](https://medication-api-1j1b.onrender.com/docs/)

## Requirements

- Go 1.26+
- Postgres 18
- Docker

## Run locally with Docker

```bash
docker compose up --build
```

Compose runs the `migrate` service to completion before starting the API, so local
development exercises the same migration runner used in production.

If you previously ran a version of this stack that seeded the schema through
`docker-entrypoint-initdb.d`, reset the volume once so the runner can record its
migrations: `docker compose down -v`.

## Database migrations

The production image contains the migration runner at `/migrate`. Run it as a
pre-deploy step against the target database, with `DATABASE_URL` set to that
database's connection string. The runner records applied versions in
`schema_migrations` and uses a PostgreSQL advisory lock to prevent concurrent
deploys from applying the same migration twice.

To run migrations locally against a configured database:

```bash
DATABASE_URL='postgres://user:password@localhost:5432/medications' go run ./cmd/migrate
```

Check process health and database readiness:


```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## API documentation

Interactive Swagger UI is served at `http://localhost:8080/docs`, backed by the raw
OpenAPI 3.1 spec at `http://localhost:8080/openapi.yaml` (source:
[`docs/openapi.yaml`](docs/openapi.yaml)). Both are served directly by the API binary,
so no external tooling is required.

## API usage

Create a medication (`201 Created`):

```bash
curl -X POST "http://localhost:8080/v1/medications" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Paracetamol","dosage":"500 mg","form":"tablet"}'
```

The response includes the generated `id`. Use it as in the examples below:

```json
{
	"id": "58072a4d-bed8-4eb8-b68b-337710483fc6",
	"name": "Paracetamol",
	"dosage": "500 mg",
	"form": "tablet"
}
```

List medications with pagination (`200 OK`; `limit` accepts 1–100 and `offset` is non-negative):

```bash
curl "http://localhost:8080/v1/medications?limit=20&offset=0"
```

Retrieve one medication (`200 OK`):

```bash
curl "http://localhost:8080/v1/medications/58072a4d-bed8-4eb8-b68b-337710483fc6"
```

Update one or more fields (`200 OK`):

```bash
curl -X PATCH "http://localhost:8080/v1/medications/58072a4d-bed8-4eb8-b68b-337710483fc6" \
  -H 'Content-Type: application/json' \
  -d '{"dosage":"750 mg"}'
```

Delete a medication (`204 No Content`):

```bash
curl -i -X DELETE "http://localhost:8080/v1/medications/58072a4d-bed8-4eb8-b68b-337710483fc6"
```

Inspect in-process HTTP counters in Prometheus text format:

```bash
curl "http://localhost:8080/metrics"
```

For request and response details, see [`docs/api-contract.md`](docs/api-contract.md).

## Test

Run the following command to execute all unit tests:

```bash
go test ./...
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `dev` | `dev`, `hom`, or `prod`. `dev` emits text logs; the others emit JSON logs. |
| `PORT` | `8080` | HTTP listening port. |
| `DATABASE_URL` | — | PostgreSQL connection URL. Any `pool_*` parameter is overridden by the settings below. |
| `DB_MAX_CONNS` | `10` | Maximum pooled database connections. |
| `DB_MIN_CONNS` | `2` | Connections kept warm; must not exceed `DB_MAX_CONNS`. |

## Health endpoints

- `GET /healthz`: process liveness check.
- `GET /readyz`: dependency readiness check.
- `GET /metrics`: HTTP request and response counters for local scraping.

## Observability

Every request carries an `X-Request-Id`: a caller-supplied value is reused when it is at
most 64 printable ASCII characters, otherwise the server generates one. It is echoed on
the response and included in the access log, so a client-reported failure can be traced
to its log line.

Responses in the 5xx range never expose the underlying failure to the client, but the
real error is logged at `ERROR` level with the request ID, method, and route.

`medication_http_responses_total` is labelled by `method`, `route` (the route pattern,
not the raw path), and `status`. Requests that match no route collapse into
`method="other",route="unmatched"` to bound label cardinality.

The API uses `/v1` versioning. Collection endpoints use `limit` and `offset` pagination. The server performs graceful shutdown and keeps its security timeouts as internal defaults.
