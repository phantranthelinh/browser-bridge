import type { NetworkRequestDetailResult, NetworkRequestsResult } from '../generated/protocol';
import type { Cdp } from './cdp';
import { BridgeError } from './errors';
import { isBlocked } from './hosts';

export const MAX_REQUESTS = 500;
export const MAX_BODY_BYTES = 10 * 1024 * 1024;

interface Recorded {
  requestId: string;
  url: string;
  method: string;
  headers: Record<string, string>;
  postData?: string;
  hasPostData: boolean;
  status: number;
  mimeType: string;
  responseHeaders?: Record<string, string>;
  completed: boolean;
  bodyBytes?: number;
  failure?: string;
}

interface Capture {
  filter?: string;
  requests: Map<string, Recorded>;
}

/**
 * Records the requests of a tab from network_start on, keeping the newest MAX_REQUESTS. Only the
 * metadata is kept; a body is fetched from Chrome when network_request_detail asks for it.
 */
export class NetworkCapture {
  private tabs = new Map<number, Capture>();

  constructor(
    private readonly cdp: Cdp,
    private readonly blockedHosts: () => readonly string[],
  ) {
    cdp.subscribe({
      event: (tabId, method, params) => this.onEvent(tabId, method, params),
      detached: (tabId) => this.tabs.delete(tabId),
    });
  }

  async start(tabId: number, filter: string | undefined): Promise<void> {
    // Recording before enabling, so the first events already have somewhere to go.
    this.tabs.set(tabId, { filter, requests: new Map() });
    try {
      await this.cdp.send(tabId, 'Network.enable', { maxResourceBufferSize: MAX_BODY_BYTES, maxTotalBufferSize: 20 * MAX_BODY_BYTES });
    } catch (e) {
      this.tabs.delete(tabId);
      throw e;
    }
  }

  async stop(tabId: number): Promise<void> {
    const was = this.tabs.delete(tabId);
    if (was) await this.cdp.send(tabId, 'Network.disable').catch(() => {});
  }

  list(tabId: number, filter: string | undefined): NetworkRequestsResult {
    const capture = this.tabs.get(tabId);
    const requests = [...(capture?.requests.values() ?? [])]
      .filter((r) => !filter || r.url.includes(filter))
      .map(({ requestId, url, method, status, mimeType, completed }) => ({ requestId, url, method, status, mimeType, completed }));
    return { capturing: !!capture, count: requests.length, requests };
  }

  async detail(tabId: number, requestId: string): Promise<NetworkRequestDetailResult> {
    const r = this.tabs.get(tabId)?.requests.get(requestId);
    if (!r) {
      throw new BridgeError('INVALID_REQUEST', `no recorded request ${requestId} in the current tab`, 'List the recorded requests with network_requests');
    }
    const request: NetworkRequestDetailResult['request'] = { url: r.url, method: r.method, headers: r.headers };
    const postData = r.postData ?? (r.hasPostData ? await this.postData(tabId, requestId) : undefined);
    if (postData !== undefined) request.postData = postData;
    const out: NetworkRequestDetailResult = { request, body: '', bodyBase64Encoded: false };
    if (r.responseHeaders) out.response = { status: r.status, headers: r.responseHeaders, mimeType: r.mimeType };

    if (r.failure) out.bodyError = `the request failed: ${r.failure}`;
    else if (!r.completed) out.bodyError = 'the response has not finished loading';
    else if ((r.bodyBytes ?? 0) > MAX_BODY_BYTES) out.bodyError = 'the body is larger than 10 MB';
    else {
      try {
        const { body, base64Encoded } = await this.cdp.send<{ body: string; base64Encoded: boolean }>(tabId, 'Network.getResponseBody', { requestId });
        if ((base64Encoded ? (body.length * 3) / 4 : body.length) > MAX_BODY_BYTES) out.bodyError = 'the body is larger than 10 MB';
        else Object.assign(out, { body, bodyBase64Encoded: base64Encoded });
      } catch (e) {
        out.bodyError = e instanceof Error ? e.message : String(e); // e.g. a redirect, or evicted from Chrome's buffer
      }
    }
    return out;
  }

  private async postData(tabId: number, requestId: string): Promise<string | undefined> {
    try {
      return (await this.cdp.send<{ postData: string }>(tabId, 'Network.getRequestPostData', { requestId })).postData;
    } catch {
      return undefined;
    }
  }

  private onEvent(tabId: number, method: string, p: any): void {
    const capture = this.tabs.get(tabId);
    if (!capture || !method.startsWith('Network.')) return;
    const known = capture.requests.get(p.requestId);
    switch (method) {
      case 'Network.requestWillBeSent': {
        // A page on an allowed host can still call a blocked one, and a blocked page calls
        // others; that traffic stays unseen, including a request that only gets there through a
        // redirect.
        if (isBlocked(p.request.url, this.blockedHosts()) || isBlocked(p.documentURL, this.blockedHosts())) {
          capture.requests.delete(p.requestId);
          return;
        }
        // A redirect reuses the request id: the entry follows it to the new URL.
        if (!known && capture.filter && !p.request.url.includes(capture.filter)) return;
        if (!known && capture.requests.size >= MAX_REQUESTS) capture.requests.delete(capture.requests.keys().next().value!);
        capture.requests.set(p.requestId, {
          requestId: p.requestId,
          url: p.request.url,
          method: p.request.method,
          headers: p.request.headers ?? {},
          postData: p.request.postData,
          hasPostData: !!p.request.hasPostData,
          status: 0,
          mimeType: '',
          completed: false,
        });
        return;
      }
      case 'Network.responseReceived':
        if (!known) return;
        known.status = p.response.status;
        known.mimeType = p.response.mimeType;
        known.responseHeaders = p.response.headers ?? {};
        return;
      case 'Network.loadingFinished':
        if (!known) return;
        known.completed = true;
        known.bodyBytes = p.encodedDataLength;
        return;
      case 'Network.loadingFailed':
        if (!known) return;
        known.completed = true;
        known.failure = p.canceled ? 'canceled' : p.errorText;
    }
  }
}
