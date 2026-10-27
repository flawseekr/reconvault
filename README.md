# ReconVault

ReconVault records HTTP request/response exchanges in PostgreSQL and enriches
every recorded hostname with DNS, ASN and WAF data. You can browse, filter,
aggregate and search the records (headers and bodies included, e.g. for leaked
secrets) through a REST API and a web dashboard.

| Component | Path | What it does |
| --------- | ---- | ------------ |
| API | [`reconvault-api/`](reconvault-api) | Go HTTP API: executes requests on demand, stores them as artifacts, serves artifacts and recon data under `/api/v1` |
| DNS autoscan | [`reconvault-dns-autoscan/`](reconvault-dns-autoscan/README.md) | Go background service: fills `domains` (root domain, ASN, WAF) and versioned `dns_records` for every recorded hostname |
| Dashboard | [`reconvault-dashboard/`](reconvault-dashboard/README.md) | React UI, built into an nginx image that is also the single public entrypoint |

Reference documentation:

- [docs/API.md](docs/API.md): every endpoint, with parameters, responses, errors and curl examples
- [reconvault-api/api/swagger.json](reconvault-api/api/swagger.json): OpenAPI 3.0 spec, also served at `/swagger.json`
- [docs/DATABASE.md](docs/DATABASE.md): schema, migrations, queries and index analysis

## Architecture

```
                    host port APP_BIND:APP_PORT (default 0.0.0.0:8080)
                                         │
                              ┌──────────▼──────────┐
 browser / curl ─────────────►│ dashboard (nginx)   │  /                      → dashboard (SPA)
                              │ single entrypoint   │  /api/                  → api:8080
                              └──────────┬──────────┘  /readyz, /swagger.json → api:8080
                                         │ network "frontend"
                              ┌──────────▼──────────┐
                              │ api (Go)            │──► internet (network "egress")
                              └──────────┬──────────┘
                                         │ network "backend" (internal, no outside route)
 ┌──────────────────────┐     ┌──────────▼──────────┐
 │ dns-autoscan (Go)    │────►│ db (PostgreSQL 18)  │
 │ DNS + HTTP probes ──►│     └─────────────────────┘
 └──────────────────────┘     (internet via "egress")
```

- **One host for everything.** Only the nginx container publishes a port. The
  dashboard and the API share one origin, so the browser needs no CORS. The
  API and the database are not published to the host.
- **Bind address.** The entrypoint binds `0.0.0.0` by default, so it is
  reachable on every host interface. Inside their containers, nginx and the
  API both listen on `0.0.0.0:8080`.
- **Old prefix.** The API used to live under `/v1`. nginx answers `/v1/*` with
  `404` so stale clients fail clearly.

## API endpoints

All `/api/v1` endpoints require `Authorization: Bearer <API_KEY>`.

| Endpoint | Purpose |
| -------- | ------- |
| `POST /api/v1/http/request` | Execute an HTTP request and store it as an artifact |
| `GET /api/v1/http/artifacts` | List artifacts (sorting, hostname filter, cursor pagination) |
| `GET /api/v1/http/artifacts/{id}` | Get one artifact with all fields |
| `DELETE /api/v1/http/artifacts/{id}` | Permanently delete an artifact |
| `GET /api/v1/http/artifacts/search` | Case-insensitive substring search across all fields |
| `GET /api/v1/http/domains` | Per-hostname artifact count and last request time |
| `GET /api/v1/domains/{hostname}` | Root domain, ASN and WAF of a hostname (DNS autoscan data) |
| `GET /api/v1/dns_records/{hostname}` | Full DNS history of a hostname, newest first |
| `GET /api/v1/subdomains/{root_domain}` | Distinct hostnames under a root domain, with total |
| `GET /api/v1/root_domains` | Root domains with unique hostname counts |
| `GET /readyz` | Readiness probe: API and database (no auth) |
| `GET /swagger.json` | OpenAPI 3.0 document (no auth) |

The API also serves `GET /healthz` (liveness), reachable only on the internal
container network.

## Requirements

- Docker Engine with the Compose plugin (`docker compose`).
- Outbound internet access for the `api` (target requests) and `dns-autoscan`
  (DNS queries, Team Cymru ASN lookups over DNS, HTTP probes for WAF
  detection) containers.

Everything builds, runs and is tested in containers; Go, Node.js and
PostgreSQL do not need to be installed on the host. If your user is not in the
`docker` group, prefix the `docker` commands with `sudo`.

## Quick start

```sh
cp .env.example .env
# Replace the placeholder secrets:
sed -i "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=$(openssl rand -hex 24)/" .env
sed -i "s/^API_KEY=.*/API_KEY=$(openssl rand -hex 32)/" .env
chmod 600 .env

docker compose up -d --build --wait
docker compose ps        # db, api, dns-autoscan and dashboard: "healthy"
```

Open `http://<host>:8080/` (for example <http://127.0.0.1:8080>) and enter
`API_KEY` when the dashboard asks for it. The key is kept in the browser tab's
`sessionStorage` only.

From the command line:

```sh
set -a; . ./.env; set +a
BASE=http://127.0.0.1:8080
AUTH="Authorization: Bearer $API_KEY"

curl -sS "$BASE/readyz"
curl -sS -X POST "$BASE/api/v1/http/request" -H "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{"method":"GET","url":"https://example.com/"}'
curl -sS "$BASE/api/v1/http/artifacts" -H "$AUTH"
curl -sS "$BASE/api/v1/http/artifacts/search?q=example" -H "$AUTH"
curl -sS "$BASE/api/v1/root_domains" -H "$AUTH"
```

The DNS autoscan picks up new hostnames on its next active pass
(`AUTOSCAN_ACTIVE_INTERVAL`, default 1 minute). Until then the recon endpoints
return `404` or empty lists for them.

Stop the stack with `docker compose down`. `docker compose down -v` also
deletes the database volume and all recorded data.

## Configuration

`docker compose` reads `.env` (start from [`.env.example`](.env.example)).
Invalid values stop the affected service at startup with a message listing
every problem.

| Variable | Default | Used by | Description |
| -------- | ------- | ------- | ----------- |
| `POSTGRES_PASSWORD` | **required** | db, api, dns-autoscan | Database password; URL-safe characters only (it is embedded in `DATABASE_URL`) |
| `API_KEY` | **required** | api | Bearer token for `/api/v1/*`, at least 32 characters |
| `POSTGRES_DB` / `POSTGRES_USER` | `reconvault` | db, api, dns-autoscan | Database name and user |
| `POSTGRES_SSLMODE` | `disable` | api, dns-autoscan | `sslmode` of the database connection (`require`/`verify-full` for a remote database) |
| `APP_BIND` / `APP_PORT` | `0.0.0.0` / `8080` | dashboard | Host interface and port of the single entrypoint |
| `DB_MAX_CONNS` | `10` | api | API connection pool size (1–1000) |
| `LOG_LEVEL` | `info` | api, dns-autoscan | `debug`, `info`, `warn` or `error` |
| `UPSTREAM_TIMEOUT` | `30s` | api | Time limit of one outbound request including the body (1s–10m) |
| `MAX_REQUEST_BYTES` | `2097152` | api | Maximum incoming API request body in bytes (1 KiB–100 MiB). nginx additionally caps bodies at 2 MiB (`client_max_body_size`) |
| `MAX_RESPONSE_BODY_BYTES` | `10485760` | api | Maximum stored upstream response body in bytes (1 KiB–100 MiB); larger bodies are truncated |
| `ALLOW_PRIVATE_NETWORKS` | `false` | api, dns-autoscan | `true` disables SSRF protection (allows loopback, private and link-local targets) |
| `SHUTDOWN_TIMEOUT` | `15s` | api | Grace period for in-flight requests on SIGTERM (1s–5m) |
| `DNS_RESOLVERS` | `1.1.1.1:53,8.8.8.8:53,9.9.9.9:53` | dns-autoscan | Recursive resolvers, `ip[:port]`, comma-separated |
| `DNS_TIMEOUT` / `DNS_RETRIES` | `5s` / `2` | dns-autoscan | Per-query timeout and retries |
| `AUTOSCAN_ACTIVE_INTERVAL` | `1m` | dns-autoscan | Interval between the starts of active passes; the daily refresh runs at 00:00 UTC |
| `AUTOSCAN_BATCH_SIZE` | `100` | dns-autoscan | Hostnames claimed per batch |
| `AUTOSCAN_CONCURRENCY` | `16` | dns-autoscan | Hostnames scanned in parallel |
| `AUTOSCAN_HTTP_PROBE_TIMEOUT` | `10s` | dns-autoscan | Timeout of the WAF detection probes |
| `AUTOSCAN_DB_MAX_CONNS` | `10` | dns-autoscan | Autoscan connection pool size |
| `AUTOSCAN_SHUTDOWN_TIMEOUT` | `30s` | dns-autoscan | How long to wait for cancelled scans to stop on SIGTERM |

Valid ranges of the autoscan settings are listed in
[its README](reconvault-dns-autoscan/README.md#configuration). Requests
rejected by nginx itself (bodies over 2 MiB, API unreachable or timing out)
get nginx's HTML error pages rather than the API's JSON envelope.

Running the API binary outside Compose: set `DATABASE_URL` (e.g.
`postgres://user:pass@host:5432/db?sslmode=require`), `API_KEY` and optionally
`HTTP_ADDR` (default `:8080`, all interfaces).

## Production deployment

1. **Prepare the host.** Install Docker with the Compose plugin and clone the
   repository.
2. **Configure secrets.** Create `.env` as in the quick start, with fresh
   random values for `POSTGRES_PASSWORD` and `API_KEY`, and `chmod 600 .env`.
   `.env` is git-ignored.
3. **Put TLS in front.** The entrypoint speaks plain HTTP and the dashboard
   has no login of its own: anyone who can reach it and knows `API_KEY` has
   full access to recorded data, which may include credentials. For a public
   deployment either:
   - terminate TLS in a load balancer or host reverse proxy that forwards to
     `APP_PORT`, and set `APP_BIND=127.0.0.1` if that proxy runs on the same
     host; or
   - keep `APP_BIND=0.0.0.0` only on a trusted network and restrict the port
     with a firewall.
4. **Start.** `docker compose up -d --build --wait`. Containers restart
   automatically (`restart: unless-stopped`), and migrations run on startup.
5. **Verify.** `docker compose ps` shows every service `healthy`, and
   `curl -sS http://127.0.0.1:${APP_PORT:-8080}/readyz` returns
   `{"status":"ok"}`.

Upgrades: pull the new revision and run `docker compose up -d --build --wait`
again. Migrations are forward-only and applied automatically.

## Operations

- **Health:** every container has a Docker health check. The API's and the
  autoscan's run a `healthcheck` subcommand of their binary that probes their
  own `/readyz` (database included); nginx's checks that it serves `/`.
  Services start in dependency order: db → api → dns-autoscan and dashboard.
- **Logs:** the API and the autoscan log structured JSON to stdout; nginx
  writes one access-log line per request to stdout, with the path but
  without the query string or referer:
  `docker compose logs -f api dashboard dns-autoscan`. Behind a TLS proxy,
  the logged client address is the proxy's.
- **Migrations:** applied automatically at startup under a PostgreSQL
  advisory lock (safe with several replicas); already-applied versions are
  skipped. The API and the autoscan track theirs separately
  (`schema_migrations`, `autoscan_schema_migrations`).
- **Shutdown:** on SIGTERM the API stops accepting connections and drains
  in-flight requests for up to `SHUTDOWN_TIMEOUT`. The autoscan cancels
  running scans (partial results are not stored) and waits up to
  `AUTOSCAN_SHUTDOWN_TIMEOUT` for them to stop.
- **Rotate the API key:** change `API_KEY` in `.env` and run
  `docker compose up -d`. Dashboard users are asked for the new key.
- **Backup:**
  `docker compose exec -T db pg_dump -U reconvault reconvault > backup.sql`
  (adjust user and database if you changed `POSTGRES_USER`/`POSTGRES_DB`).
- **Restore** into an empty database:
  `docker compose exec -T db psql -U reconvault reconvault < backup.sql`.

## Testing

All tests run in containers against a throwaway PostgreSQL 18 on tmpfs:

```sh
docker compose -f docker-compose.test.yaml run --rm tests               # API: gofmt, vet, unit + integration tests
docker compose -f docker-compose.test.yaml run --rm dns-autoscan-tests  # DNS autoscan: gofmt, vet, unit + integration tests
docker compose -f docker-compose.test.yaml run --rm dashboard-tests     # dashboard: typecheck, lint, tests, production build
docker compose -f docker-compose.test.yaml down -v
```

- **API:** configuration parsing; request validation (methods, URLs, header
  injection, limits); the fetcher (exact headers, no redirects, truncation,
  timeouts, ignored TLS errors, SSRF blocking); every handler (auth, strict
  JSON, parameter validation, error mapping, 404/405, removal of the old `/v1`
  prefix); the OpenAPI contract (routes and response fields match the code);
  migrations (rollback of a failing one, concurrent runs); SQL round-trips,
  sorting, cursor pagination, search and the recon queries against
  PostgreSQL; end-to-end flows through the real HTTP stack.
- **DNS autoscan:** see [its README](reconvault-dns-autoscan/README.md#testing).
- **Dashboard:** see [its README](reconvault-dashboard/README.md#testing).

Integration tests skip when `TEST_DATABASE_URL` is unset; the test compose
file sets it.

Dependency vulnerability scans (Go modules and npm packages):

```sh
for m in reconvault-api reconvault-dns-autoscan; do
  docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -e GOMODCACHE=/tmp/mod -e GOCACHE=/tmp/cache \
    -v "$PWD/$m:/src:ro" -w /src golang:1.27-alpine \
    go run golang.org/x/vuln/cmd/govulncheck@latest ./...
done
docker run --rm -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$PWD/reconvault-dashboard:/src:ro" node:24-alpine \
  sh -c 'cp /src/package.json /src/package-lock.json /tmp/ && cd /tmp && npm audit'
```

## Project layout

```
.
├── docker-compose.yaml        # production stack: db, api, dns-autoscan, dashboard (entrypoint)
├── docker-compose.test.yaml   # containerized test runners + throwaway PostgreSQL
├── .env.example               # configuration template
├── docs/                      # API reference, database design
├── reconvault-api/
│   ├── Dockerfile             # multi-stage, distroless, non-root
│   ├── api/swagger.json       # OpenAPI 3.0 spec (embedded, served at /swagger.json)
│   ├── cmd/reconvault-api/    # main: config, wiring, graceful shutdown, healthcheck subcommand
│   └── internal/
│       ├── config/            # environment configuration + validation
│       ├── artifact/          # artifact types, validation, service
│       ├── recon/             # read model of the autoscan tables
│       ├── fetcher/           # outbound HTTP client with SSRF guard and limits
│       ├── postgres/          # repositories (SQL)
│       ├── database/          # pgx pool, embedded migrations + runner
│       ├── httpapi/           # routing, handlers, middleware, JSON errors
│       └── testdb/            # integration-test helpers
├── reconvault-dns-autoscan/   # see its README
└── reconvault-dashboard/      # see its README (src/, nginx/ config, Dockerfile)
```

The API depends only on the Go standard library and
[`pgx`](https://github.com/jackc/pgx). The autoscan adds `miekg/dns`,
`projectdiscovery/cdncheck` and `golang.org/x/net`.

## Security

- **Authentication:** every `/api/v1` endpoint requires the bearer key,
  compared in constant time on SHA-256 digests. The key is checked before
  routing, so a 401 doesn't reveal whether a route exists.
- **Network isolation:** only the nginx entrypoint is published. The database
  sits on an internal network without outside access; the API is reachable
  only through nginx.
- **SSRF protection:** outbound connections to non-public addresses are
  refused, checked on the resolved IP at connect time (defeats DNS
  rebinding). Redirects are not followed and proxy environment variables are
  ignored.
- **TLS errors on targets are ignored by design:** self-signed, expired and
  mismatched certificates are recorded like any other target.
- **Input validation:** strict JSON (unknown fields and trailing data
  rejected), header validation (no CR/LF injection), size limits, whitelisted
  sort identifiers. All SQL uses bind parameters.
- **Information hygiene:** internal errors are logged, never returned. The
  API's request logs and nginx's access log omit query strings (search terms
  may be secrets); the API's also omit headers and bodies.
  API responses carry `Cache-Control: no-store` and `X-Content-Type-Options:
  nosniff`.
- **Dashboard:** strict Content-Security-Policy (scripts from `'self'` only),
  `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`. Captured HTML is
  shown as text and never rendered. The API key is never built into the
  image.
- **Containers:** `api`, `dns-autoscan` and `dashboard` run as non-root
  users with read-only root filesystems, all capabilities dropped and
  `no-new-privileges`; the Go images are distroless. `db` uses the stock
  `postgres` image defaults and is reachable only on the internal network.

## Known limitations

- **Plain HTTP and no user accounts.** TLS termination and any user
  authentication beyond the shared API key must be provided in front of the
  entrypoint (see [Production deployment](#production-deployment)).
- **No rate limiting.** Add it at the TLS-terminating proxy if needed.
- **Raw responses are reconstructed, not byte-exact.** Header names are
  canonicalized and sorted. Rows recorded before migration 0004 hold the body
  only.
- **Bodies are stored as text.** Invalid UTF-8 and NUL bytes become U+FFFD;
  `response_body_size_bytes` still reports the raw byte count. Bodies over
  `MAX_RESPONSE_BODY_BYTES` are truncated.
- **Unindexed sorts.** Sorting by `full_url` or `response_content_type`
  without a hostname filter scans the table (see
  [docs/DATABASE.md](docs/DATABASE.md)).
- **Aggregations scan their tables.** `/api/v1/http/domains` and
  `/api/v1/root_domains` aggregate on every call, so their cost grows with
  the row count.
- **Limited pagination.** Only `GET /api/v1/http/artifacts` is paginated.
  Search, domains and root domains are bounded by `limit`; DNS history and
  subdomain lists are returned whole.
