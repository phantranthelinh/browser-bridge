import { test as base, chromium, expect, type BrowserContext, type Worker } from '@playwright/test';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { once } from 'node:events';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BRIDGE_BIN, DAEMON_PORT, EXTENSION_DIR } from './paths';
import { startSite, type Site } from './site';

// Code passed to sw.evaluate runs in the extension's service worker, where chrome.* exists.
declare const chrome: any;

export interface Envelope<T = any> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string; hint?: string };
}

/** The daemon's HTTP API, the way an agent calls it. */
export class Bridge {
  constructor(
    readonly base: string,
    /** The daemon's log so far. */
    readonly log: () => string = () => '',
  ) {}

  async command<T = any>(session: string, action: string, args: object = {}, timeoutMs?: number): Promise<Envelope<T>> {
    const res = await fetch(`${this.base}/command`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, args, session, timeoutMs }),
    });
    return (await res.json()) as Envelope<T>;
  }

  async status(): Promise<any> {
    return (await fetch(`${this.base}/status`)).json();
  }
}

/** Asserts ok and returns data, printing the error when there is one. */
export async function ok<T = any>(p: Promise<Envelope<T>>): Promise<T> {
  const env = await p;
  expect(env.error, JSON.stringify(env.error)).toBeUndefined();
  expect(env.ok).toBe(true);
  return env.data as T;
}

export interface TabInfo {
  active: boolean;
  url: string;
  groupTitle: string | null;
}

/** What Chrome itself reports about a tab, or null once it is closed. */
export function tabInfo(sw: Worker, tabId: number): Promise<TabInfo | null> {
  return sw.evaluate(async (id: number) => {
    const t = await chrome.tabs.get(id).catch(() => null);
    if (!t) return null;
    const group = t.groupId === -1 ? null : await chrome.tabGroups.get(t.groupId);
    return { active: t.active, url: t.url, groupTitle: group ? group.title : null };
  }, tabId);
}

type WorkerFixtures = { site: Site; daemon: Bridge; browserCtx: BrowserContext; bridge: Bridge; sw: Worker };
type TestFixtures = { session: string };

export const test = base.extend<TestFixtures, WorkerFixtures>({
  site: [
    async ({}, use) => {
      const site = await startSite();
      await use(site);
      await site.close();
    },
    { scope: 'worker' },
  ],

  daemon: [
    async ({}, use) => {
      const home = mkdtempSync(join(tmpdir(), 'bridge-e2e-'));
      writeFileSync(join(home, 'config.json'), JSON.stringify({ blockedHosts: ['blocked.localhost'] }));
      const proc = spawn(BRIDGE_BIN, ['serve', '--addr', `127.0.0.1:${DAEMON_PORT}`], {
        env: { ...process.env, BRIDGE_HOME: home },
        stdio: ['ignore', 'ignore', 'pipe'],
      });
      let stderr = '';
      proc.stderr!.on('data', (d) => (stderr += d));
      const bridge = new Bridge(`http://127.0.0.1:${DAEMON_PORT}`, () => stderr);
      await expect
        .poll(() => bridge.status().then((s) => s.running, () => false), { timeout: 10_000, message: `daemon did not start:\n${stderr}` })
        .toBe(true);
      // A daemon left over from the previous worker would also answer /status; ours must still be alive.
      expect(proc.exitCode, `daemon exited:\n${stderr}`).toBeNull();
      await use(bridge);
      // Wait for the exit: after a failed test Playwright starts a new worker at once, and its daemon
      // needs the port this one holds.
      const exited = once(proc, 'exit');
      proc.kill();
      await exited;
    },
    { scope: 'worker' },
  ],

  browserCtx: [
    async ({ daemon }, use) => {
      const ctx = await chromium.launchPersistentContext(mkdtempSync(join(tmpdir(), 'bridge-e2e-profile-')), {
        // The "chromium" channel runs new headless mode, which loads extensions; HEADED=1 shows the window.
        channel: 'chromium',
        headless: process.env.HEADED !== '1',
        args: [`--disable-extensions-except=${EXTENSION_DIR}`, `--load-extension=${EXTENSION_DIR}`],
      });
      await expect.poll(async () => (await daemon.status()).extension.connected, { timeout: 15_000, message: 'extension never connected' }).toBe(true);
      // Playwright dismisses every JS dialog that has no listener, the extension's tabs included.
      // An idle listener leaves them open for handle_dialog, as in a browser nobody automates.
      ctx.on('dialog', () => {});
      await use(ctx);
      await ctx.close();
    },
    { scope: 'worker' },
  ],

  // Tests talk to the daemon through this fixture, so a connected browser is always behind it, even
  // in a fresh worker after a failed test.
  bridge: [
    async ({ daemon, browserCtx }, use) => {
      expect(browserCtx).toBeTruthy();
      await use(daemon);
    },
    { scope: 'worker' },
  ],

  sw: [
    async ({ browserCtx }, use) => {
      const sw = browserCtx.serviceWorkers()[0] ?? (await browserCtx.waitForEvent('serviceworker'));
      await use(sw);
    },
    { scope: 'worker' },
  ],

  // The browser is shared by every test of a worker, so each test gets its own session.
  session: async ({}, use) => {
    await use(`e2e-${randomBytes(4).toString('hex')}`);
  },
});

export { expect };
