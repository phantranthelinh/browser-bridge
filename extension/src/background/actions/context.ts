import type { Cdp } from '../cdp';
import type { Dialogs } from '../dialogs';
import { noCurrentTab } from '../errors';
import type { NetworkCapture } from '../network';
import type { PageAgentClient } from '../page';
import type { RefCounters } from '../refs';
import type { Ctx } from '../router';

/** What the browser actions work with besides the session store. */
export interface TabCtx extends Ctx {
  cdp: Cdp;
  page: PageAgentClient;
  dialogs: Dialogs;
  network: NetworkCapture;
  refs: RefCounters;
}

export async function currentTab(ctx: TabCtx): Promise<number> {
  const s = await ctx.sessions.get(ctx.session);
  if (s.currentTabId === null) throw noCurrentTab(ctx.session);
  return s.currentTabId;
}
