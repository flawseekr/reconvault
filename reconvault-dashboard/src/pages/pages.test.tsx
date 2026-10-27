import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { artifact, dnsRecord, domain, domainInfo, listMeta, summary } from "@/test/fixtures";
import { json, mockFetch, renderRoute } from "@/test/utils";

const ID = "550e8400-e29b-41d4-a716-446655440000";
const notFound = () => json({ error: { code: "not_found", message: "artifact not found" } }, 404);

describe("ArtifactDetailPage", () => {
  it("shows overview, parsed response headers and both bodies", async () => {
    mockFetch((url) => (url.pathname === `/api/v1/http/artifacts/${ID}` ? json(artifact()) : json({ status: "ok" })));
    renderRoute(`/http/${ID}`);

    expect(await screen.findByRole("heading", { name: /https:\/\/api\.example\.com\/v1\/resource/ })).toBeInTheDocument();
    const overview = screen.getByRole("region", { name: "Overview" });
    expect(within(overview).getByRole("link", { name: "api.example.com" })).toHaveAttribute("href", "/host/api.example.com");
    const breadcrumb = screen.getByRole("navigation", { name: "Breadcrumb" });
    expect(within(breadcrumb).getByRole("link", { name: "api.example.com" })).toHaveAttribute("href", "/host/api.example.com");
    expect(screen.getByText("HTTP/1.1 200 OK")).toBeInTheDocument();
    expect(screen.getByText("X-Token")).toBeInTheDocument();
    expect(screen.getByText("hdr_secret")).toBeInTheDocument();
    expect(screen.getByText('"success"')).toBeInTheDocument(); // highlighted JSON key

    await userEvent.setup().click(screen.getByRole("tab", { name: "Request" }));
    expect(screen.getByText("User-Agent")).toBeInTheDocument();
    expect(screen.getByText('"id"')).toBeInTheDocument();
  });

  it("renders captured HTML as text, never as markup", async () => {
    const html = '<img src=x onerror="window.__pwned=1"><script>window.__pwned=1</script><b>bold</b>';
    mockFetch(() => json(artifact({ response_content_type: "text/html", response: `HTTP/1.1 200 OK\r\nContent-Type: text/html\r\n\r\n${html}` })));
    const { container } = renderRoute(`/http/${ID}`);
    expect(await screen.findByText(html)).toBeInTheDocument();
    expect(container.querySelector("img, script, b")).toBeNull();
    expect((window as { __pwned?: number }).__pwned).toBeUndefined();
  });

  it("explains legacy body-only records and failed requests", async () => {
    mockFetch(() => json(artifact({ response: '{"legacy":true}' })));
    renderRoute(`/http/${ID}`);
    expect(await screen.findByText(/recorded before headers were stored/)).toBeInTheDocument();
    expect(screen.getByText('"legacy"')).toBeInTheDocument();
  });

  it("handles a failed request with no response", async () => {
    mockFetch(() => json(artifact({ response: null, response_status_code: null, response_content_type: null, response_body_size_bytes: null })));
    renderRoute(`/http/${ID}`);
    expect(await screen.findByText(/request failed before a response arrived/)).toBeInTheDocument();
  });

  it("rejects invalid IDs without calling the API", async () => {
    const fetch = mockFetch(() => json({ status: "ok" }));
    renderRoute("/http/not-a-uuid");
    expect(await screen.findByText("This is not a valid artifact ID")).toBeInTheDocument();
    expect(fetch.mock.calls.some(([u]) => String(u).includes("/api/v1/http/artifacts"))).toBe(false);
  });

  it("shows a not-found state for unknown IDs", async () => {
    mockFetch((url) => (url.pathname.startsWith("/api/v1/") ? notFound() : json({ status: "ok" })));
    renderRoute(`/http/${ID}`);
    expect(await screen.findByText("No artifact with this ID")).toBeInTheDocument();
  });
});

describe("HostPage", () => {
  it("filters by hostname and pages with cursors", async () => {
    const user = userEvent.setup();
    const seen: URLSearchParams[] = [];
    mockFetch((url) => {
      if (url.pathname === "/api/v1/http/domains") return json({ data: [domain({ artifact_count: 3 })], meta: {} });
      if (url.pathname !== "/api/v1/http/artifacts") return json({ status: "ok" });
      seen.push(url.searchParams);
      const cursor = url.searchParams.get("cursor");
      return cursor === "c2"
        ? json({ data: [summary({ id: "650e8400-e29b-41d4-a716-446655440000", full_url: "https://api.example.com/page2" })], meta: { ...listMeta, next_cursor: null } })
        : json({ data: [summary()], meta: { ...listMeta, next_cursor: "c2" } });
    });
    const { router } = renderRoute("/host/API.Example.com");

    expect(await screen.findByRole("heading", { name: "api.example.com" })).toBeInTheDocument();
    expect(seen[0]?.get("hostname")).toBe("api.example.com");
    expect(seen[0]?.get("limit")).toBe("25");
    expect(await screen.findByText(/3 artifacts/)).toBeInTheDocument();
    expect(screen.getByText(/Page 1/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Next page" }));
    expect(await screen.findByText("https://api.example.com/page2")).toBeInTheDocument();
    expect(router.state.location.search).toBe("?cursor=c2");
    expect(screen.getByText(/Page 2/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Previous page" }));
    await waitFor(() => expect(router.state.location.search).toBe(""));
    expect(await screen.findByText(/Page 1/)).toBeInTheDocument();
  });

  it("sends sort parameters from the URL", async () => {
    const seen: URLSearchParams[] = [];
    mockFetch((url) => {
      if (url.pathname === "/api/v1/http/artifacts") seen.push(url.searchParams);
      return url.pathname === "/api/v1/http/artifacts" ? json({ data: [], meta: listMeta }) : json({ data: [], meta: {} });
    });
    renderRoute("/host/a.com?sort_by=response_status_code&sort_order=ASC&limit=100");
    expect(await screen.findByText("No artifacts for this host")).toBeInTheDocument();
    expect(seen[0]?.get("sort_by")).toBe("response_status_code");
    expect(seen[0]?.get("sort_order")).toBe("ASC");
    expect(seen[0]?.get("limit")).toBe("100");
  });
});

describe("SearchPage", () => {
  it("searches the API with the query from the URL", async () => {
    let query: URLSearchParams | undefined;
    mockFetch((url) => {
      if (url.pathname === "/api/v1/http/artifacts/search") {
        query = url.searchParams;
        return json({ data: [{ ...summary(), hostname: "api.example.com", endpoint_path: "/v1/resource" }], meta: { query: "secret", limit: 30 } });
      }
      return json({ status: "ok" });
    });
    renderRoute("/search?q=secret");
    expect(await screen.findByText(/1 match for/)).toBeInTheDocument();
    expect(query?.get("q")).toBe("secret");
    expect(query?.get("limit")).toBe("30");
    expect(screen.getByRole("searchbox")).toHaveValue("secret");
  });

  it("validates input before requesting and updates the URL on submit", async () => {
    const user = userEvent.setup();
    const fetch = mockFetch((url) =>
      url.pathname === "/api/v1/http/artifacts/search" ? json({ data: [], meta: { query: "tok", limit: 30 } }) : json({ status: "ok" }),
    );
    const { router } = renderRoute("/search");
    const box = await screen.findByRole("searchbox");

    await user.type(box, "   ");
    await user.click(screen.getByRole("button", { name: "Search" }));
    expect(screen.getByText("Enter a search term.")).toBeInTheDocument();
    expect(fetch.mock.calls.some(([u]) => String(u).includes("/search"))).toBe(false);

    await user.clear(box);
    await user.type(box, "tok");
    await user.keyboard("{Enter}");
    await waitFor(() => expect(router.state.location.search).toBe("?q=tok"));
    expect(await screen.findByText("No matches")).toBeInTheDocument();
  });
});

describe("HomePage", () => {
  it("lists artifacts and hosts, and records a URL", async () => {
    const user = userEvent.setup();
    let posted: unknown;
    mockFetch((url, init) => {
      if (url.pathname === "/api/v1/http/artifacts") {
        expect(url.searchParams.get("limit")).toBe("50");
        return json({ data: [summary()], meta: listMeta });
      }
      if (url.pathname === "/api/v1/http/domains") return json({ data: [domain()], meta: {} });
      if (url.pathname === "/api/v1/root_domains") return json({ data: [], meta: { limit: 100 } });
      if (url.pathname === "/api/v1/http/request") {
        posted = JSON.parse(String(init.body));
        return json({ ...artifact(), id: "750e8400-e29b-41d4-a716-446655440000", response_body_truncated: false }, 201);
      }
      return json({ status: "ok" });
    });
    renderRoute("/");

    expect(await screen.findByRole("link", { name: /GET https:\/\/api\.example\.com\/v1\/resource/ })).toBeInTheDocument();
    const hosts = await screen.findByRole("region", { name: "Recently scanned hosts" });
    expect(within(hosts).getByRole("link", { name: /api\.example\.com/ })).toHaveAttribute("href", "/host/api.example.com");

    await user.type(screen.getByLabelText("URL to request"), "not a url");
    await user.click(screen.getByRole("button", { name: /send/i }));
    expect(screen.getByText("Enter an absolute http:// or https:// URL.")).toBeInTheDocument();
    expect(posted).toBeUndefined();

    await user.clear(screen.getByLabelText("URL to request"));
    await user.type(screen.getByLabelText("URL to request"), "https://example.com");
    await user.click(screen.getByRole("button", { name: /send/i }));
    expect(await screen.findByRole("link", { name: /view artifact/i })).toHaveAttribute("href", "/http/750e8400-e29b-41d4-a716-446655440000");
    expect(posted).toEqual({ method: "GET", url: "https://example.com" });
  });

  it("asks for an API key when none is stored, without calling the API", async () => {
    const fetch = mockFetch((url) =>
      url.pathname.startsWith("/api/v1/") ? json({ error: { code: "unauthorized", message: "missing or invalid API key" } }, 401) : json({ status: "ok" }),
    );
    renderRoute("/", { apiKey: null });
    expect(await screen.findByRole("dialog", { name: "Connect to ReconVault" })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 300));
    expect(fetch.mock.calls.filter(([u]) => String(u).includes("/api/v1/"))).toHaveLength(0);
  });

  it("stops after one 401 when the stored key is rejected", async () => {
    const fetch = mockFetch((url) =>
      url.pathname.startsWith("/api/v1/") ? json({ error: { code: "unauthorized", message: "missing or invalid API key" } }, 401) : json({ status: "ok" }),
    );
    renderRoute("/", { apiKey: "revoked-key-0123456789abcdef0123456789" });
    expect(await screen.findByRole("dialog", { name: "Connect to ReconVault" })).toBeInTheDocument();
    await new Promise((r) => setTimeout(r, 300));
    // One request per query (artifacts, domains, root domains) and no refetch loop.
    expect(fetch.mock.calls.filter(([u]) => String(u).includes("/api/v1/")).length).toBeLessThanOrEqual(3);
  });

  it("loads data once a key is entered", async () => {
    const user = userEvent.setup();
    mockFetch((url, init) => {
      const auth = (init.headers as Record<string, string> | undefined)?.Authorization;
      if (url.pathname === "/api/v1/http/artifacts" && auth) return json({ data: [summary()], meta: listMeta });
      if ((url.pathname === "/api/v1/http/domains" || url.pathname === "/api/v1/root_domains") && auth) return json({ data: [], meta: {} });
      return json({ status: "ok" });
    });
    renderRoute("/", { apiKey: null });
    await user.type(await screen.findByLabelText("API key"), "k".repeat(31));
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(screen.getByText("API keys are at least 32 characters long.")).toBeInTheDocument();
    await user.type(screen.getByLabelText("API key"), "k");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByRole("link", { name: /GET https:\/\/api\.example\.com/ })).toBeInTheDocument();
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("shows a 404 page for unknown routes", async () => {
    mockFetch(() => json({ status: "ok" }));
    renderRoute("/does/not/exist");
    expect(await screen.findByText("There is nothing at this address")).toBeInTheDocument();
  });
});

describe("HostPage recon sections", () => {
  const recon = (overrides: { info?: Response; dns?: Response } = {}) =>
    mockFetch((url) => {
      if (url.pathname === "/api/v1/domains/www.example.com") return overrides.info ?? json(domainInfo());
      if (url.pathname === "/api/v1/dns_records/www.example.com") {
        return (
          overrides.dns ??
          json({
            data: [dnsRecord({ record_value: "192.0.2.2", updated_at: "2026-10-27T12:30:00Z" }), dnsRecord({ ttl: null, updated_at: "2026-10-20T08:00:00Z" })],
            meta: { hostname: "www.example.com", total: 2 },
          })
        );
      }
      if (url.pathname === "/api/v1/http/artifacts") return json({ data: [], meta: listMeta });
      return json({ data: [], meta: {} });
    });

  it("shows domain info and the full DNS history above the artifacts", async () => {
    recon();
    renderRoute("/host/WWW.example.com");

    const info = await screen.findByRole("region", { name: "Domain info" });
    expect(await within(info).findByText("example.com")).toBeInTheDocument();
    expect(within(info).getByRole("link", { name: /More subdomains under this domain/ })).toHaveAttribute("href", "/subdomain/example.com");
    expect(within(info).getByText("WAF Present?")).toBeInTheDocument();
    expect(within(info).getByText("Yes")).toBeInTheDocument();
    expect(within(info).getByText("13335")).toBeInTheDocument();
    expect(within(info).getByText("CLOUDFLARENET")).toBeInTheDocument();

    const dns = screen.getByRole("region", { name: "DNS history" });
    const rows = await within(dns).findAllByRole("row");
    expect(rows).toHaveLength(3); // header + 2 records, server order kept
    expect(within(rows[1]!).getByText("192.0.2.2")).toBeInTheDocument();
    expect(within(rows[2]!).getByText("—")).toBeInTheDocument(); // null TTL
    expect(within(dns).getByText("2 records, newest first")).toBeInTheDocument();

    // Placement: Domain info, then DNS history, then the existing artifacts table.
    const artifacts = screen.getByRole("region", { name: "Artifacts" });
    expect(info.compareDocumentPosition(dns) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(dns.compareDocumentPosition(artifacts) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(await within(artifacts).findByText("No artifacts for this host")).toBeInTheDocument();
  });

  it("shows Unknown for missing WAF and ASN values", async () => {
    recon({ info: json(domainInfo({ waf: null, asn: null, asn_name: null })) });
    renderRoute("/host/www.example.com");
    const info = await screen.findByRole("region", { name: "Domain info" });
    expect(await within(info).findAllByText("Unknown")).toHaveLength(2);
    expect(within(info).getByText("No ASN name available")).toBeInTheDocument();
  });

  it("shows No when no WAF was detected", async () => {
    recon({ info: json(domainInfo({ waf: false })) });
    renderRoute("/host/www.example.com");
    const info = await screen.findByRole("region", { name: "Domain info" });
    expect(await within(info).findByText("No")).toBeInTheDocument();
  });

  it("handles unscanned hosts, empty histories and errors", async () => {
    recon({
      info: json({ error: { code: "not_found", message: "domain not found" } }, 404),
      dns: json({ data: [], meta: { hostname: "www.example.com", total: 0 } }),
    });
    renderRoute("/host/www.example.com");
    expect(await screen.findByText("Not scanned yet")).toBeInTheDocument();
    expect(await screen.findByText("No DNS records")).toBeInTheDocument();
  });

  it("shows retryable errors", async () => {
    const failure = () => json({ error: { code: "internal_error", message: "internal server error" } }, 500);
    mockFetch((url) =>
      url.pathname.startsWith("/api/v1/domains/") || url.pathname.startsWith("/api/v1/dns_records/")
        ? failure()
        : json({ data: [], meta: listMeta }),
    );
    renderRoute("/host/www.example.com");
    expect(await screen.findByText("Could not load domain info")).toBeInTheDocument();
    expect(await screen.findByText("Could not load DNS history")).toBeInTheDocument();
  });
});

describe("SubdomainPage", () => {
  it("lists unique hostnames with a total and links to each host", async () => {
    let requested = "";
    mockFetch((url) => {
      if (url.pathname.startsWith("/api/v1/subdomains/")) {
        requested = url.pathname;
        return json({
          data: [{ hostname: "api.example.com" }, { hostname: "we ird/host.example.com" }],
          meta: { root_domain: "example.com", total: 2 },
        });
      }
      return json({ status: "ok" });
    });
    renderRoute("/subdomain/Example.COM");

    expect(await screen.findByRole("heading", { name: "example.com" })).toBeInTheDocument();
    expect(requested).toBe("/api/v1/subdomains/example.com");
    expect(await screen.findByText("2 unique hostnames")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "api.example.com" })).toHaveAttribute("href", "/host/api.example.com");
    expect(screen.getByRole("link", { name: "we ird/host.example.com" })).toHaveAttribute("href", "/host/we%20ird%2Fhost.example.com");
  });

  it("shows empty and error states", async () => {
    mockFetch(() => json({ data: [], meta: { root_domain: "none.test", total: 0 } }));
    const { unmount } = renderRoute("/subdomain/none.test");
    expect(await screen.findByText("No hostnames under this domain")).toBeInTheDocument();
    expect(screen.getByText("0 unique hostnames")).toBeInTheDocument();
    unmount();

    mockFetch(() => json({ error: { code: "validation_error", message: "root_domain is invalid", field: "root_domain" } }, 400));
    renderRoute("/subdomain/bad.test");
    expect(await screen.findByText("Could not load subdomains")).toBeInTheDocument();
  });

  it("rejects invalid root domains without calling the API", async () => {
    const fetch = mockFetch(() => json({ status: "ok" }));
    renderRoute(`/subdomain/${"a".repeat(256)}`);
    expect(await screen.findByText("This root domain is not valid")).toBeInTheDocument();
    expect(fetch.mock.calls.some(([u]) => String(u).includes("/api/v1/subdomains"))).toBe(false);
  });
});

describe("HomePage root domains overview", () => {
  const home = (rootDomains: Response) =>
    mockFetch((url) => {
      if (url.pathname === "/api/v1/root_domains") return rootDomains;
      if (url.pathname === "/api/v1/http/artifacts") return json({ data: [], meta: listMeta });
      return json({ data: [domain()], meta: {} });
    });

  it("lists root domains with counts below the existing sections", async () => {
    home(json({ data: [{ root_domain: "example.com", hostname_count: 12 }, { root_domain: "solo.test", hostname_count: 1 }], meta: { limit: 100 } }));
    renderRoute("/");

    const overview = await screen.findByRole("region", { name: "Root Domains Overview" });
    const link = await within(overview).findByRole("link", { name: /example\.com/ });
    expect(link).toHaveAttribute("href", "/subdomain/example.com");
    expect(within(link).getByText("12 hostnames")).toBeInTheDocument();
    expect(within(overview).getByText("1 hostname")).toBeInTheDocument();

    const hosts = screen.getByRole("region", { name: "Recently scanned hosts" });
    expect(hosts.compareDocumentPosition(overview) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(screen.getByRole("region", { name: "HTTP artifacts" })).toBeInTheDocument();
  });

  it("shows an empty state", async () => {
    home(json({ data: [], meta: { limit: 100 } }));
    renderRoute("/");
    expect(await screen.findByText("No root domains yet")).toBeInTheDocument();
  });

  it("shows an error state", async () => {
    home(json({ error: { code: "validation_error", message: "limit is invalid", field: "limit" } }, 400));
    renderRoute("/");
    expect(await screen.findByText("Could not load root domains")).toBeInTheDocument();
  });
});
