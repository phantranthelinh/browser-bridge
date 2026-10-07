import { browser } from 'wxt/browser';
import { BridgeError } from './errors';

interface Waiter {
  onEvent(method: string, params: any): void;
  onDetach(reason: string): void;
}

export interface EventWait<T> {
  done: Promise<T>;
  cancel(): void;
}

/** Long-lived per-tab state (dialogs, network capture, page agent worlds) follows CDP through this. */
export interface CdpListener {
  event?(tabId: number, method: string, params: any): void;
  /** The tab is no longer attached: closed, cancelled by the user, or released by us. */
  detached?(tabId: number): void;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** chrome.debugger for tabs: attach once per tab and keep it, send commands, wait for events. */
export class Cdp {
  private attached = new Set<number>();
  private attaching = new Map<number, Promise<void>>();
  private waiters = new Map<number, Set<Waiter>>();
  private listeners: CdpListener[] = [];

  constructor() {
    browser.debugger.onEvent.addListener((source, method, params) => {
      if (source.tabId === undefined) return;
      for (const l of this.listeners) l.event?.(source.tabId, method, params);
      for (const w of [...(this.waiters.get(source.tabId) ?? [])]) w.onEvent(method, params);
    });
    browser.debugger.onDetach.addListener((source, reason) => {
      if (source.tabId === undefined) return;
      this.attached.delete(source.tabId);
      for (const l of this.listeners) l.detached?.(source.tabId);
      for (const w of [...(this.waiters.get(source.tabId) ?? [])]) w.onDetach(reason);
    });
  }

  subscribe(listener: CdpListener): void {
    this.listeners.push(listener);
  }

  /**
   * After a service worker restart the debugger sessions survive but this object does not: tabs
   * of our sessions that Chrome still reports as attached are taken to be ours.
   */
  async restore(tabIds: Iterable<number>): Promise<void> {
    const ours = new Set(tabIds);
    for (const t of await browser.debugger.getTargets()) {
      if (t.attached && t.tabId !== undefined && ours.has(t.tabId)) this.attached.add(t.tabId);
    }
  }

  attach(tabId: number): Promise<void> {
    if (this.attached.has(tabId)) return Promise.resolve();
    let p = this.attaching.get(tabId);
    if (!p) {
      p = this.doAttach(tabId).finally(() => this.attaching.delete(tabId));
      this.attaching.set(tabId, p);
    }
    return p;
  }

  private async doAttach(tabId: number): Promise<void> {
    try {
      await browser.debugger.attach({ tabId }, '1.3');
    } catch (e) {
      throw attachError(e);
    }
    this.attached.add(tabId);
    await this.raw(tabId, 'Page.enable');
    await this.raw(tabId, 'Runtime.enable');
    // Makes a background tab take real input as if it had focus (spike result in spec §13).
    await this.raw(tabId, 'Emulation.setFocusEmulationEnabled', { enabled: true });
  }

  async detach(tabId: number): Promise<void> {
    this.attached.delete(tabId);
    // Chrome fires onDetach only when it ends the session, not when we do.
    for (const l of this.listeners) l.detached?.(tabId);
    await browser.debugger.detach({ tabId }).catch(() => {});
  }

  /** Sends a command, attaching first. A session that vanished behind our back is re-attached once. */
  async send<T = any>(tabId: number, method: string, params?: Record<string, unknown>): Promise<T> {
    await this.attach(tabId);
    try {
      return await this.raw<T>(tabId, method, params);
    } catch (e) {
      if (!/not attached/i.test(message(e))) throw commandError(method, e);
    }
    this.attached.delete(tabId);
    await this.attach(tabId);
    try {
      return await this.raw<T>(tabId, method, params);
    } catch (e) {
      throw commandError(method, e);
    }
  }

  /** Starts listening before the command that causes the event, so a fast event is not missed. */
  waitForEvent<T = any>(tabId: number, match: (method: string, params: any) => boolean, deadline: number): EventWait<T> {
    let finish = () => {};
    const done = new Promise<T>((resolve, reject) => {
      const waiter: Waiter = {
        onEvent: (method, params) => {
          if (!match(method, params)) return;
          finish();
          resolve(params as T);
        },
        onDetach: (reason) => {
          finish();
          reject(
            reason === 'canceled_by_user'
              ? new BridgeError('DETACHED_BY_USER', 'the user pressed Cancel on the debugging bar', 'Ask the user before continuing')
              : new BridgeError('TAB_NOT_FOUND', 'the tab was closed'),
          );
        },
      };
      const timer = setTimeout(() => {
        finish();
        reject(new BridgeError('TIMEOUT', 'the page did not finish loading before timeoutMs', 'Retry with a larger timeoutMs'));
      }, Math.max(0, deadline - Date.now()));
      finish = () => {
        clearTimeout(timer);
        this.waiters.get(tabId)?.delete(waiter);
      };
      if (!this.waiters.has(tabId)) this.waiters.set(tabId, new Set());
      this.waiters.get(tabId)!.add(waiter);
    });
    return { done, cancel: () => finish() };
  }

  private async raw<T>(tabId: number, method: string, params?: Record<string, unknown>): Promise<T> {
    return (await browser.debugger.sendCommand({ tabId }, method, params)) as T;
  }
}

function attachError(e: unknown): BridgeError {
  const m = message(e);
  if (/no tab with/i.test(m)) return new BridgeError('TAB_NOT_FOUND', 'the tab no longer exists');
  if (/another debugger/i.test(m)) return new BridgeError('CDP_ERROR', m, 'Close DevTools on that tab: only one debugger can attach');
  return new BridgeError('RESTRICTED_URL', `this tab cannot be controlled: ${m}`, 'Browser pages such as chrome:// cannot be automated');
}

function commandError(method: string, e: unknown): BridgeError {
  const m = message(e);
  if (/no tab with/i.test(m)) return new BridgeError('TAB_NOT_FOUND', 'the tab no longer exists');
  return new BridgeError('CDP_ERROR', `${method}: ${m}`);
}
