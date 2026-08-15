# Medication API

REST API for medication records, built with Go and PostgreSQL.

## Requirements

- Go 1.26+

## Run locally

```bash
go run ./cmd/api
```

The server listens on `http://localhost:8080` by default.

```bash
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz
```

## Run with Docker

```bash
docker compose up --build
```

The Compose database applies the initial migration when its volume is created.

## API

With the API running, set a base URL:

```bash
API_URL=http://localhost:8080
```

Create a medication (`201 Created`):

```bash
curl -X POST "$API_URL/v1/medications" \
  -H 'Content-Type: application/json' \
  -d '{"name":"Paracetamol","dosage":"500 mg","form":"tablet"}'
```

The response includes the generated `id`. Use it in the examples below:

```bash
MEDICATION_ID='<id returned by POST>'
```

List medications with pagination (`200 OK`; `limit` accepts 1–100 and `offset` is non-negative):

```bash
curl "$API_URL/v1/medications?limit=20&offset=0"
```

Retrieve one medication (`200 OK`):

```bash
curl "$API_URL/v1/medications/$MEDICATION_ID"
```

Update one or more fields (`200 OK`):

```bash
curl -X PATCH "$API_URL/v1/medications/$MEDICATION_ID" \
  -H 'Content-Type: application/json' \
  -d '{"dosage":"750 mg"}'
```

Delete a medication (`204 No Content`):

```bash
curl -i -X DELETE "$API_URL/v1/medications/$MEDICATION_ID"
```

Check process health and database readiness:

```bash
curl "$API_URL/healthz"
curl "$API_URL/readyz"
```

For request and response details, see [`docs/api-contract.md`](docs/api-contract.md).

## Test

```bash
go test ./...
```

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `APP_ENV` | `dev` | `dev`, `hom`, or `prod`. `dev` emits text logs; the others emit JSON logs. |
| `PORT` | `8080` | HTTP listening port. |
| `DATABASE_URL` | — | PostgreSQL connection URL. |

## Health endpoints

- `GET /healthz`: process liveness check.
- `GET /readyz`: dependency readiness check.

The API uses `/v1` versioning. Collection endpoints use `limit` and `offset` pagination. The server performs graceful shutdown and keeps its security timeouts as internal defaults.
