import type { Hello, RequestFrame, ResponseFrame, Welcome } from '../generated/protocol';
import type { ConnectionStatus } from '../shared/state';

export const PING_INTERVAL_MS = 20_000;

/** 1s, 2s, 4s ... capped at 30s. */
export function backoffMs(attempt: number): number {
  return Math.min(30_000, 1000 * 2 ** attempt);
}

/** The slice of WebSocket the connection uses, so tests can drive it without a server. */
export interface SocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(): void;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
}

export interface ConnectionOptions {
  url(): Promise<string>;
  hello(): Hello;
  onWelcome(w: Welcome): void;
  onRequest(f: RequestFrame): Promise<ResponseFrame>;
  onStatus(s: ConnectionStatus): void;
  openSocket?(url: string): SocketLike;
}

const OPEN = 1;

const closeReasons: Record<number, string> = {
  4400: 'the daemon speaks another protocol version: update the extension or the daemon',
  4409: 'another Browser Bridge extension is already connected to the daemon',
};

/**
 * The single WebSocket to the daemon: hello/welcome handshake, a ping every 20s (an active socket
 * keeps the service worker alive), and reconnection with backoff after every close.
 */
export class Connection {
  private socket: SocketLike | null = null;
  private attempt = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(private readonly opts: ConnectionOptions) {}

  get connected(): boolean {
    return this.pingTimer !== null;
  }

  /** Connects unless a socket is already open or opening. Safe to call from alarms. */
  async connect(): Promise<void> {
    if (this.socket) return;
    this.clearRetry();
    const url = await this.opts.url();
    if (this.socket) return; // another connect() won while the URL was being read
    this.opts.onStatus({ state: 'connecting', url });
    const socket = (this.opts.openSocket ?? ((u) => new WebSocket(u) as unknown as SocketLike))(url);
    this.socket = socket;
    socket.onopen = () => socket.send(JSON.stringify(this.opts.hello()));
    socket.onmessage = (ev) => this.onMessage(socket, url, ev.data);
    socket.onclose = (ev) => this.onClose(socket, url, ev.code);
  }

  /** Drops the current socket and connects again at once, e.g. after the daemon URL changed. */
  reconnect(): void {
    const old = this.socket;
    this.socket = null;
    this.stopPing();
    this.attempt = 0;
    old?.close();
    void this.connect();
  }

  private onMessage(socket: SocketLike, url: string, raw: unknown): void {
    let frame: { type?: string };
    try {
      frame = JSON.parse(String(raw));
    } catch {
      return;
    }
    if (frame.type === 'welcome') {
      const w = frame as Welcome;
      this.attempt = 0;
      this.opts.onWelcome(w);
      this.opts.onStatus({ state: 'connected', url, daemonVersion: w.daemonVersion, blockedHosts: w.blockedHosts });
      this.stopPing();
      this.pingTimer = setInterval(() => socket.send('{"type":"ping"}'), PING_INTERVAL_MS);
    } else if (frame.type === 'request') {
      void this.opts.onRequest(frame as RequestFrame).then((resp) => {
        if (socket.readyState === OPEN) socket.send(JSON.stringify(resp));
      });
    }
  }

  private onClose(socket: SocketLike, url: string, code: number): void {
    if (this.socket !== socket) return; // replaced by reconnect()
    this.socket = null;
    this.stopPing();
    this.opts.onStatus({ state: 'disconnected', url, error: closeReasons[code] ?? (code === 1006 ? 'cannot reach the daemon' : undefined) });
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.connect();
    }, backoffMs(this.attempt++));
  }

  private stopPing(): void {
    if (this.pingTimer !== null) clearInterval(this.pingTimer);
    this.pingTimer = null;
  }

  private clearRetry(): void {
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    this.retryTimer = null;
  }
}
