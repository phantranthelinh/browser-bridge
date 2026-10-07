import { check, clickPoint, clip, fileInput, focus, hasText, prepareFill, scroll, selectOption } from './actions';
import { AgentError, Refs } from './dom';
import { snapshot } from './snapshot';

/** What the service worker reads back from every call: a value or an error, never a throw. */
export type AgentReply<T = unknown> = { value: T } | { error: { code: string; message: string; hint?: string } };

export interface PageAgent {
  call(method: string, args: unknown[]): AgentReply;
  /** Hands over the element the last fileInput call checked, for CDP to use as a remote object. */
  take(): Element | null;
}

/**
 * Installs the page agent as globalThis.__bridge in the isolated world it runs in, once per
 * document. The refs live here, out of reach of the page's own scripts.
 */
export function install(): void {
  const g = globalThis as { __bridge?: PageAgent };
  if (g.__bridge) return;
  const refs = new Refs();
  let held: Element | null = null;
  const methods: Record<string, (...args: any[]) => unknown> = {
    snapshot: (opts) => snapshot(refs, opts),
    clickPoint: (selector) => clickPoint(refs, selector),
    prepareFill: (selector, value) => prepareFill(refs, selector, value),
    hasText: (selector, value) => hasText(refs, selector, value),
    select: (selector, want) => selectOption(refs, selector, want),
    focus: (selector) => focus(refs, selector),
    scroll: (args) => scroll(refs, args),
    clip: (selector) => clip(refs, selector),
    check: (cond) => check(refs, cond),
    fileInput: (selector, count) => {
      held = fileInput(refs, selector, count);
    },
  };
  g.__bridge = {
    call(method, args) {
      try {
        const fn = methods[method];
        if (!fn) throw new Error(`the page agent has no method ${method}`);
        return { value: fn(...args) ?? null };
      } catch (e) {
        if (e instanceof AgentError) return { error: { code: e.code, message: e.message, hint: e.hint } };
        return { error: { code: 'INTERNAL', message: e instanceof Error ? e.message : String(e) } };
      }
    },
    take() {
      const el = held;
      held = null;
      return el;
    },
  };
}
