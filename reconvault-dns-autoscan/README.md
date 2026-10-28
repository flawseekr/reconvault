# reconvault-dns-autoscan

A background Go service that enriches the hostnames recorded by the
ReconVault API. For every hostname it finds in `http_artifacts`, it:

- records the **root domain, ASN and WAF status** in `domains`
- discovers **DNS records** for the hostname and its root domain, and keeps
  a **history** of every change in `dns_records`

It runs alongside the API in `docker-compose.yaml` and needs no manual
operation.

## Scheduling

| Mode | When | Does |
| ---- | ---- | ---- |
| **Active** | At startup, then every `ACTIVE_INTERVAL` (1 min) | (1) Hostnames in `http_artifacts` without a `domains` row are domain-scanned. (2) Hostnames and root domains in `domains` that were never DNS-scanned get a DNS scan. |
| **Daily** | Every day at **00:00 UTC** | Re-scans the DNS records of every scanned target, inserting only record sets that changed. Then re-scans the root domain, ASN and WAF status of every domain, so a host that moves to another network or adds or drops a WAF is updated within a day. |

- **Missed runs:** if the service was down at midnight, the daily run happens
  at the next start. The last completion is stored in `autoscan_jobs`.
- **Several replicas:** each job takes a PostgreSQL advisory lock, so running
  several replicas never duplicates work.
- **Shutdown:** on SIGTERM, in-flight scans stop and partial results are not
  stored.

"Never DNS-scanned" means no outcome is recorded in `dns_scan_state` yet.
Checking for missing `dns_records` rows instead would rescan names that
have no records (for example NXDOMAIN) on every pass, forever.

## Domain scan → `domains`

| Column | Source | `NULL` when |
| ------ | ------ | ----------- |
| `hostname` | `http_artifacts.hostname` (lower-cased) | never |
| `root_domain` | Public Suffix List | never (see below) |
| `asn`, `asn_name` | Team Cymru IP-to-ASN | not determinable reliably |
| `waf` | provider IP ranges, CNAMEs, passive HTTP fingerprints | inconclusive |

### `root_domain`

The registrable domain from the **ICANN section** of the Public Suffix List
(`golang.org/x/net/publicsuffix`). Multi-level public suffixes are handled:

| Hostname | `root_domain` |
| -------- | ------------- |
| `a.b.example.co.uk` | `example.co.uk` |
| `api.service.gov.uk` | `service.gov.uk` |
| `user.github.io` | `github.io` |
| `a.b.invalid` | `b.invalid` |
| `192.0.2.1`, `localhost` | unchanged |

- **Private PSL entries** such as `github.io` aren't treated as suffixes.
  This matches the common definition of a root domain (it's tldextract's
  default).
- **Unlisted TLDs** follow the PSL's implicit `*` rule.

### `asn` and `asn_name`

These come from [Team Cymru's IP-to-ASN mapping](https://www.team-cymru.com/ip-asn-mapping),
queried over DNS. It's free, needs no API key, and is widely used by
recon tools.

1. The hostname's addresses are resolved (up to 4, IPv4 and IPv6).
2. For each address, `origin.asn.cymru.com` returns the ASN announcing it.
   When several prefixes cover the address, the **most specific** one decides.
3. `AS<n>.asn.cymru.com` returns that ASN's registered name. The record must
   repeat the same number, so `asn` and `asn_name` always describe the same
   ASN. The trailing country code is removed: `CLOUDFLARENET, US` becomes
   `CLOUDFLARENET`.

Both stay `NULL` when:
- the address isn't announced
- the prefix has several origin ASNs (MOAS)
- the hostname's addresses map to different ASNs
- the name record is missing
- lookups fail

A later rescan that can't determine the ASN keeps the previously verified
value instead of erasing it.

### `waf`

| Value | Meaning |
| ----- | ------- |
| `true` | Evidence of a WAF was found (see below) |
| `false` | The host answered over HTTP(S) and no WAF signal was found |
| `NULL` | Inconclusive: the host couldn't be reached and no other evidence exists |

Evidence, in order:

1. **Provider IP ranges:** an address falls inside a WAF provider's
   published range (Cloudflare, Imperva Incapsula, ArvanCloud), using
   ProjectDiscovery's [cdncheck](https://github.com/projectdiscovery/cdncheck)
   dataset.
2. **CNAME targets:** names that only exist behind a WAF product
   (`*.incapdns.net`, `*.impervadns.net`, `*.cdn.cloudflare.net`, `*.sucuri.net`).
3. **Passive HTTP fingerprints,** modelled on [wafw00f](https://github.com/EnableSecurity/wafw00f)'s
   passive checks: response headers and cookies that WAF products add to
   every proxied response. Examples are `CF-RAY`, `X-Sucuri-ID`, Incapsula
   `incap_ses_*`/`visid_incap_*` cookies, `X-Amzn-Waf-Action`, F5 `TS01*`,
   Barracuda, FortiWeb and Reblaze cookies, and ModSecurity and Wallarm
   server headers.

What counts as evidence is deliberately strict:

- **A CDN is not evidence of a WAF.** CloudFront, Fastly and Akamai edge
  hostnames, `Server: AkamaiGHost` and `X-Azure-Ref` appear on every
  response from those CDNs, WAF or not, so they're ignored. cdncheck's
  CNAME check is not used for the same reason: it labels every CDN as a
  WAF.
- **The service sends no attack payloads.** It doesn't trigger WAF block
  pages the way wafw00f's active mode does. Doing that automatically to
  every recorded host would be intrusive and gets the scanner's IP banned.
- **The HTTP probe** is a single `GET /` over HTTPS, then HTTP. Redirects
  aren't followed and TLS certificate errors are ignored (the project
  policy). The same address policy as the API applies (see Security).

## DNS scan → `dns_records`

Record types queried for every target: `A`, `AAAA`, `CNAME`, `MX`, `NS`,
`TXT`, `SOA`, `CAA` and `HTTPS`. IP-literal hostnames get a `PTR` lookup of
their reverse name instead.

### Accurate answers and TTLs

Records are queried **from the zone's authoritative nameservers** (recursion
disabled). They return the TTL configured in the zone; a recursive resolver
would return the time remaining in its cache.

1. **Find the zone:** an SOA lookup through the recursive resolvers. Its
   owner, from the answer or authority section, is the zone apex.
2. **Find the servers:** NS lookups, then A/AAAA lookups of up to 4
   nameservers. Zones and servers are cached for an hour.
3. **Query them directly,** with EDNS0, retries across servers, TCP when a
   UDP answer is truncated, and **TCP when a UDP answer lacks the AA flag**.
   Some networks transparently intercept UDP port 53 and answer from a
   cache, and TCP is rarely intercepted.
4. **Follow CNAMEs across zones,** up to 8 hops.
5. **Fall back** to the recursive resolvers (`DNS_RESOLVERS`) when the
   authoritative servers are unreachable, refuse, or are lame. TTLs are
   then cache values.

### Record values

| Type | `record_value` |
| ---- | -------------- |
| `A`, `AAAA` | Canonical IP (`2001:db8::1`) |
| `CNAME`, `NS`, `PTR` | Lower-cased name, no trailing dot |
| `MX` | `<priority> <exchange>`: `10 mail.example.com`; null MX: `0 .` |
| `TXT` | The character-strings joined, in escaped presentation form |
| `SOA` | `<mname> <rname> <serial> <refresh> <retry> <expire> <minimum>` |
| `CAA` | `<flags> <tag> "<value>"`: `0 issue "letsencrypt.org"` |
| `HTTPS` | Presentation form: `1 . alpn="h3,h2" ipv4hint=...` |

Each type has these properties:

- Multiple values of one type are separate rows. Duplicate values within a
  response are merged, keeping the lowest TTL.
- Records reached through a CNAME are stored under the queried hostname,
  as a resolving client sees them. The `CNAME` row records the alias itself.

### History without duplicates

`dns_records` is **append-only**. Rows are never updated or deleted.

- A **(hostname, type) record set** is one *version*: rows that share one
  `updated_at`.
- A scan inserts a new version **only when the set of values changed**.
  A TTL-only change doesn't create a version.

Every lookup's outcome is stored separately, in `dns_scan_state`:

| `status` | Meaning | Effect on the current record set |
| -------- | ------- | -------------------------------- |
| `found` | Records returned | New version if the set changed |
| `nodata` | Name exists, no records of this type | Cleared (`current_version = NULL`); history kept |
| `nxdomain` | Name doesn't exist | Cleared; history kept |
| `error` | Timeout, SERVFAIL, REFUSED… | **Unchanged**: a failure never removes valid records |

Other columns:

- `current_version`: the version that is currently valid.
- `last_success_at`: the latest conclusive answer.
- `last_attempt_at`, `last_error`, `consecutive_failures`: track problems.

```sql
-- Currently valid records
SELECT * FROM dns_records_current WHERE hostname = 'example.com';

-- Full history of a record set, newest first
SELECT updated_at, record_value, ttl FROM dns_records
WHERE hostname = 'example.com' AND record_type = 'MX'
ORDER BY updated_at DESC, record_value;

-- Lookups that keep failing
SELECT hostname, record_type, consecutive_failures, last_error
FROM dns_scan_state WHERE status = 'error' ORDER BY consecutive_failures DESC;
```

## Database

The service owns `domains`, `dns_records`, `dns_scan_state` and
`autoscan_jobs`, plus the view `dns_records_current`. Its migrations are
embedded and run at startup:

- They're tracked in `autoscan_schema_migrations`, separately from the API's
  `schema_migrations`.
- An advisory lock serializes them.
- They run only after `http_artifacts` exists, which the API creates.

`domains` and `dns_records` follow the specified schema exactly. The added
indexes are:

| Index | Purpose |
| ----- | ------- |
| `idx_domains_root_domain` | Root-domain target listing |
| `idx_dns_records_version (hostname, record_type, updated_at DESC)` | Version lookups and history |
| `uq_dns_records_version_value` (unique, on `md5(record_value)`) | No duplicate value within a version. The value is hashed because TXT values can exceed the B-tree entry size limit |

## Configuration

| Variable | Compose `.env` name | Default | Range | Description |
| -------- | ------------------- | ------- | ----- | ----------- |
| `DATABASE_URL` | (built from `POSTGRES_*`) | **required** | | PostgreSQL connection string |
| `DB_MAX_CONNS` | `AUTOSCAN_DB_MAX_CONNS` | `10` | 1–1000 | Pool size |
| `DNS_RESOLVERS` | `DNS_RESOLVERS` | `1.1.1.1:53,8.8.8.8:53,9.9.9.9:53` | | Recursive resolvers (`ip[:port]`, comma-separated) |
| `DNS_TIMEOUT` | `DNS_TIMEOUT` | `5s` | 100ms–1m | Per-query timeout |
| `DNS_RETRIES` | `DNS_RETRIES` | `2` | 0–10 | Extra rounds over the server list |
| `ACTIVE_INTERVAL` | `AUTOSCAN_ACTIVE_INTERVAL` | `1m` | 1s–24h | Interval between the starts of active passes; a pass that runs longer is followed immediately by the next |
| `BATCH_SIZE` | `AUTOSCAN_BATCH_SIZE` | `100` | 1–10000 | Hostnames per database batch |
| `SCAN_CONCURRENCY` | `AUTOSCAN_CONCURRENCY` | `16` | 1–512 | Hostnames scanned in parallel |
| `HTTP_PROBE_TIMEOUT` | `AUTOSCAN_HTTP_PROBE_TIMEOUT` | `10s` | 1s–2m | WAF fingerprinting request timeout |
| `ALLOW_PRIVATE_NETWORKS` | `ALLOW_PRIVATE_NETWORKS` (shared with the API) | `false` | | `true` allows scanning loopback/private targets |
| `LOG_LEVEL` | `LOG_LEVEL` (shared with the API) | `info` | | `debug` also logs per-host WAF evidence and DNS changes |
| `SHUTDOWN_TIMEOUT` | `AUTOSCAN_SHUTDOWN_TIMEOUT` | `30s` | 1s–10m | How long to wait for cancelled scans to stop on SIGTERM |
| `HEALTH_ADDR` | (not set by Compose) | `:8081` | | Liveness and readiness endpoint, used by the container health check |

Invalid values stop the service at startup with a message listing every
problem. See the root `.env.example`.

## Security

- **SSRF guard:** connections to loopback, private, link-local, CGNAT and
  reserved ranges are refused. This covers HTTP probes and authoritative
  nameserver addresses, so a hostile zone can't point the scanner at
  internal hosts. The check runs on the resolved address of every
  connection. The configured recursive resolvers are trusted.
- **Container:** a distroless non-root image with a read-only root
  filesystem and all capabilities dropped. It publishes no ports and reaches
  only the database network and the outbound network.

## Operations

```sh
docker compose logs -f dns-autoscan      # JSON logs
docker compose exec db psql -U reconvault -d reconvault \
  -c "SELECT status, count(*) FROM dns_scan_state GROUP BY 1"
```

The Docker health check calls `/readyz` (database reachable) through
`reconvault-dns-autoscan healthcheck`.

## Testing

```sh
# From the repository root: gofmt, vet, unit, DNS and PostgreSQL integration tests
docker compose -f docker-compose.test.yaml run --rm dns-autoscan-tests
docker compose -f docker-compose.test.yaml down -v
```

The DNS tests need no internet: `internal/testdns` runs an in-process DNS
server that acts as both an authoritative server (exact TTLs) and a
recursive server (cache-reduced TTLs). It can also inject SERVFAIL, lame
zones, truncation and UDP interception.

The 46 tests cover:

- authoritative TTLs and cross-zone CNAMEs
- NXDOMAIN, NODATA and failure classification
- the blocked-nameserver policy, plus the TCP fallbacks for truncated and
  intercepted answers
- value normalization for every type
- root domains, Cymru parsing (including MOAS and name/number mismatch),
  and WAF signals and inconclusive results
- the full version lifecycle: new, unchanged, changed, failed, absent and
  reappearing sets
- job locks and the schedule
- an end-to-end active pass and daily refresh over real PostgreSQL

## Known limitations

- **TTL accuracy depends on reaching the nameservers.** TTLs are exact only
  when the authoritative servers are reachable over UDP or TCP from the
  deployment network. Otherwise the recursive fallback stores cache TTLs.
- **WAF detection is passive.** WAFs that add no headers or cookies to
  normal responses and use no listed IP range or CNAME are reported as
  `false`, which means "no WAF detected".
- **No SRV records.** `SRV` is not among the queried types, and
  `_service._proto` names aren't enumerated.
- **The ASN is the hosting network.** It reflects the network serving the
  addresses, as seen from this resolver. CDNs answer differently by region.
