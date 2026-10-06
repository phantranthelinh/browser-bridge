// State shared by the service worker, which writes it, and the side panel, which displays it.

// import.meta.env.WXT_DAEMON_URL lets the E2E build point at its own daemon, so tests never touch
// a daemon the user is running on the default port.
export const DEFAULT_DAEMON_URL: string = import.meta.env.WXT_DAEMON_URL || 'ws://127.0.0.1:9876/ws';

// chrome.storage.session holds sessions, connection and log, so they survive a service worker
// restart but not a browser restart. chrome.storage.local holds daemonUrl.
export const KEYS = {
  sessions: 'sessions',
  connection: 'connection',
  log: 'log',
  daemonUrl: 'daemonUrl',
} as const;

export interface SessionState {
  groupId: number | null;
  tabIds: number[];
  borrowedTabIds: number[];
  currentTabId: number | null;
  // Set when the user pressed Cancel on the debugging bar; cleared by navigate or find_tab.
  stopped: boolean;
}

export type Sessions = Record<string, SessionState>;

export interface ConnectionStatus {
  state: 'connecting' | 'connected' | 'disconnected';
  url: string;
  daemonVersion?: string;
  blockedHosts?: string[];
  error?: string;
}

export interface LogEntry {
  at: number;
  session: string;
  action: string;
  selector?: string;
  ms: number;
  code?: string;
}

/** The part of chrome.storage.StorageArea the store classes use; tests pass an in-memory one. */
export interface StorageLike {
  get(key: string): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
}
