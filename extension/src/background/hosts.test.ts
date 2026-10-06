import { describe, expect, it } from 'vitest';
import { hostMatches, httpHost, isBlocked, isRestricted, normalizeHost, queryHost } from './hosts';

describe('hosts', () => {
  it('normalizes case and the trailing dot', () => {
    expect(normalizeHost('BANK.com.')).toBe('bank.com');
  });

  it('reads hosts only from http(s) URLs', () => {
    expect(httpHost('https://www.Kimi.com/chat')).toBe('www.kimi.com');
    expect(httpHost('http://bank.com:8443/')).toBe('bank.com');
    expect(httpHost('about:blank')).toBeNull();
    expect(httpHost('chrome://settings')).toBeNull();
    expect(httpHost('not a url')).toBeNull();
    expect(httpHost(undefined)).toBeNull();
  });

  it('reads the host from what an agent passes to find_tab', () => {
    expect(queryHost('kimi.com')).toBe('kimi.com');
    expect(queryHost('www.kimi.com/chat')).toBe('www.kimi.com');
    expect(queryHost('https://kimi.com/chat?x=1')).toBe('kimi.com');
  });

  it('matches a host and its subdomains, never a lookalike suffix', () => {
    expect(hostMatches('kimi.com', 'kimi.com')).toBe(true);
    expect(hostMatches('www.kimi.com', 'kimi.com')).toBe(true);
    expect(hostMatches('notkimi.com', 'kimi.com')).toBe(false);
    expect(hostMatches('kimi.com.evil.net', 'kimi.com')).toBe(false);
  });

  it('blocks the same URLs as the daemon', () => {
    const blocked = ['bank.com'];
    for (const url of ['https://bank.com/login', 'https://www.bank.com', 'https://BANK.com./', 'https://bank.com:8443/']) {
      expect(isBlocked(url, blocked), url).toBe(true);
    }
    for (const url of ['https://notbank.com/', 'https://bank.com.evil.net/', 'about:blank', undefined]) {
      expect(isBlocked(url, blocked), String(url)).toBe(false);
    }
  });

  it('treats browser pages and extension stores as restricted', () => {
    for (const url of ['chrome://newtab/', 'edge://settings', 'file:///C:/x.txt', 'https://chromewebstore.google.com/detail/x', 'https://chrome.google.com/webstore/x']) {
      expect(isRestricted(url), url).toBe(true);
    }
    for (const url of ['about:blank', 'https://kimi.com/', 'http://localhost:3000/']) {
      expect(isRestricted(url), url).toBe(false);
    }
  });
});
