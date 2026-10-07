import { describe, expect, it } from 'vitest';
import type { Cdp, CdpListener } from './cdp';
import { BridgeError } from './errors';
import { MAX_BODY_BYTES, MAX_REQUESTS, NetworkCapture } from './network';

function setup(bodies: Record<string, { body: string; base64Encoded: boolean }> = {}) {
  const listeners: CdpListener[] = [];
  const sent: string[] = [];
  const cdp = {
    subscribe: (l: CdpListener) => listeners.push(l),
    send: async (_tabId: number, method: string, params?: { requestId?: string }) => {
      sent.push(method);
      if (method === 'Network.getResponseBody') {
        const b = bodies[params!.requestId!];
        if (!b) throw new BridgeError('CDP_ERROR', 'Network.getResponseBody: No resource with given identifier found');
        return b;
      }
      return {};
    },
  } as unknown as Cdp;
  const capture = new NetworkCapture(cdp, () => ['bank.com']);
  const emit = (method: string, params: object, tabId = 1) => listeners.forEach((l) => l.event?.(tabId, method, params));
  const request = (id: string, url: string, extra: object = {}) =>
    emit('Network.requestWillBeSent', { requestId: id, request: { url, method: 'GET', headers: { Accept: '*/*' }, ...extra } });
  const finish = (id: string, status = 200, bytes = 10) => {
    emit('Network.responseReceived', { requestId: id, response: { status, mimeType: 'application/json', headers: { 'Content-Type': 'application/json' } } });
    emit('Network.loadingFinished', { requestId: id, encodedDataLength: bytes });
  };
  return { capture, emit, request, finish, sent, detach: (tabId: number) => listeners.forEach((l) => l.detached?.(tabId)) };
}

describe('NetworkCapture', () => {
  it('records nothing before network_start', async () => {
    const { capture, request } = setup();
    request('1', 'https://a.com/api');
    expect(capture.list(1, undefined)).toEqual({ capturing: false, count: 0, requests: [] });
  });

  it('records the requests of its own tab, filtered by URL', async () => {
    const { capture, request, finish } = setup();
    await capture.start(1, '/api/');
    request('1', 'https://a.com/api/users');
    request('2', 'https://a.com/logo.png');
    request('3', 'https://a.com/api/other', {});
    finish('1');
    expect(capture.list(1, undefined)).toEqual({
      capturing: true,
      count: 2,
      requests: [
        { requestId: '1', url: 'https://a.com/api/users', method: 'GET', status: 200, mimeType: 'application/json', completed: true },
        { requestId: '3', url: 'https://a.com/api/other', method: 'GET', status: 0, mimeType: '', completed: false },
      ],
    });
    expect(capture.list(1, 'users').count).toBe(1);
    expect(capture.list(2, undefined).capturing).toBe(false);
  });

  it('never records traffic to a blocked host, even through a redirect', async () => {
    const { capture, request } = setup();
    await capture.start(1, undefined);
    request('1', 'https://api.bank.com/accounts');
    request('2', 'https://a.com/go');
    request('2', 'https://www.bank.com/login'); // the redirect of request 2
    request('3', 'https://a.com/ok');
    expect(capture.list(1, undefined).requests.map((r) => r.requestId)).toEqual(['3']);
    await expect(capture.detail(1, '2')).rejects.toMatchObject({ code: 'INVALID_REQUEST' });
  });

  it('keeps only the newest requests', async () => {
    const { capture, request } = setup();
    await capture.start(1, undefined);
    for (let i = 0; i < MAX_REQUESTS + 5; i++) request(String(i), `https://a.com/${i}`);
    const { count, requests } = capture.list(1, undefined);
    expect(count).toBe(MAX_REQUESTS);
    expect(requests[0]!.requestId).toBe('5');
  });

  it('fetches the body on demand and explains when there is none', async () => {
    const { capture, request, finish, emit } = setup({ '1': { body: '{"ok":true}', base64Encoded: false } });
    await capture.start(1, undefined);
    request('1', 'https://a.com/api', { method: 'POST', postData: 'q=1', hasPostData: true });
    finish('1');
    expect(await capture.detail(1, '1')).toEqual({
      request: { url: 'https://a.com/api', method: 'POST', headers: { Accept: '*/*' }, postData: 'q=1' },
      response: { status: 200, headers: { 'Content-Type': 'application/json' }, mimeType: 'application/json' },
      body: '{"ok":true}',
      bodyBase64Encoded: false,
    });

    request('2', 'https://a.com/slow');
    expect((await capture.detail(1, '2')).bodyError).toContain('not finished');
    request('3', 'https://a.com/big');
    finish('3', 200, MAX_BODY_BYTES + 1);
    expect((await capture.detail(1, '3')).bodyError).toContain('10 MB');
    request('4', 'https://a.com/down');
    emit('Network.loadingFailed', { requestId: '4', errorText: 'net::ERR_CONNECTION_REFUSED' });
    expect((await capture.detail(1, '4')).bodyError).toContain('ERR_CONNECTION_REFUSED');

    await expect(capture.detail(1, 'nope')).rejects.toMatchObject({ code: 'INVALID_REQUEST' });
  });

  it('stops and forgets on network_stop and when the tab goes away', async () => {
    const { capture, request, sent, detach } = setup();
    await capture.start(1, undefined);
    request('1', 'https://a.com/');
    await capture.stop(1);
    expect(sent).toEqual(['Network.enable', 'Network.disable']);
    expect(capture.list(1, undefined).capturing).toBe(false);

    await capture.start(1, undefined);
    detach(1);
    expect(capture.list(1, undefined).capturing).toBe(false);
  });
});
