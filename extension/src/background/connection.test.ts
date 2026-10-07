import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { RequestFrame, ResponseFrame, Welcome } from '../generated/protocol';
import type { ConnectionStatus } from '../shared/state';
import { backoffMs, Connection, PING_INTERVAL_MS, type SocketLike } from './connection';

class FakeSocket implements SocketLike {
  readyState = 0;
  sent: string[] = [];
  onopen: SocketLike['onopen'] = null;
  onmessage: SocketLike['onmessage'] = null;
  onclose: SocketLike['onclose'] = null;
  constructor(readonly url: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(frame: object) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
  drop(code: number) {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}

const welcome: Welcome = { type: 'welcome', protocolVersion: 2, daemonVersion: '0.1.0', blockedHosts: ['bank.com'] };

function setup(onRequest = async (f: RequestFrame): Promise<ResponseFrame> => ({ type: 'response', id: f.id, ok: true, data: {} })) {
  const sockets: FakeSocket[] = [];
  const statuses: ConnectionStatus[] = [];
  const welcomes: Welcome[] = [];
  const conn = new Connection({
    url: async () => 'ws://127.0.0.1:9876/ws',
    hello: () => ({ type: 'hello', protocolVersion: 2, extensionVersion: '0.1.0', extensionId: 'x', browser: 'chrome', actions: ['navigate'] }),
    onWelcome: (w) => welcomes.push(w),
    onRequest,
    onStatus: (s) => statuses.push(s),
    openSocket: (url) => {
      const s = new FakeSocket(url);
      sockets.push(s);
      return s;
    },
  });
  return { conn, sockets, statuses, welcomes };
}

describe('backoffMs', () => {
  it('doubles from 1s and stops at 30s', () => {
    expect([0, 1, 2, 4, 5, 10].map(backoffMs)).toEqual([1000, 2000, 4000, 16000, 30000, 30000]);
  });
});

describe('Connection', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('says hello on open and is connected after welcome', async () => {
    const { conn, sockets, statuses, welcomes } = setup();
    await conn.connect();
    sockets[0]!.open();
    expect(JSON.parse(sockets[0]!.sent[0]!)).toMatchObject({ type: 'hello', protocolVersion: 2, actions: ['navigate'] });
    expect(conn.connected).toBe(false);
    sockets[0]!.receive(welcome);
    expect(conn.connected).toBe(true);
    expect(welcomes).toEqual([welcome]);
    expect(statuses.at(-1)).toEqual({ state: 'connected', url: 'ws://127.0.0.1:9876/ws', daemonVersion: '0.1.0', blockedHosts: ['bank.com'] });
  });

  it('pings every 20 seconds once connected', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    vi.advanceTimersByTime(PING_INTERVAL_MS * 2);
    expect(sockets[0]!.sent.filter((s) => s === '{"type":"ping"}')).toHaveLength(2);
  });

  it('answers requests on the same socket', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    sockets[0]!.receive({ type: 'request', id: '42', session: 's', action: 'list_tabs', args: {}, deadline: 0 });
    await vi.waitFor(() => expect(sockets[0]!.sent.some((s) => s.includes('"id":"42"'))).toBe(true));
  });

  it('drops the answer when the socket closed while the command ran', async () => {
    let finish!: (r: ResponseFrame) => void;
    const { conn, sockets } = setup(() => new Promise((r) => (finish = r)));
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    sockets[0]!.receive({ type: 'request', id: '1', session: 's', action: 'reload', args: {}, deadline: 0 });
    sockets[0]!.drop(1006);
    finish({ type: 'response', id: '1', ok: true, data: {} });
    await Promise.resolve();
    expect(sockets[0]!.sent.some((s) => s.includes('"response"'))).toBe(false);
  });

  it('explains refusals and reconnects with growing delays', async () => {
    const { conn, sockets, statuses } = setup();
    await conn.connect();
    sockets[0]!.drop(4409);
    expect(statuses.at(-1)).toMatchObject({ state: 'disconnected' });
    expect(statuses.at(-1)!.error).toContain('already connected');
    await vi.advanceTimersByTimeAsync(999);
    expect(sockets).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(2);
    sockets[1]!.drop(1006);
    expect(statuses.at(-1)!.error).toBe('cannot reach the daemon');
    await vi.advanceTimersByTimeAsync(1999);
    expect(sockets).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(3);
    // a welcome resets the delay
    sockets[2]!.open();
    sockets[2]!.receive(welcome);
    sockets[2]!.drop(1006);
    await vi.advanceTimersByTimeAsync(1000);
    expect(sockets).toHaveLength(4);
  });

  it('names a protocol version mismatch', async () => {
    const { conn, sockets, statuses } = setup();
    await conn.connect();
    sockets[0]!.drop(4400);
    expect(statuses.at(-1)!.error).toContain('protocol version');
  });

  it('opens one socket however often connect is called', async () => {
    const { conn, sockets } = setup();
    await Promise.all([conn.connect(), conn.connect(), conn.connect()]);
    expect(sockets).toHaveLength(1);
  });

  it('reconnect replaces the socket at once and ignores the old one closing', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    conn.reconnect();
    await vi.waitFor(() => expect(sockets).toHaveLength(2));
    sockets[0]!.drop(1000);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sockets).toHaveLength(2);
  });
});
