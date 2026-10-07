import type { ActionName, Dialog, RequestFrame, ResponseFrame } from '../generated/protocol';
import { BridgeError, blockedHost, toErrorBody } from './errors';
import { isBlocked } from './hosts';
import type { SessionStore } from './sessions';

/** What every action handler gets besides its args. */
export interface Ctx {
  session: string;
  /** Epoch ms after which the daemon has already answered TIMEOUT. */
  deadline: number;
  sessions: SessionStore;
  blockedHosts(): readonly string[];
}

export type Handler<C extends Ctx = Ctx> = (ctx: C, args: any) => Promise<unknown>;

// Spec §6.1 and §7.1: these never get stuck behind a dialog, a Cancel or a blocked host, so an
// agent can always get out of a tab.
const CLEANUP = new Set<string>(['list_tabs', 'close_tab', 'close_session']);
// Moving away from a blocked page has to work; their result is checked after the page loads.
const LEAVES_PAGE = new Set<string>(['navigate', 'find_tab', 'go_back', 'go_forward']);
// The two actions that resume a session the user stopped with Cancel.
const RESUMES = new Set<string>(['navigate', 'find_tab']);
const ANSWERS_DIALOG = 'handle_dialog';

export interface RouterDeps<C extends Ctx> {
  sessions: SessionStore;
  handlers: Partial<Record<ActionName, Handler<C>>>;
  makeCtx(session: string, deadline: number): C;
  tabUrl(tabId: number): Promise<string | undefined>;
  blockedHosts(): readonly string[];
  /** The JS dialog open in a tab, if any. */
  dialog(tabId: number): Dialog | undefined;
}

export function createRouter<C extends Ctx>(deps: RouterDeps<C>): (frame: RequestFrame) => Promise<ResponseFrame> {
  async function run(frame: RequestFrame): Promise<unknown> {
    const action = frame.action;
    const handler = deps.handlers[action as ActionName];
    if (!handler) throw new BridgeError('INTERNAL', `${action} is not implemented in this extension version`);
    const s = await deps.sessions.get(frame.session);
    if (s.stopped && !CLEANUP.has(action) && !RESUMES.has(action)) {
      throw new BridgeError(
        'DETACHED_BY_USER',
        `the user pressed Cancel on the debugging bar of session ${frame.session}`,
        'Ask the user before continuing. navigate or find_tab resumes the session',
      );
    }
    if (!CLEANUP.has(action) && !LEAVES_PAGE.has(action) && s.currentTabId !== null) {
      if (isBlocked(await deps.tabUrl(s.currentTabId), deps.blockedHosts())) throw blockedHost();
    }
    // An open dialog stops the page's scripts, and with them anything sent to the tab: refused
    // now, such a command would only time out.
    const dialog = s.currentTabId === null ? undefined : deps.dialog(s.currentTabId);
    if (dialog && !CLEANUP.has(action) && action !== ANSWERS_DIALOG) {
      throw new BridgeError(
        'DIALOG_OPEN',
        `the current tab shows a ${dialog.type} dialog`,
        `Call handle_dialog with accept true or false first. The dialog says: "${dialog.message.slice(0, 200)}"`,
      );
    }
    return handler(deps.makeCtx(frame.session, frame.deadline), frame.args ?? {});
  }

  return async (frame) => {
    try {
      const data = await run(frame);
      return { type: 'response', id: frame.id, ok: true, data: data ?? {} };
    } catch (e) {
      return { type: 'response', id: frame.id, ok: false, error: toErrorBody(e) };
    }
  };
}
