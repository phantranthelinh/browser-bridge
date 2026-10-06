import { describe, expect, it } from 'vitest';
import type { RequestFrame } from '../generated/protocol';
import { BridgeError } from './errors';
import { createRouter, type Ctx, type Handler } from './router';
import { SessionStore } from './sessions';
import { memoryStorage } from './test-support';

function frame(action: string, args: unknown = {}, session = 's1'): RequestFrame {
  return { type: 'request', id: '7', session, action, args, deadline: Date.now() + 1000 };
}

function setup(tabUrls: Record<number, string> = {}) {
  const sessions = new SessionStore(memoryStorage());
  const calls: { action: string; ctx: Ctx; args: unknown }[] = [];
  const ok = (action: string): Handler => async (ctx, args) => {
    calls.push({ action, ctx, args });
    return { action };
  };
  const names = ['navigate', 'find_tab', 'list_tabs', 'close_tab', 'close_session', 'go_back', 'go_forward', 'reload'] as const;
  const handlers: Partial<Record<string, Handler>> = Object.fromEntries(names.map((n) => [n, ok(n)]));
  handlers.snapshot = async () => undefined;
  handlers.click = async () => {
    throw new BridgeError('STALE_REF', '@e1 is gone', 'Take a new snapshot');
  };
  handlers.fill = async () => {
    throw new Error('boom');
  };
  const route = createRouter({
    sessions,
    handlers,
    blockedHosts: () => ['bank.com'],
    tabUrl: async (id) => tabUrls[id],
    makeCtx: (session, deadline) => ({ session, deadline, sessions, blockedHosts: () => ['bank.com'] }),
  });
  return { sessions, calls, route };
}

describe('router', () => {
  it('answers with the handler result and the request id', async () => {
    const { route, calls } = setup();
    const resp = await route(frame('navigate', { url: 'https://a.com' }));
    expect(resp).toEqual({ type: 'response', id: '7', ok: true, data: { action: 'navigate' } });
    expect(calls[0]!.args).toEqual({ url: 'https://a.com' });
    expect(calls[0]!.ctx.session).toBe('s1');
  });

  it('turns an empty result into {}', async () => {
    const { route } = setup();
    expect((await route(frame('snapshot'))).data).toEqual({});
  });

  it('passes BridgeError codes and hints through and wraps anything else as INTERNAL', async () => {
    const { route } = setup();
    expect((await route(frame('click'))).error).toEqual({ code: 'STALE_REF', message: '@e1 is gone', hint: 'Take a new snapshot' });
    expect((await route(frame('fill'))).error).toEqual({ code: 'INTERNAL', message: 'boom' });
  });

  it('reports actions this version does not implement', async () => {
    const { route } = setup();
    const resp = await route(frame('screenshot'));
    expect(resp.ok).toBe(false);
    expect(resp.error?.code).toBe('INTERNAL');
    expect(resp.error?.message).toContain('screenshot');
  });

  it('refuses everything but cleanup, navigate and find_tab after the user pressed Cancel', async () => {
    const { route, sessions } = setup();
    await sessions.update('s1', (s) => {
      s.stopped = true;
    });
    expect((await route(frame('reload'))).error?.code).toBe('DETACHED_BY_USER');
    expect((await route(frame('go_back'))).error?.code).toBe('DETACHED_BY_USER');
    for (const action of ['list_tabs', 'close_tab', 'close_session', 'navigate', 'find_tab']) {
      expect((await route(frame(action))).ok, action).toBe(true);
    }
  });

  it('refuses page actions while the current tab is on a blocked host, but lets the agent leave', async () => {
    const { route, sessions } = setup({ 5: 'https://www.bank.com/accounts' });
    await sessions.update('s1', (s) => {
      s.tabIds = [5];
      s.currentTabId = 5;
    });
    const resp = await route(frame('reload'));
    expect(resp.error?.code).toBe('BLOCKED_HOST');
    expect(JSON.stringify(resp)).not.toContain('bank.com');
    for (const action of ['navigate', 'find_tab', 'go_back', 'go_forward', 'list_tabs', 'close_tab', 'close_session']) {
      expect((await route(frame(action))).ok, action).toBe(true);
    }
  });

  it('does not check hosts when the session has no current tab', async () => {
    const { route } = setup();
    expect((await route(frame('reload'))).ok).toBe(true);
  });
});
