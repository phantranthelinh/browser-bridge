import { expect, ok, test } from '../fixtures';

// The daemon drops a socket that is silent for 60s, and Chrome stops an idle service worker after
// 30s. The 20s ping has to prevent both.
test('stays connected while idle for longer than the daemon idle timeout', async ({ bridge, session, site }) => {
  test.slow();
  await new Promise((r) => setTimeout(r, 70_000));
  expect((await bridge.status()).extension.connected).toBe(true);
  expect(bridge.log()).not.toContain('extension disconnected');
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
});
