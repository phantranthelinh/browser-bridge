import { browser } from 'wxt/browser';
import type {
  CloseSessionResult,
  CloseTabResult,
  FindTabArgs,
  FindTabResult,
  ListTabsResult,
  NavigateArgs,
  PageResult,
  TabResult,
} from '../../generated/protocol';
import type { Cdp, EventWait } from '../cdp';
import { BridgeError, blockedHost, noCurrentTab } from '../errors';
import { hostMatches, httpHost, isBlocked, isRestricted, queryHost } from '../hosts';
import type { Ctx, Handler } from '../router';

export interface TabCtx extends Ctx {
  cdp: Cdp;
}

async function currentTab(ctx: TabCtx): Promise<number> {
  const s = await ctx.sessions.get(ctx.session);
  if (s.currentTabId === null) throw noCurrentTab(ctx.session);
  return s.currentTabId;
}

/** Resolves when the main frame finished loading. A back/forward-cache restore fires no load event. */
function waitForLoad(ctx: TabCtx, tabId: number): EventWait<unknown> {
  return ctx.cdp.waitForEvent(
    tabId,
    (method, p) =>
      method === 'Page.loadEventFired' ||
      (method === 'Page.frameNavigated' && !p.frame.parentId && p.type === 'BackForwardCacheRestore'),
    ctx.deadline,
  );
}

function waitForSameDocument(ctx: TabCtx, tabId: number): EventWait<unknown> {
  return ctx.cdp.waitForEvent(tabId, (method) => method === 'Page.navigatedWithinDocument', ctx.deadline);
}

/** URL and title once the page settled; a page that ended up on a blocked host reveals neither. */
async function settled(ctx: TabCtx, tabId: number): Promise<PageResult> {
  const tab = await browser.tabs.get(tabId);
  if (isBlocked(tab.url, ctx.blockedHosts())) throw blockedHost();
  return { url: tab.url ?? '', title: tab.title ?? '' };
}

/** Runs a command that starts a navigation, then waits for it unless the command failed. */
async function navigateWith(wait: EventWait<unknown>, start: () => Promise<unknown>): Promise<void> {
  try {
    await start();
  } catch (e) {
    wait.cancel();
    throw e;
  }
  await wait.done;
}

async function openSessionTab(ctx: TabCtx, groupTitle: string | undefined): Promise<number> {
  const tab = await browser.tabs.create({ url: 'about:blank', active: false });
  const tabId = tab.id!;
  const s = await ctx.sessions.get(ctx.session);
  let groupId: number | null = null;
  if (s.groupId !== null) {
    groupId = await browser.tabs.group({ groupId: s.groupId, tabIds: [tabId] }).catch(() => null); // the user removed the group
  }
  if (groupId === null) {
    groupId = await browser.tabs.group({ tabIds: [tabId] });
    await browser.tabGroups.update(groupId, { title: groupTitle || ctx.session });
  }
  await ctx.sessions.update(ctx.session, (st) => {
    st.groupId = groupId;
    st.tabIds.push(tabId);
    st.currentTabId = tabId;
    st.stopped = false;
  });
  return tabId;
}

const navigate: Handler<TabCtx> = async (ctx, args: NavigateArgs): Promise<TabResult> => {
  const s = await ctx.sessions.get(ctx.session);
  const fresh = s.currentTabId === null || args.newTab === true;
  const tabId = fresh ? await openSessionTab(ctx, args.groupTitle) : s.currentTabId!;
  if (!fresh) await ctx.sessions.update(ctx.session, (st) => void (st.stopped = false));

  const wait = waitForLoad(ctx, tabId);
  let nav: { errorText?: string; loaderId?: string };
  try {
    nav = await ctx.cdp.send(tabId, 'Page.navigate', { url: args.url });
  } catch (e) {
    wait.cancel();
    throw e;
  }
  if (nav.errorText) {
    wait.cancel();
    throw new BridgeError('NAVIGATION_FAILED', `${args.url}: ${nav.errorText}`, 'Check the URL and that the site is reachable');
  }
  // A same-document navigation (only the #fragment changed) has no loader and fires no load event.
  if (nav.loaderId) await wait.done;
  else wait.cancel();
  // The about:blank the tab was created with would otherwise be a history entry go_back can reach.
  if (fresh) await ctx.cdp.send(tabId, 'Page.resetNavigationHistory');
  return { tabId, ...(await settled(ctx, tabId)) };
};

const findTab: Handler<TabCtx> = async (ctx, args: FindTabArgs): Promise<FindTabResult> => {
  const blocked = ctx.blockedHosts();
  const wanted = args.url ? queryHost(args.url) : null;
  if (args.url && !wanted) throw new BridgeError('INVALID_REQUEST', `cannot read a host from ${args.url}`);

  if (args.active) {
    const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
    if (!tab?.id) throw new BridgeError('TAB_NOT_FOUND', 'there is no active tab');
    const host = httpHost(tab.url);
    if (wanted && !(host && hostMatches(host, wanted))) {
      throw new BridgeError('TAB_NOT_FOUND', `the tab the user is looking at is not on ${wanted}`, 'Ask the user to switch to that tab, or use navigate');
    }
    if (isRestricted(tab.url)) throw new BridgeError('RESTRICTED_URL', 'the tab the user is looking at is a browser page that cannot be automated');
    if (isBlocked(tab.url, blocked)) throw blockedHost();
    const id = tab.id;
    const st = await ctx.sessions.update(ctx.session, (st) => {
      if (!st.tabIds.includes(id) && !st.borrowedTabIds.includes(id)) st.borrowedTabIds.push(id);
      st.currentTabId = id;
      st.stopped = false;
    });
    return { tabId: id, url: tab.url ?? '', title: tab.title ?? '', borrowed: st.borrowedTabIds.includes(id) };
  }

  const s = await ctx.sessions.get(ctx.session);
  for (const id of [...s.tabIds, ...s.borrowedTabIds]) {
    const tab = await browser.tabs.get(id).catch(() => null);
    const host = httpHost(tab?.url);
    if (!tab || !host || !hostMatches(host, wanted!) || isBlocked(tab.url, blocked)) continue;
    await ctx.sessions.update(ctx.session, (st) => {
      st.currentTabId = id;
      st.stopped = false;
    });
    return { tabId: id, url: tab.url ?? '', title: tab.title ?? '', borrowed: s.borrowedTabIds.includes(id) };
  }
  throw new BridgeError(
    'TAB_NOT_FOUND',
    `no tab of session ${ctx.session} is on ${wanted}`,
    'Use navigate to open it, or find_tab with active:true to borrow the tab the user is looking at',
  );
};

const listTabs: Handler<TabCtx> = async (ctx): Promise<ListTabsResult> => {
  const s = await ctx.sessions.get(ctx.session);
  const blocked = ctx.blockedHosts();
  const tabs: ListTabsResult['tabs'] = [];
  for (const id of [...s.tabIds, ...s.borrowedTabIds]) {
    const tab = await browser.tabs.get(id).catch(() => null);
    if (!tab) continue;
    // A tab that ended up on a blocked host stays listed so the agent can close it, but blank.
    const hidden = isBlocked(tab.url, blocked);
    tabs.push({
      tabId: id,
      url: hidden ? '' : (tab.url ?? ''),
      title: hidden ? '' : (tab.title ?? ''),
      current: id === s.currentTabId,
      borrowed: s.borrowedTabIds.includes(id),
    });
  }
  return { tabs };
};

const closeTab: Handler<TabCtx> = async (ctx): Promise<CloseTabResult> => {
  const tabId = await currentTab(ctx);
  const s = await ctx.sessions.get(ctx.session);
  if (s.borrowedTabIds.includes(tabId)) {
    // A borrowed tab belongs to the user: give it back, never close it.
    await ctx.cdp.detach(tabId);
    await ctx.sessions.update(ctx.session, (st) => {
      st.borrowedTabIds = st.borrowedTabIds.filter((t) => t !== tabId);
      st.currentTabId = null;
    });
    return { closed: false, released: true };
  }
  await browser.tabs.remove(tabId).catch(() => {}); // already closed by the user is fine
  await ctx.sessions.update(ctx.session, (st) => {
    st.tabIds = st.tabIds.filter((t) => t !== tabId);
    st.currentTabId = null;
  });
  return { closed: true, released: false };
};

const closeSession: Handler<TabCtx> = async (ctx): Promise<CloseSessionResult> => {
  const s = await ctx.sessions.get(ctx.session);
  let closed = 0;
  for (const id of s.tabIds) {
    try {
      await browser.tabs.remove(id);
      closed++;
    } catch {
      // closed by the user already
    }
  }
  for (const id of s.borrowedTabIds) await ctx.cdp.detach(id);
  await ctx.sessions.remove(ctx.session); // Chrome removes the group once its last tab is gone
  return { closed };
};

const stripHash = (url: string) => url.split('#')[0];

function historyStep(delta: -1 | 1): Handler<TabCtx> {
  return async (ctx): Promise<PageResult> => {
    const tabId = await currentTab(ctx);
    const h = await ctx.cdp.send<{ currentIndex: number; entries: { id: number; url: string }[] }>(tabId, 'Page.getNavigationHistory');
    const target = h.entries[h.currentIndex + delta];
    if (!target) {
      throw new BridgeError('NAVIGATION_FAILED', delta < 0 ? 'there is no page to go back to' : 'there is no page to go forward to');
    }
    const sameDocument = stripHash(target.url) === stripHash(h.entries[h.currentIndex]?.url ?? '');
    const wait = sameDocument ? waitForSameDocument(ctx, tabId) : waitForLoad(ctx, tabId);
    await navigateWith(wait, () => ctx.cdp.send(tabId, 'Page.navigateToHistoryEntry', { entryId: target.id }));
    return settled(ctx, tabId);
  };
}

const reload: Handler<TabCtx> = async (ctx): Promise<PageResult> => {
  const tabId = await currentTab(ctx);
  await navigateWith(waitForLoad(ctx, tabId), () => ctx.cdp.send(tabId, 'Page.reload'));
  return settled(ctx, tabId);
};

export const tabHandlers = {
  navigate,
  find_tab: findTab,
  list_tabs: listTabs,
  close_tab: closeTab,
  close_session: closeSession,
  go_back: historyStep(-1),
  go_forward: historyStep(1),
  reload,
};
