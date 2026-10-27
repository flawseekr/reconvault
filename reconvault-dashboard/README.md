# reconvault-dashboard

A React + TypeScript dashboard for the ReconVault API: browse recorded HTTP
exchanges, drill into hosts and their DNS/ASN/WAF data, search every field for
leaked secrets, and inspect raw requests and responses.

Its production image (nginx) is also the stack's **single public entrypoint**:
it serves the dashboard at `/` and proxies the API at `/api/`, `/readyz` and
`/swagger.json` from the same host.

| Route | Page | API calls |
| ----- | ---- | --------- |
| `/` | Search bar, "Request a URL" bar, latest 50 artifacts, recently scanned hosts, Root Domains Overview | `GET /api/v1/http/artifacts?limit=50`, `GET /api/v1/http/domains`, `GET /api/v1/root_domains`, `POST /api/v1/http/request` |
| `/search?q=…` | Full-field search | `GET /api/v1/http/artifacts/search` |
| `/host/:hostname` | Domain info (root domain, WAF, ASN), full DNS history, then the host's artifacts with paging | `GET /api/v1/domains/{hostname}`, `GET /api/v1/dns_records/{hostname}`, `GET /api/v1/http/artifacts?hostname=…`, `GET /api/v1/http/domains` |
| `/subdomain/:root_domain` | Unique hostnames under a root domain, each linking to its host page | `GET /api/v1/subdomains/{root_domain}` |
| `/http/:id` | One artifact: overview, request, parsed response, raw response | `GET /api/v1/http/artifacts/{id}` |

Every page also polls `GET /readyz` every 30 s for the status indicator. All
routes support direct navigation and refresh.

## Stack

- **Core:** React 19 with TypeScript (strict), built with Vite 7.
- **UI:** Tailwind CSS 4, shadcn/ui components on Radix primitives, Lucide icons.
- **Routing and data:** React Router 7, TanStack Query 5 for server state,
  TanStack Table 8 for sortable tables.
- **Forms:** React Hook Form with Zod.
- **Testing:** Vitest and Testing Library.

## Running it

The dashboard is part of the root `docker-compose.yaml`:

```sh
cd ..                                   # repository root
docker compose up -d --build --wait
```

Open `http://<host>:8080/` (`APP_PORT`, default `8080`) and enter the
server's `API_KEY` (from `.env`) when asked.

## How it talks to the API

```
browser ──► nginx (this image, host APP_BIND:APP_PORT) ──/api/, /readyz, /swagger.json──► api:8080
              └─ serves the SPA build for every other path
```

- **Same origin.** All requests go to relative paths (`/api/v1/...`,
  `/readyz`) on the page's own origin, so the browser needs no CORS and the
  CSP allows `connect-src 'self'` only. In development, the Vite dev server
  proxies the same paths.
- **API key.**
  - The key is **never baked into the build or the image.**
  - Each user enters it once per tab. It's kept in `sessionStorage`, which
    the browser clears when the tab closes, and sent as a Bearer token.
  - A 401 clears it and asks again.
  - No `/api/v1` request is sent while no key is set.
- **Typed client.** `src/lib/api-client.ts` is the single place requests are
  made. It adds the Authorization header, applies timeouts (15 s; 90 s for
  `POST /api/v1/http/request`, which waits on the target; 5 s for `/readyz`)
  combined with TanStack Query's cancellation signal, and maps the API's
  error envelope to `ApiError`. Types in `src/types/api.ts` mirror
  `reconvault-api/api/swagger.json`.
- **Caching.** Data is considered fresh for 15 s by default (30 s for hosts
  and search, 60 s for DNS autoscan data) and refetched on window focus.
  Artifact details never change, so they're cached until reload. Recording a
  request invalidates artifact lists, hosts and search. Client errors (4xx)
  are not retried.

## Behavior that follows the API's capabilities

- **Sorting.** Home and Host pages sort **on the server** using `sort_by` and
  `sort_order`. The search endpoint has no sort parameters, so search results
  are sorted **in the browser** within the loaded results, and the page says
  so.
- **Pagination.** On the Host page, paging uses the API's keyset cursors
  (`meta.next_cursor`) with Previous and Next. The API exposes no total
  count, so the page shows "Page N · M rows" only.
  - Page size (25, 50 or 100), sort and cursor live in the URL; changing the
    sort, page size or host resets paging.
  - The trail of earlier cursors is stored in the history entry, so Previous
    survives refreshes. A shared deep link to a later page shows "Page —",
    and Previous returns to page 1.
- **Hosts.** `GET /api/v1/http/domains` returns up to 100 hosts, most recent
  first. Home shows 12 with a "Show all" toggle. The Host page shows a host's
  artifact count and last request time when the host is in that list.
- **Domain info and DNS history.** A hostname the DNS autoscan hasn't scanned
  yet shows "Not scanned yet". Missing WAF or ASN values show "Unknown". The
  DNS history is the complete, unpaginated list (newest first) and scrolls
  inside a fixed-height area.
- **Root domains.** Home lists up to 100 root domains (the API default),
  largest first, each linking to its subdomain page.
- **Raw responses.** `response` holds the status line, headers and body; the
  detail page splits it into those parts. Artifacts recorded before the API's
  raw-response migration hold only the body, and the page says headers are
  unavailable.
- **Bodies.**
  - JSON is pretty-printed and highlighted.
  - HTML and XML are shown as source text and **never rendered**.
  - Binary content (by media type, or by the U+FFFD replacement characters the
    API stores for invalid UTF-8) is hidden behind a notice.
  - Very large bodies show the first 100,000 characters with a "Show all"
    button.
  - Every block has a copy button.

## Configuration

| Variable | When | Default | Purpose |
| -------- | ---- | ------- | ------- |
| `API_UPSTREAM` | container runtime | `http://api:8080` | Where nginx proxies `/api/`, `/readyz` and `/swagger.json` |
| `APP_BIND` / `APP_PORT` | root compose `.env` | `0.0.0.0` / `8080` | Published address of the entrypoint |
| `DEV_API_PROXY_TARGET` | `npm run dev` | `http://127.0.0.1:8080` | Where the dev server proxies the same paths (default: the running compose entrypoint) |

See [`.env.example`](.env.example) for the development variable.

## Development

Node doesn't need to be installed on the host. With the compose stack
running, start the dev server with hot reload on <http://127.0.0.1:5173>:

```sh
docker run --rm -it --user "$(id -u):$(id -g)" -e HOME=/tmp --network host \
  -v "$PWD:/app" -w /app node:24-alpine sh -c "npm ci && npm run dev"
```

| Script | Does |
| ------ | ---- |
| `npm run dev` | Vite dev server |
| `npm run typecheck` | `tsc -b` (strict) |
| `npm run lint` | ESLint (typescript-eslint, react-hooks) |
| `npm test` | Vitest (unit, component and page tests) |
| `npm run build` | Typecheck + production build to `dist/` |
| `npm run check` | Typecheck, lint, tests and build, as CI would run them |

## Testing

```sh
# From the repository root: `npm ci && npm run check` in a clean container
docker compose -f docker-compose.test.yaml run --rm dashboard-tests
```

The source is mounted read-only and copied inside the container, so the run
never writes into the working tree. The tests cover:

- **Unit:** formatters; the raw-response parser, including legacy records;
  body classification and the JSON tokenizer; input validation mirroring the
  API's rules; URL-encoding route builders; the clipboard fallback; the API
  client (auth header, error envelope, 401 handling, timeouts, cancellation,
  no `/api/v1` calls without a key).
- **Components:** the artifact table (fallbacks for missing values,
  `aria-sort`, row navigation, accessible link names, client sorting with
  nulls last).
- **Pages**, through the real router and providers with a mocked `fetch`:
  artifact detail (captured HTML never rendered, legacy and failed artifacts,
  invalid and unknown IDs); host filtering and cursor paging; domain info
  (Yes/No/Unknown, not scanned yet) and DNS history placed above the
  artifacts; the subdomain page (totals, encoded links, empty, error and
  invalid states); the Root Domains Overview below the existing Home
  sections; search URL state and validation; the API-key gate without
  request loops; the URL-submit flow; the 404 page.

## Production image

`Dockerfile` builds with `npm ci` and serves `dist/` from
`nginxinc/nginx-unprivileged` (uid 101, listening on `0.0.0.0:8080`). Compose
runs it with a read-only root filesystem, tmpfs mounts for nginx's runtime
files, all capabilities dropped, and `no-new-privileges`.

nginx config (`nginx/`):

- **Routing:** `/api/` (whole prefix, so unknown API paths get the API's JSON
  errors), `/readyz` and `/swagger.json` are proxied to the API; `/v1/*` (the
  old API prefix) returns `404`; every other path falls back to the SPA shell;
  hashed `/assets/` are cached immutably.
- **Limits:** request bodies up to 2 MiB; proxy timeouts of 120 s for the API
  (longer than the API's default `UPSTREAM_TIMEOUT`).
- **Security headers:** a strict CSP, `X-Frame-Options: DENY`, `nosniff`,
  `Referrer-Policy: no-referrer` and `Permissions-Policy`. The CSP allows
  `'unsafe-inline'` styles only because Radix dialogs inject a `<style>`
  element with runtime-computed values; scripts are limited to `'self'`.

## Known limitations

- **No total counts.** The API returns no totals for artifacts or search
  results, so the UI shows row counts per page only.
- **Search has no sorting or pagination.** Results are the newest `limit`
  (up to 100) matches, sorted client-side.
- **Hosts and root domains are not paginated.** Only the first 100 (the API
  default) are shown.
- **Not byte-exact.** Response headers appear as the API reconstructs them:
  canonical names, sorted.
- **No dashboard login.** Anyone who can reach the entrypoint and knows the
  API key has full access. Put TLS (and, if needed, an authenticating proxy)
  in front for anything beyond a trusted network.
