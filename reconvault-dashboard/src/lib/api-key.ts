/**
 * The API key is entered by the user and kept in sessionStorage only: it is
 * never bundled into the build, and it is discarded when the tab closes.
 */
const STORAGE_KEY = "reconvault.apiKey";
const listeners = new Set<() => void>();

function read(): string | null {
  try {
    return sessionStorage.getItem(STORAGE_KEY);
  } catch {
    return null; // storage unavailable (privacy mode, sandboxed frame)
  }
}

let current = read();

export function getApiKey(): string | null {
  return current;
}

export function setApiKey(key: string | null): void {
  const next = key && key.trim() ? key.trim() : null;
  // Notifying on a no-op would refetch every query, and a refetch that
  // fails with 401 clears the key again: an endless request loop.
  if (next === current) return;
  current = next;
  try {
    if (current) sessionStorage.setItem(STORAGE_KEY, current);
    else sessionStorage.removeItem(STORAGE_KEY);
  } catch {
    // Keep the in-memory value; it still works for this page load.
  }
  listeners.forEach((l) => l());
}

export function subscribeApiKey(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}
