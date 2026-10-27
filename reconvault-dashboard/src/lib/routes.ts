/** Route builders; parameters are always URL-encoded. */
export const routes = {
  home: "/",
  search: "/search",
};

export function hostPath(hostname: string): string {
  return `/host/${encodeURIComponent(hostname)}`;
}

export function subdomainPath(rootDomain: string): string {
  return `/subdomain/${encodeURIComponent(rootDomain)}`;
}

export function artifactPath(id: string): string {
  return `/http/${encodeURIComponent(id)}`;
}

export function searchPath(q?: string): string {
  return q ? `/search?${new URLSearchParams({ q }).toString()}` : "/search";
}
