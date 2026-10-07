import { KEYS, type StorageLike } from '../shared/state';

type Counters = Record<string, number>;

/**
 * The next unused ref number of each tab, so a ref number is never handed out twice for a tab: an
 * old ref fails as STALE_REF instead of naming some other element. It lives in
 * chrome.storage.local because an agent can hold refs across a service worker restart and even
 * an extension reload or update, both of which start a fresh page agent in the tab.
 */
export class RefCounters {
  private chain: Promise<unknown> = Promise.resolve();

  constructor(private readonly area: StorageLike) {}

  async next(tabId: number): Promise<number> {
    return (await this.all())[tabId] ?? 1;
  }

  /** Records a counter value; it only ever moves forward. */
  advance(tabId: number, next: number): Promise<void> {
    return this.exclusive(async () => {
      const all = await this.all();
      if ((all[tabId] ?? 1) >= next) return;
      all[tabId] = next;
      await this.area.set({ [KEYS.refs]: all });
    });
  }

  /** Drops the counters of tabs that are gone, e.g. closed while the extension was not running. */
  keepOnly(tabIds: Iterable<number>): Promise<void> {
    return this.exclusive(async () => {
      const live = new Set([...tabIds].map(String));
      const all = await this.all();
      const dead = Object.keys(all).filter((id) => !live.has(id));
      if (dead.length === 0) return;
      for (const id of dead) delete all[id];
      await this.area.set({ [KEYS.refs]: all });
    });
  }

  forget(tabId: number): Promise<void> {
    return this.exclusive(async () => {
      const all = await this.all();
      if (!(tabId in all)) return;
      delete all[tabId];
      await this.area.set({ [KEYS.refs]: all });
    });
  }

  private async all(): Promise<Counters> {
    return ((await this.area.get(KEYS.refs))[KEYS.refs] as Counters | undefined) ?? {};
  }

  private exclusive<T>(fn: () => Promise<T>): Promise<T> {
    const run = this.chain.then(fn, fn);
    this.chain = run.catch(() => {});
    return run;
  }
}
