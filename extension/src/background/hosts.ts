// Host rules shared by blockedHosts and find_tab. They mirror the daemon's checks.go, so a host the
// daemon refuses at navigate is also refused when a page redirects there.

export function normalizeHost(host: string): string {
  return host.toLowerCase().replace(/\.$/, '');
}

/** Host of an http(s) URL; null for about:, chrome:, data: and anything unparsable. */
export function httpHost(url: string | undefined): string | null {
  if (!url) return null;
  try {
    const u = new URL(url);
    return u.protocol === 'http:' || u.protocol === 'https:' ? normalizeHost(u.hostname) : null;
  } catch {
    return null;
  }
}

/** Reads the host from what an agent passes to find_tab: "kimi.com", "www.kimi.com/chat" or a full URL. */
export function queryHost(query: string): string | null {
  return httpHost(query.includes('://') ? query : `http://${query}`);
}

/** True for the host itself and any subdomain of it. */
export function hostMatches(host: string, base: string): boolean {
  return host === base || host.endsWith('.' + base);
}

export function isBlocked(url: string | undefined, blockedHosts: readonly string[]): boolean {
  const host = httpHost(url);
  return host !== null && blockedHosts.some((b) => hostMatches(host, b));
}

/** Pages an extension cannot drive: browser pages and the extension stores. */
export function isRestricted(url: string | undefined): boolean {
  if (!url || url === 'about:blank') return false;
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return true;
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return true;
  const host = normalizeHost(u.hostname);
  return (
    host === 'chromewebstore.google.com' ||
    (host === 'chrome.google.com' && u.pathname.startsWith('/webstore')) ||
    (host === 'microsoftedge.microsoft.com' && u.pathname.startsWith('/addons'))
  );
}
