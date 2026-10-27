# ReconVault API Reference

The API is served under the `/api/v1` prefix. In the Docker deployment it is
reached through the single entrypoint (nginx) that also serves the
dashboard, e.g. `http://<host>:8080/api/v1/...` (`APP_PORT`, default
`8080`). The API container itself is not published.

All request and response bodies are JSON (`application/json; charset=utf-8`).
Timestamps are RFC 3339 in UTC with up to microsecond precision, e.g.
`2026-10-27T12:30:00.123456Z`.

A machine-readable OpenAPI 3.0 description of every endpoint is in
[`reconvault-api/api/swagger.json`](../reconvault-api/api/swagger.json) and is
served at `GET /swagger.json` without authentication. A contract test fails
the build if the spec's routes or response fields drift from the code.

## Contents

- [Authentication](#authentication)
- [Errors](#errors)
- [POST /api/v1/http/request](#post-apiv1httprequest)
- [GET /api/v1/http/artifacts](#get-apiv1httpartifacts)
- [GET /api/v1/http/artifacts/{id}](#get-apiv1httpartifactsid)
- [DELETE /api/v1/http/artifacts/{id}](#delete-apiv1httpartifactsid)
- [GET /api/v1/http/artifacts/search](#get-apiv1httpartifactssearch)
- [GET /api/v1/http/domains](#get-apiv1httpdomains)
- [GET /api/v1/domains/{hostname}](#get-apiv1domainshostname)
- [GET /api/v1/dns_records/{hostname}](#get-apiv1dns_recordshostname)
- [GET /api/v1/subdomains/{root_domain}](#get-apiv1subdomainsroot_domain)
- [GET /api/v1/root_domains](#get-apiv1root_domains)
- [Health checks](#health-checks)
- [OpenAPI document](#openapi-document)

---

## Authentication

Every `/api/v1/*` endpoint requires the API key configured in `API_KEY`:

```
Authorization: Bearer <API_KEY>
```

Missing or wrong keys return `401 Unauthorized` with a
`WWW-Authenticate: Bearer realm="reconvault"` header. The key is checked
before routing, so an unknown path under `/api/` also returns `401` without a
valid key (and `404` with one). `/readyz`, `/healthz` and `/swagger.json` are
unauthenticated and expose no recorded data.

The examples below assume:

```sh
export BASE=http://127.0.0.1:8080        # the entrypoint (APP_PORT)
export AUTH="Authorization: Bearer $API_KEY"
```

## Errors

Every error returned by the API uses the same envelope. (Responses produced
by the nginx entrypoint itself are not JSON: `413` for bodies over 2 MiB,
`502`/`504` when the API is unreachable or too slow, and `404` for the old
`/v1/` prefix.)

```json
{
  "error": {
    "code": "validation_error",
    "message": "limit: must be an integer between 1 and 100",
    "field": "limit"
  }
}
```

| Field         | Type   | Description                                                        |
| ------------- | ------ | ------------------------------------------------------------------ |
| `code`        | string | Machine-readable error code (table below)                          |
| `message`     | string | Human-readable description                                         |
| `field`       | string | Offending input field, for validation errors only (omitted otherwise) |
| `artifact_id` | string | ID of the recorded failed attempt, for upstream errors only (omitted otherwise) |

| Status | `code`                    | When                                                                          |
| ------ | ------------------------- | ----------------------------------------------------------------------------- |
| 400    | `bad_request`             | Malformed JSON, unknown fields, wrong types, trailing data, empty body        |
| 400    | `validation_error`        | Invalid parameter or field value (see each endpoint)                          |
| 401    | `unauthorized`            | Missing or invalid API key                                                    |
| 403    | `destination_not_allowed` | Target resolves to a non-public address (SSRF protection)                     |
| 404    | `not_found`               | Unknown artifact ID, unscanned hostname (`domain not found`) or route         |
| 405    | `method_not_allowed`      | Route exists but not for this method (`Allow` header lists methods)           |
| 413    | `payload_too_large`       | Request body exceeds `MAX_REQUEST_BYTES`                                      |
| 415    | `unsupported_media_type`  | `Content-Type` is not `application/json`                                      |
| 500    | `internal_error`          | Unexpected server error (details are logged, never returned)                  |
| 502    | `upstream_request_failed` | Target could not be reached (DNS, connection, TLS handshake, protocol error)  |
| 503    | `service_unavailable`     | `/readyz` only: database unreachable                                          |
| 504    | `upstream_timeout`        | Target did not respond within `UPSTREAM_TIMEOUT`                              |

---

## POST /api/v1/http/request

Sends an HTTP request to the given URL and stores the request and response
as an artifact in `http_artifacts`.

### Request body

| Field     | Type   | Required | Description                                                                   |
| --------- | ------ | -------- | ----------------------------------------------------------------------------- |
| `method`  | string | Yes      | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS` or `TRACE` (case-insensitive, stored upper-case) |
| `url`     | string | Yes      | Absolute `http://` or `https://` URL, max 8192 characters, host max 253 characters |
| `headers` | object | No       | Header name → string value. Max 100 entries                                   |
| `body`    | string | No       | Request body, sent as-is                                                      |

Header rules:

- Names must be valid RFC 9110 tokens. Names are case-insensitive, so two
  entries differing only in case are rejected as duplicates.
- Values must be valid UTF-8 without control characters (tab is allowed).
  This prevents header injection.
- If `User-Agent` is absent or empty, the request is sent with
  `User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36`.
- `Host` overrides the HTTP `Host` header (virtual hosting). The connection
  still goes to the host in `url`.
- `Content-Length` and `Transfer-Encoding` are computed by the client.

### Behavior

- **TLS certificate errors are always ignored.** This is a project
  requirement. Self-signed, expired, untrusted and hostname-mismatched
  certificates are accepted, as are TLS 1.0/1.1 servers, so every HTTPS
  target can be recorded as it is. The SSRF address policy still applies.
- **Redirects are not followed.** The 3xx response is recorded as returned.
- **No transparent compression.** Only the headers you specify (plus the
  default `User-Agent`) are sent, and the body is stored exactly as the
  server sent it.
- **SSRF protection.** Requests to loopback, private (RFC 1918 / ULA),
  link-local (including cloud metadata `169.254.169.254`), CGNAT, multicast,
  reserved and documentation ranges are rejected with `403`. The check runs
  on the *resolved* IP of every connection, so DNS tricks and rebinding
  cannot bypass it. Disable it only in trusted environments
  (`ALLOW_PRIVATE_NETWORKS=true`).
- **Raw response.** `response` holds the full response as text in HTTP/1.x
  wire format: status line, every response header, a blank line, then the
  body, with CRLF line endings. The status line shows the protocol actually
  negotiated, e.g. `HTTP/1.1 200 OK` or `HTTP/2.0 200 OK`. `net/http` doesn't expose the original
  bytes, so header names are canonicalized (`content-type` becomes
  `Content-Type`) and sorted by name. `Transfer-Encoding` is restored when
  the body was chunked.
- **Body limit.** At most `MAX_RESPONSE_BODY_BYTES` (default 10 MiB) of the
  response body is stored. Headers are limited to 1 MiB separately. Larger bodies are truncated and
  `response_body_truncated` is `true`. `response_body_size_bytes` then
  holds the declared `Content-Length` when available, otherwise the stored
  size.
- **Text storage.** Bodies and the raw response are stored in `TEXT` columns. Invalid UTF-8
  sequences and NUL bytes are replaced with U+FFFD. `response_body_size_bytes`
  counts raw bytes, before that substitution.
- **Failed attempts are recorded.** If the request was attempted but failed
  (DNS, refused, TLS, timeout), an artifact is stored with all `response_*`
  fields `null`, and its ID is returned in `error.artifact_id`. Validation
  failures and SSRF-blocked requests are **not** stored.

### Responses

**`201 Created`**: `Location: /api/v1/http/artifacts/{id}`. The body is the
stored artifact (same fields as
[GET /api/v1/http/artifacts/{id}](#get-apiv1httpartifactsid)) plus
`response_body_truncated`.

Errors: `400`, `401`, `403`, `413`, `415`, `500`, `502`, `504`.

### Example

```sh
curl -sS -X POST "$BASE/api/v1/http/request" -H "$AUTH" \
  -H 'Content-Type: application/json' \
  -d '{
    "method": "POST",
    "url": "https://api.example.com/v1/resource",
    "headers": { "Content-Type": "application/json" },
    "body": "{\"id\":123,\"name\":\"test\"}"
  }'
```

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "http_method": "POST",
  "hostname": "api.example.com",
  "endpoint_path": "/v1/resource",
  "full_url": "https://api.example.com/v1/resource",
  "requested_at": "2026-10-27T12:30:00.123456Z",
  "response_status_code": 200,
  "response_content_type": "application/json",
  "response_body_size_bytes": 16,
  "request_headers": {
    "Content-Type": "application/json",
    "User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"
  },
  "request_body": "{\"id\":123,\"name\":\"test\"}",
  "response": "HTTP/1.1 200 OK\r\nContent-Length: 16\r\nContent-Type: application/json\r\nDate: Tue, 27 Oct 2026 12:30:00 GMT\r\n\r\n{\"success\":true}",
  "response_body_truncated": false
}
```

Self-signed or otherwise invalid certificates need no extra options:

```sh
curl -sS -X POST "$BASE/api/v1/http/request" -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"method": "GET", "url": "https://self-signed.badssl.com/"}'
```

Upstream failure:

```json
{
  "error": {
    "code": "upstream_request_failed",
    "message": "the upstream request failed",
    "artifact_id": "89cb559a-bdf4-4592-898e-9039bb62fdce"
  }
}
```

---

## GET /api/v1/http/artifacts

Lists artifacts with sorting, an optional hostname filter and cursor-based
pagination.

### Query parameters

| Parameter    | Default        | Description                                                                                                                     |
| ------------ | -------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| `sort_by`    | `requested_at` | `http_method`, `full_url`, `response_status_code`, `response_content_type`, `response_body_size_bytes`, `requested_at`          |
| `sort_order` | `DESC`         | `ASC` or `DESC` (case-insensitive)                                                                                              |
| `hostname`   | (none)         | Exact hostname match, case-insensitive, max 255 bytes. Must not be empty if present                                             |
| `limit`      | `30`           | Integer from `1` to `100`: the page size                                                                                        |
| `cursor`     | (none)         | Opaque `meta.next_cursor` value from the previous page                                                                          |

Rows with equal sort values are ordered by `id` in the same direction, so
the order is total and deterministic. `null` values follow PostgreSQL
semantics: last for `ASC`, first for `DESC`.

### Pagination

Pagination is keyset (cursor) based:

1. Request the first page as usual.
2. If `meta.next_cursor` is not `null`, request the next page with
   `?cursor=<next_cursor>`. The cursor remembers `sort_by`, `sort_order`
   and `hostname`, so you don't need to repeat them. `limit` can change
   between pages.
3. Stop when `meta.next_cursor` is `null`.

How cursors behave:

- **Stable while new data arrives.** Artifacts recorded while you page
  don't shift or duplicate rows across pages. With the default
  newest-first order, new artifacts simply don't appear in later pages.
- **Fast at any depth.** Each page is an index range scan starting right
  after the previous page, not an `OFFSET` that re-reads skipped rows.
- **Validated.** A malformed or tampered cursor returns `400` with
  `field: "cursor"`. So does a cursor combined with explicit
  `sort_by`, `sort_order` or `hostname` values that differ from the ones
  it was issued for.
- **Safe with deletions.** A cursor stores sort values rather than a row
  reference, so paging continues correctly even if the row it came from
  was deleted.

### Response

**`200 OK`**

| Field                      | Type            |
| -------------------------- | --------------- |
| `id`                       | string (UUID)   |
| `http_method`              | string          |
| `full_url`                 | string          |
| `response_status_code`     | integer \| null |
| `response_content_type`    | string \| null  |
| `response_body_size_bytes` | integer \| null |
| `requested_at`             | datetime        |

```json
{
  "data": [
    {
      "id": "15f93681-261f-4571-8919-8e7fbd67aa1e",
      "http_method": "POST",
      "full_url": "https://api.example.com/v1/resource",
      "response_status_code": 200,
      "response_content_type": "application/json",
      "response_body_size_bytes": 627,
      "requested_at": "2026-10-27T14:01:42.629027Z"
    }
  ],
  "meta": {
    "sort_by": "requested_at",
    "sort_order": "DESC",
    "hostname": "api.example.com",
    "limit": 10,
    "next_cursor": "eyJzIjoicmVxdWVzdGVkX2F0IiwibyI6IkRFU0MiLCJoIjoiYXBpLmV4YW1wbGUuY29tIiwidiI6IjIwMjYtMTAtMjdUMTQ6MDE6NDIuNjI5MDI3WiIsImkiOiIxNWY5MzY4MS0yNjFmLTQ1NzEtODkxOS04ZTdmYmQ2N2FhMWUifQ"
  }
}
```

`meta.hostname` is `null` when no filter is applied. `meta.next_cursor` is
`null` on the last page.

Errors: `400` (invalid `sort_by`, `sort_order`, `limit`, `hostname` or `cursor`), `401`, `500`.

### Examples

```sh
curl -sS "$BASE/api/v1/http/artifacts" -H "$AUTH"
curl -sS "$BASE/api/v1/http/artifacts?hostname=api.example.com" -H "$AUTH"
curl -sS "$BASE/api/v1/http/artifacts?sort_by=response_status_code&sort_order=ASC" -H "$AUTH"
curl -sS "$BASE/api/v1/http/artifacts?hostname=api.example.com&sort_by=requested_at&sort_order=DESC&limit=10" -H "$AUTH"

# Next page
curl -sS "$BASE/api/v1/http/artifacts?limit=10&cursor=$NEXT_CURSOR" -H "$AUTH"
```

Walking all pages with `jq`:

```sh
url="$BASE/api/v1/http/artifacts?limit=100"
while :; do
  page=$(curl -sS "$url" -H "$AUTH")
  echo "$page" | jq -c '.data[]'
  next=$(echo "$page" | jq -r '.meta.next_cursor // empty')
  [ -z "$next" ] && break
  url="$BASE/api/v1/http/artifacts?limit=100&cursor=$next"
done
```

---

## GET /api/v1/http/artifacts/{id}

Returns every column of one artifact.

### Path parameters

| Parameter | Type | Required | Description                                       |
| --------- | ---- | -------- | ------------------------------------------------- |
| `id`      | UUID | Yes      | Canonical form `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`, case-insensitive |

### Response

**`200 OK`**

| Field                      | Type            |
| -------------------------- | --------------- |
| `id`                       | string (UUID)   |
| `http_method`              | string          |
| `hostname`                 | string          |
| `endpoint_path`            | string          |
| `full_url`                 | string          |
| `requested_at`             | datetime        |
| `response_status_code`     | integer \| null |
| `response_content_type`    | string \| null  |
| `response_body_size_bytes` | integer \| null |
| `request_headers`          | object \| null  |
| `request_body`             | string \| null  |
| `response`                 | string \| null  |

```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "http_method": "POST",
  "hostname": "api.example.com",
  "endpoint_path": "/v1/resource",
  "full_url": "https://api.example.com/v1/resource",
  "requested_at": "2026-10-27T12:30:00Z",
  "response_status_code": 200,
  "response_content_type": "application/json",
  "response_body_size_bytes": 128,
  "request_headers": { "Content-Type": "application/json" },
  "request_body": "{\"id\":123,\"name\":\"test\"}",
  "response": "HTTP/1.1 200 OK\r\nContent-Length: 16\r\nContent-Type: application/json\r\nDate: Tue, 27 Oct 2026 12:30:00 GMT\r\n\r\n{\"success\":true}"
}
```

| Status | When                     |
| ------ | ------------------------ |
| 400    | Invalid UUID format      |
| 401    | Missing/invalid API key  |
| 404    | No artifact with this ID |
| 500    | Unexpected server error  |

```sh
curl -sS "$BASE/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000" -H "$AUTH"
```

---

## DELETE /api/v1/http/artifacts/{id}

Permanently deletes one artifact. There is no soft delete and no API to
restore it. The row is removed from `http_artifacts` and disappears from
listings, search and domain counts immediately.

### Path parameters

| Parameter | Type | Required | Description                                       |
| --------- | ---- | -------- | ------------------------------------------------- |
| `id`      | UUID | Yes      | Canonical form `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`, case-insensitive |

### Responses

| Status | Body          | When                                     |
| ------ | ------------- | ---------------------------------------- |
| 204    | (empty)       | Artifact deleted                         |
| 400    | error envelope | Invalid UUID format (`validation_error`) |
| 401    | error envelope | Missing or invalid API key              |
| 404    | error envelope | No artifact with this ID, including one already deleted |
| 500    | error envelope | Unexpected server error                 |

```sh
curl -sS -X DELETE "$BASE/api/v1/http/artifacts/550e8400-e29b-41d4-a716-446655440000" -H "$AUTH" -w '%{http_code}\n'
# 204
```

```sql
DELETE FROM http_artifacts WHERE id = $1;  -- 0 rows affected → 404
```

---

## GET /api/v1/http/artifacts/search

Case-insensitive substring search across **every** column: `id`,
`http_method`, `hostname`, `endpoint_path`, `full_url`, `requested_at`,
`response_status_code`, `response_content_type`, `response_body_size_bytes`,
`request_headers` (keys and values), `request_body` and `response` (the raw
response, so response header names and values are searchable too).
Use it to find leaked secrets such as tokens, keys or passwords.

### Query parameters

| Parameter | Type    | Required | Description                                                                  |
| --------- | ------- | -------- | ---------------------------------------------------------------------------- |
| `q`       | string  | Yes      | Search keyword, 1–1024 bytes of UTF-8, not blank, no control characters. Matched literally: `%`, `_` and `\` are not wildcards |
| `limit`   | integer | No       | `1`–`100`, default `30`                                                      |

- Results are ordered newest first (`requested_at DESC`).
- `q` is matched exactly as given. Leading and trailing spaces are kept but
  a whitespace-only query is rejected.
- `requested_at` is matched in UTC with exactly six fractional digits, e.g.
  `2026-10-27T12:30:00.000000Z` (JSON responses trim trailing zeros, so
  `2026-10-27T12:30:00Z` as shown in a response does not match). Prefixes
  work: `q=2026-10-27T12` matches everything requested during that hour.
- Results contain metadata only. Fetch bodies and headers with
  [GET /api/v1/http/artifacts/{id}](#get-apiv1httpartifactsid).

### Response

**`200 OK`**

```json
{
  "data": [
    {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "http_method": "POST",
      "hostname": "api.example.com",
      "endpoint_path": "/v1/resource",
      "full_url": "https://api.example.com/v1/resource",
      "requested_at": "2026-10-27T12:30:00Z",
      "response_status_code": 200,
      "response_content_type": "application/json",
      "response_body_size_bytes": 128
    }
  ],
  "meta": { "query": "secret", "limit": 30 }
}
```

Errors: `400` (missing/blank/invalid `q`, invalid `limit`), `401`, `500`.

```sh
curl -sS "$BASE/api/v1/http/artifacts/search?q=secret" -H "$AUTH"
curl -sS -G "$BASE/api/v1/http/artifacts/search" --data-urlencode 'q=Authorization: Bearer' -d limit=10 -H "$AUTH"
```

---

## GET /api/v1/http/domains

Lists every unique hostname with its artifact count and latest request time.

### Query parameters

| Parameter    | Default         | Description                                                   |
| ------------ | --------------- | ------------------------------------------------------------- |
| `sort_by`    | `latest_update` | `total_artifacts` (by `artifact_count`) or `latest_update` (by `last_requested_at`) |
| `sort_order` | `DESC`          | `ASC` or `DESC` (case-insensitive)                            |
| `limit`      | `100`           | Integer from `1` to `1000`                                    |

Ties are ordered by `hostname ASC`.

### SQL

```sql
SELECT hostname,
       COUNT(*)          AS artifact_count,
       MAX(requested_at) AS last_requested_at
FROM http_artifacts
GROUP BY hostname
ORDER BY last_requested_at DESC, hostname ASC   -- or artifact_count, per sort_by/sort_order
LIMIT $1;
```

### Response

**`200 OK`**

```json
{
  "data": [
    { "hostname": "api.example.com", "artifact_count": 42, "last_requested_at": "2026-10-27T14:01:43.760757Z" },
    { "hostname": "httpbin.org", "artifact_count": 3, "last_requested_at": "2026-10-27T14:01:42.629027Z" }
  ],
  "meta": { "sort_by": "latest_update", "sort_order": "DESC", "limit": 100 }
}
```

Errors: `400` (invalid `sort_by`, `sort_order` or `limit`), `401`, `500`.

```sh
curl -sS "$BASE/api/v1/http/domains" -H "$AUTH"
curl -sS "$BASE/api/v1/http/domains?sort_by=total_artifacts&sort_order=DESC" -H "$AUTH"
```

---

## Recon endpoints

The next four endpoints read the `domains` and `dns_records` tables. Those
tables are created and filled by `reconvault-dns-autoscan`; the API only
reads them. If the tables don't exist yet, the endpoints behave as if they
are empty.

Hostname and root-domain path parameters are trimmed and lower-cased. They
must be 1–255 bytes of valid UTF-8 with no control characters;
otherwise the response is `400 validation_error`. Encode them with
`encodeURIComponent` (or equivalent) in clients.

## GET /api/v1/domains/{hostname}

Returns the `domains` row for one hostname.

**`200 OK`**

```json
{
  "hostname": "www.example.com",
  "root_domain": "example.com",
  "asn": 13335,
  "asn_name": "CLOUDFLARENET",
  "waf": true,
  "updated_at": "2026-10-27T12:30:00Z"
}
```

`asn`, `asn_name` and `waf` are `null` while unknown. A `null` `waf` means
WAF detection has no result, which is not the same as `false`.

Errors: `400` (invalid hostname), `401`, `404` (`domain not found`: the
hostname has not been scanned), `500`.

```sh
curl -sS "$BASE/api/v1/domains/www.example.com" -H "$AUTH"
```

## GET /api/v1/dns_records/{hostname}

Returns the hostname's complete DNS history. Every stored record version is
included with no limit or pagination, sorted newest first by
`updated_at DESC, record_type, record_value`. An unknown hostname returns an
empty list rather than `404`.

**`200 OK`**

```json
{
  "data": [
    { "hostname": "www.example.com", "record_type": "A", "record_value": "192.0.2.10", "ttl": 300, "updated_at": "2026-10-27T12:30:00Z" },
    { "hostname": "www.example.com", "record_type": "A", "record_value": "192.0.2.9", "ttl": null, "updated_at": "2026-10-20T08:00:00Z" }
  ],
  "meta": { "hostname": "www.example.com", "total": 2 }
}
```

Errors: `400`, `401`, `500`.

```sh
curl -sS "$BASE/api/v1/dns_records/www.example.com" -H "$AUTH"
```

## GET /api/v1/subdomains/{root_domain}

Returns the distinct hostnames whose `root_domain` matches, sorted by
`hostname ASC`, with no pagination. `meta.total` always equals the number of
items in `data`.

**`200 OK`**

```json
{
  "data": [{ "hostname": "api.example.com" }, { "hostname": "www.example.com" }],
  "meta": { "root_domain": "example.com", "total": 2 }
}
```

Errors: `400`, `401`, `500`.

```sh
curl -sS "$BASE/api/v1/subdomains/example.com" -H "$AUTH"
```

## GET /api/v1/root_domains

Returns root domains with their number of unique hostnames, sorted by
`hostname_count DESC, root_domain ASC`. The dashboard home page uses this
endpoint.

| Parameter | Default | Description                |
| --------- | ------- | -------------------------- |
| `limit`   | `100`   | Integer from `1` to `1000` |

**`200 OK`**

```json
{
  "data": [{ "root_domain": "example.com", "hostname_count": 2 }],
  "meta": { "limit": 100 }
}
```

Errors: `400` (invalid `limit`), `401`, `500`.

```sh
curl -sS "$BASE/api/v1/root_domains?limit=20" -H "$AUTH"
```

---

## Health checks

Unauthenticated, intended for orchestrators and load balancers.

| Endpoint       | Purpose                                | Success               | Failure                   | Reachable via entrypoint |
| -------------- | -------------------------------------- | --------------------- | ------------------------- | ------------------------ |
| `GET /readyz`  | Readiness: the database is reachable   | `200 {"status":"ok"}` | `503 service_unavailable` | Yes                      |
| `GET /healthz` | Liveness: the process is serving HTTP  | `200 {"status":"ok"}` | (none)                    | No (container network only) |

The API image's health check runs `/reconvault-api healthcheck`, which
probes `/readyz` on the container's own port.

```sh
curl -sS "$BASE/readyz"
```

---

## OpenAPI document

`GET /swagger.json` returns the OpenAPI 3.0 document (no authentication; it
contains no data). Its `servers` entry is `/`, so tools that load it from the
entrypoint call the API on the same host.

```sh
curl -sS "$BASE/swagger.json" -o swagger.json
```
