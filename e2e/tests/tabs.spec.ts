import { expect, ok, tabInfo, test } from '../fixtures';

declare const chrome: any;

test('the extension connects and identifies itself', async ({ bridge }) => {
  const st = await bridge.status();
  expect(st.extension).toMatchObject({ connected: true, id: 'nfjidhefdgblbbfhnmbcogkbphipngif', version: '0.3.0', browser: 'chrome' });
});

test('navigate opens one background tab per session, grouped under the session name', async ({ bridge, session, site, sw }) => {
  const first = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  expect(first).toMatchObject({ url: site.url('/index.html'), title: 'Bridge test page' });
  expect(await tabInfo(sw, first.tabId)).toMatchObject({ active: false, groupTitle: session });

  const reused = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(reused).toMatchObject({ tabId: first.tabId, title: 'Bridge test page 2' });

  const second = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html'), newTab: true }));
  expect(second.tabId).not.toBe(first.tabId);
  expect(await tabInfo(sw, second.tabId)).toMatchObject({ active: false, groupTitle: session });
});

test('groupTitle names the group when it is created', async ({ bridge, session, site, sw }) => {
  const r = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html'), groupTitle: 'Jira report' }));
  expect((await tabInfo(sw, r.tabId))!.groupTitle).toBe('Jira report');
});

test('navigate reports a site that cannot be reached', async ({ bridge, session }) => {
  const r = await bridge.command(session, 'navigate', { url: 'http://127.0.0.1:1/' });
  expect(r.error?.code).toBe('NAVIGATION_FAILED');
});

test('back and forward walk the history and stop at its ends', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(await ok(bridge.command(session, 'go_back'))).toEqual({ url: site.url('/index.html'), title: 'Bridge test page' });
  // the tab was created on about:blank; that entry must not be reachable
  expect((await bridge.command(session, 'go_back')).error?.code).toBe('NAVIGATION_FAILED');
  expect(await ok(bridge.command(session, 'go_forward'))).toEqual({ url: site.url('/page2.html'), title: 'Bridge test page 2' });
  expect((await bridge.command(session, 'go_forward')).error?.code).toBe('NAVIGATION_FAILED');
});

test('history steps inside one document finish', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html#a') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html#b') }));
  expect((await ok(bridge.command(session, 'go_back', {}, 5000))).url).toBe(site.url('/index.html#a'));
});

test('reload answers with the page', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(await ok(bridge.command(session, 'reload'))).toEqual({ url: site.url('/page2.html'), title: 'Bridge test page 2' });
});

test('list_tabs and close_tab', async ({ bridge, session, site, sw }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  const b = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  const listed = await ok(bridge.command(session, 'list_tabs'));
  expect(listed.tabs).toEqual([
    { tabId: a.tabId, url: site.url('/index.html'), title: 'Bridge test page', current: false, borrowed: false },
    { tabId: b.tabId, url: site.url('/page2.html'), title: 'Bridge test page 2', current: true, borrowed: false },
  ]);
  expect(await ok(bridge.command(session, 'close_tab'))).toEqual({ closed: true, released: false });
  expect(await tabInfo(sw, b.tabId)).toBeNull();
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toEqual([
    { tabId: a.tabId, url: site.url('/index.html'), title: 'Bridge test page', current: false, borrowed: false },
  ]);
  expect((await bridge.command(session, 'reload')).error?.code).toBe('NO_CURRENT_TAB');
});

test('find_tab picks a session tab by host', async ({ bridge, session, site }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html', 'localhost') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  expect(await ok(bridge.command(session, 'find_tab', { url: 'localhost' }))).toMatchObject({ tabId: a.tabId, borrowed: false });
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs.find((t: any) => t.current).tabId).toBe(a.tabId);
  const missing = await bridge.command(session, 'find_tab', { url: 'example.org' });
  expect(missing.error?.code).toBe('TAB_NOT_FOUND');
  expect(missing.error?.hint).toContain('navigate');
});

test('find_tab active borrows the user tab, leaves it ungrouped, and close_tab only releases it', async ({ bridge, browserCtx, session, site, sw }) => {
  const page = await browserCtx.newPage();
  await page.goto(site.url('/page2.html'));
  await page.bringToFront();
  const r = await ok(bridge.command(session, 'find_tab', { active: true }));
  expect(r).toMatchObject({ url: site.url('/page2.html'), title: 'Bridge test page 2', borrowed: true });
  expect((await tabInfo(sw, r.tabId))!.groupTitle).toBeNull();
  expect(await ok(bridge.command(session, 'reload'))).toMatchObject({ title: 'Bridge test page 2' });
  expect(await ok(bridge.command(session, 'close_tab'))).toEqual({ closed: false, released: true });
  expect(page.isClosed()).toBe(false);
  expect(await tabInfo(sw, r.tabId)).not.toBeNull();
  await page.close();
});

test('find_tab active refuses a tab on another host', async ({ bridge, browserCtx, session, site }) => {
  const page = await browserCtx.newPage();
  await page.goto(site.url('/index.html'));
  await page.bringToFront();
  expect((await bridge.command(session, 'find_tab', { active: true, url: 'example.org' })).error?.code).toBe('TAB_NOT_FOUND');
  await page.close();
});

test('close_session closes its own tabs and gives borrowed ones back', async ({ bridge, browserCtx, session, site, sw }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  const b = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  const page = await browserCtx.newPage();
  await page.goto(site.url('/index.html'));
  await page.bringToFront();
  await ok(bridge.command(session, 'find_tab', { active: true }));
  expect(await ok(bridge.command(session, 'close_session'))).toEqual({ closed: 2 });
  expect(await tabInfo(sw, a.tabId)).toBeNull();
  expect(await tabInfo(sw, b.tabId)).toBeNull();
  expect(page.isClosed()).toBe(false);
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toEqual([]);
  await page.close();
});

test('blocked hosts: refused up front, caught after a redirect, and the agent can still leave', async ({ bridge, session, site }) => {
  const direct = await bridge.command(session, 'navigate', { url: site.url('/index.html', 'blocked.localhost') });
  expect(direct.error?.code).toBe('BLOCKED_HOST');

  const viaRedirect = await bridge.command(session, 'navigate', {
    url: site.url('/redirect?to=' + encodeURIComponent(site.url('/index.html', 'blocked.localhost'))),
  });
  expect(viaRedirect.error?.code).toBe('BLOCKED_HOST');
  expect(JSON.stringify(viaRedirect)).not.toContain('Bridge test page');

  expect((await bridge.command(session, 'reload')).error?.code).toBe('BLOCKED_HOST');
  // still listed, so the agent can close it, but without the blocked page's URL or title
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toEqual([
    { tabId: expect.any(Number), url: '', title: '', current: true, borrowed: false },
  ]);
  expect(await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }))).toMatchObject({ title: 'Bridge test page' });
});

test('a tab the user closes leaves the session', async ({ bridge, session, site, sw }) => {
  const r = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  await sw.evaluate((id: number) => chrome.tabs.remove(id), r.tabId);
  await expect.poll(async () => (await ok(bridge.command(session, 'list_tabs'))).tabs.length).toBe(0);
  expect((await bridge.command(session, 'reload')).error?.code).toBe('NO_CURRENT_TAB');
});
