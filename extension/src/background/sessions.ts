import { KEYS, type SessionState, type Sessions, type StorageLike } from '../shared/state';

function emptySession(): SessionState {
  return { groupId: null, tabIds: [], borrowedTabIds: [], currentTabId: null, stopped: false };
}

// Session names such as "constructor" pass the daemon's regex, and all[name] would return what a
// plain object inherits from Object.prototype instead of a session.
function own(all: Sessions, name: string): SessionState {
  return Object.hasOwn(all, name) ? all[name]! : emptySession();
}

/**
 * Session state in one storage key. Commands of different sessions and tab events run
 * concurrently, so every read-modify-write goes through one promise chain; without it two updates
 * would read the same snapshot and the second write would drop the first.
 */
export class SessionStore {
  private chain: Promise<unknown> = Promise.resolve();

  constructor(private readonly area: StorageLike) {}

  async all(): Promise<Sessions> {
    return ((await this.area.get(KEYS.sessions))[KEYS.sessions] as Sessions | undefined) ?? {};
  }

  async get(name: string): Promise<SessionState> {
    return own(await this.all(), name);
  }

  update(name: string, change: (s: SessionState) => void): Promise<SessionState> {
    return this.exclusive(async () => {
      const all = await this.all();
      const s = own(all, name);
      change(s);
      all[name] = s;
      await this.area.set({ [KEYS.sessions]: all });
      return s;
    });
  }

  remove(name: string): Promise<void> {
    return this.exclusive(async () => {
      const all = await this.all();
      delete all[name];
      await this.area.set({ [KEYS.sessions]: all });
    });
  }

  /** Drops a tab from every session, e.g. after the user closed it. Returns the sessions that held it. */
  forgetTab(tabId: number): Promise<string[]> {
    return this.eachHolding(tabId, (s) => {
      s.tabIds = s.tabIds.filter((t) => t !== tabId);
      s.borrowedTabIds = s.borrowedTabIds.filter((t) => t !== tabId);
      if (s.currentTabId === tabId) s.currentTabId = null;
    });
  }

  /** Stops every session holding the tab: the user pressed Cancel on its debugging bar. */
  stopSessionsOf(tabId: number): Promise<string[]> {
    return this.eachHolding(tabId, (s) => {
      s.stopped = true;
    });
  }

  private eachHolding(tabId: number, change: (s: SessionState) => void): Promise<string[]> {
    return this.exclusive(async () => {
      const all = await this.all();
      const touched = Object.entries(all).filter(([, s]) => s.tabIds.includes(tabId) || s.borrowedTabIds.includes(tabId));
      for (const [, s] of touched) change(s);
      if (touched.length > 0) await this.area.set({ [KEYS.sessions]: all });
      return touched.map(([name]) => name);
    });
  }

  private exclusive<T>(fn: () => Promise<T>): Promise<T> {
    const run = this.chain.then(fn, fn);
    this.chain = run.catch(() => {});
    return run;
  }
}
