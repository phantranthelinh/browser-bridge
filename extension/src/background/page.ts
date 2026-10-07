import { browser } from 'wxt/browser';
import type { ErrorBody } from '../generated/protocol';
import type { AgentReply } from '../page-agent';
import type { Cdp } from './cdp';
import { BridgeError, blockedHost } from './errors';
import { isBlocked } from './hosts';

const WORLD = 'bridge';

/** The page is between documents: the world we held is gone and a new one has to be made. */
export function isLostContext(e: unknown): boolean {
  return (
    e instanceof BridgeError &&
    e.code === 'CDP_ERROR' &&
    /Cannot find context|uniqueContextId not found|context was destroyed|Inspected target navigated|No frame for given id/i.test(e.message)
  );
}

interface World {
  id: number;
  /** How Runtime.evaluate names the world: by uniqueContextId when Chrome reported one. */
  target: { uniqueContextId: string } | { contextId: number };
}

function exceptionText(details: any): string {
  return details?.exception?.description ?? details?.text ?? 'unknown exception';
}

/**
 * Runs the page agent (src/page-agent) in an isolated world of each tab's main frame. The world
 * is made and the agent injected on first use in a document; a new document needs a new world,
 * which the next call makes.
 */
export class PageAgentClient {
  private worlds = new Map<number, World>();
  private making = new Map<number, Promise<World>>();
  private created = new Map<number, { id: number; uniqueId?: string }>();
  private source: Promise<string> | null = null;

  constructor(
    private readonly cdp: Cdp,
    private readonly blockedHosts: () => readonly string[],
  ) {
    cdp.subscribe({
      event: (tabId, method, params) => {
        if (method === 'Runtime.executionContextCreated' && params.context.name === WORLD) {
          this.created.set(tabId, { id: params.context.id, uniqueId: params.context.uniqueId });
        } else if (method === 'Runtime.executionContextsCleared') {
          this.worlds.delete(tabId);
        } else if (method === 'Runtime.executionContextDestroyed' && this.worlds.get(tabId)?.id === params.executionContextId) {
          this.worlds.delete(tabId);
        } else if (method === 'Page.frameNavigated' && !params.frame.parentId) {
          this.worlds.delete(tabId);
        }
      },
      detached: (tabId) => {
        this.worlds.delete(tabId);
        this.created.delete(tabId);
      },
    });
  }

  /** Calls a page agent method. Its errors arrive as BridgeErrors with the agent's code. */
  async call<T>(tabId: number, method: string, ...args: unknown[]): Promise<T> {
    const expression = `globalThis.__bridge.call(${JSON.stringify(method)}, ${JSON.stringify(args)})`;
    const reply = (await this.evaluate(tabId, expression, true)).value as AgentReply<T>;
    if ('error' in reply) throw new BridgeError(reply.error.code as ErrorBody['code'], reply.error.message, reply.error.hint);
    return reply.value;
  }

  /** Calls a page agent method that picks an element, and returns that element's CDP objectId. */
  async element(tabId: number, method: string, ...args: unknown[]): Promise<string> {
    await this.call(tabId, method, ...args);
    const r = await this.evaluate(tabId, 'globalThis.__bridge.take()', false);
    if (!r.objectId) throw new BridgeError('INTERNAL', `the page agent lost the element ${method} picked`);
    return r.objectId;
  }

  private async evaluate(tabId: number, expression: string, byValue: boolean): Promise<{ value?: unknown; objectId?: string }> {
    for (let attempt = 0; ; attempt++) {
      try {
        const world = await this.world(tabId);
        const r = await this.cdp.send(tabId, 'Runtime.evaluate', { ...world.target, expression, returnByValue: byValue });
        if (r.exceptionDetails) throw new BridgeError('INTERNAL', `page agent: ${exceptionText(r.exceptionDetails)}`);
        return r.result;
      } catch (e) {
        // The page moved on to another document while the world was being found, made or used.
        if (attempt > 0 || !isLostContext(e)) throw e;
        this.worlds.delete(tabId);
      }
    }
  }

  private world(tabId: number): Promise<World> {
    const known = this.worlds.get(tabId);
    if (known !== undefined) return Promise.resolve(known);
    let p = this.making.get(tabId);
    if (!p) {
      p = this.makeWorld(tabId).finally(() => this.making.delete(tabId));
      this.making.set(tabId, p);
    }
    return p;
  }

  private async makeWorld(tabId: number): Promise<World> {
    const { frameTree } = await this.cdp.send(tabId, 'Page.getFrameTree');
    // The router checked the tab's URL, but a navigation may have committed since: a new world is
    // a new document, so it gets the same check before anything reads it.
    if (isBlocked(frameTree.frame.url, this.blockedHosts())) throw blockedHost();
    const { executionContextId: id } = await this.cdp.send(tabId, 'Page.createIsolatedWorld', { frameId: frameTree.frame.id, worldName: WORLD });
    // Context ids are reused across renderer processes, so after a cross-site navigation a stale
    // id could name a context of the new page. The unique id cannot.
    const uniqueId = this.created.get(tabId)?.id === id ? this.created.get(tabId)?.uniqueId : undefined;
    const world: World = { id, target: uniqueId ? { uniqueContextId: uniqueId } : { contextId: id } };
    const r = await this.cdp.send(tabId, 'Runtime.evaluate', { ...world.target, expression: await this.code(), returnByValue: true });
    if (r.exceptionDetails) throw new BridgeError('INTERNAL', `cannot start the page agent: ${exceptionText(r.exceptionDetails)}`);
    this.worlds.set(tabId, world);
    return world;
  }

  private code(): Promise<string> {
    this.source ??= fetch(browser.runtime.getURL('/page-agent.js'))
      .then((r) => {
        if (!r.ok) throw new Error(`page-agent.js: HTTP ${r.status}`);
        return r.text();
      })
      .catch((e) => {
        this.source = null; // let the next command try again
        throw new BridgeError('INTERNAL', `cannot load the page agent: ${e instanceof Error ? e.message : e}`);
      });
    return this.source;
  }
}
