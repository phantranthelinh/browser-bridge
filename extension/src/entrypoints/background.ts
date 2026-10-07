import { browser } from 'wxt/browser';
import { defineBackground } from 'wxt/utils/define-background';
import { tabHandlers, type TabCtx } from '../background/actions/tabs';
import { Cdp } from '../background/cdp';
import { Connection } from '../background/connection';
import { CommandLog } from '../background/log';
import { createRouter } from '../background/router';
import { SessionStore } from '../background/sessions';
import type { Hello } from '../generated/protocol';
import { DEFAULT_DAEMON_URL, KEYS } from '../shared/state';

// Typed as the schema's literal: when the daemon bumps ProtocolVersion, this line stops compiling.
const PROTOCOL_VERSION: Hello['protocolVersion'] = 2;

export default defineBackground(() => {
  const sessions = new SessionStore(browser.storage.session);
  const log = new CommandLog(browser.storage.session);
  const cdp = new Cdp();
  let blockedHosts: string[] = [];

  const route = createRouter<TabCtx>({
    sessions,
    handlers: tabHandlers,
    blockedHosts: () => blockedHosts,
    tabUrl: (tabId) => browser.tabs.get(tabId).then((t) => t.url, () => undefined),
    makeCtx: (session, deadline) => ({ session, deadline, sessions, cdp, blockedHosts: () => blockedHosts }),
  });

  const connection = new Connection({
    url: async () => ((await browser.storage.local.get(KEYS.daemonUrl))[KEYS.daemonUrl] as string | undefined) || DEFAULT_DAEMON_URL,
    hello: () => ({
      type: 'hello',
      protocolVersion: PROTOCOL_VERSION,
      extensionVersion: browser.runtime.getManifest().version,
      extensionId: browser.runtime.id,
      browser: navigator.userAgent.includes('Edg/') ? 'edge' : 'chrome',
      actions: Object.keys(tabHandlers),
    }),
    onWelcome: (w) => {
      blockedHosts = w.blockedHosts ?? [];
    },
    onStatus: (s) => void browser.storage.session.set({ [KEYS.connection]: s }),
    onRequest: async (frame) => {
      const start = Date.now();
      const resp = await route(frame);
      const selector = (frame.args as { selector?: unknown } | undefined)?.selector;
      void log.add({
        at: start,
        session: frame.session,
        action: frame.action,
        selector: typeof selector === 'string' ? selector : undefined,
        ms: Date.now() - start,
        code: resp.error?.code,
      });
      return resp;
    },
  });

  browser.tabs.onRemoved.addListener((tabId) => void sessions.forgetTab(tabId));
  browser.debugger.onDetach.addListener((source, reason) => {
    if (source.tabId === undefined) return;
    if (reason === 'canceled_by_user') void sessions.stopSessionsOf(source.tabId);
    else void sessions.forgetTab(source.tabId);
  });
  browser.storage.onChanged.addListener((changes, area) => {
    if (area === 'local' && KEYS.daemonUrl in changes) connection.reconnect();
  });
  // Wakes the worker to retry while the daemon is down; 30s is the shortest period Chrome allows.
  void browser.alarms.create('reconnect', { periodInMinutes: 0.5 });
  browser.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name === 'reconnect') void connection.connect();
  });
  void browser.sidePanel.setPanelBehavior({ openPanelOnActionClick: true });

  void sessions.all().then((all) => cdp.restore(Object.values(all).flatMap((s) => [...s.tabIds, ...s.borrowedTabIds])));
  void connection.connect();
});
