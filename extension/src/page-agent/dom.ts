// DOM helpers of the page agent. Everything here runs in the "bridge" isolated world of the page:
// it sees the page's DOM but none of the page's JavaScript.

export type AgentCode = 'STALE_REF' | 'ELEMENT_NOT_FOUND' | 'AMBIGUOUS_SELECTOR' | 'ELEMENT_NOT_INTERACTABLE' | 'INVALID_REQUEST';

/** An error the service worker passes on to the agent as {code, message, hint}. */
export class AgentError extends Error {
  constructor(
    readonly code: AgentCode,
    message: string,
    readonly hint?: string,
  ) {
    super(message);
  }
}

/**
 * The refs of one document. Numbers come from a per-tab counter the service worker keeps across
 * documents, so a number never comes back for another element: an old ref fails as STALE_REF
 * instead of hitting whatever took its place.
 */
export class Refs {
  next = 1;
  private byElement = new WeakMap<Element, number>();
  private byNumber = new Map<number, WeakRef<Element>>();

  refOf(el: Element): number {
    let n = this.byElement.get(el);
    if (n === undefined) {
      n = this.next++;
      this.byElement.set(el, n);
      this.byNumber.set(n, new WeakRef(el));
    }
    return n;
  }

  element(n: number): Element | undefined {
    const el = this.byNumber.get(n)?.deref();
    return el?.isConnected ? el : undefined;
  }
}

const REF = /^@e(\d+)$/;

/** The element a selector names, or null when it names none (yet). More than one match is an error. */
export function find(refs: Refs, selector: string): Element | null {
  if (selector.startsWith('@e')) {
    const m = REF.exec(selector);
    if (!m) throw new AgentError('INVALID_REQUEST', `${selector} is not a ref: refs look like @e12`);
    return refs.element(Number(m[1])) ?? null;
  }
  let found: NodeListOf<Element>;
  try {
    found = document.querySelectorAll(selector);
  } catch {
    throw new AgentError('INVALID_REQUEST', `${selector} is not a valid CSS selector`, 'Use a ref from snapshot such as @e12, or a valid CSS selector');
  }
  if (found.length > 1) {
    throw new AgentError('AMBIGUOUS_SELECTOR', `${found.length} elements match ${selector}`, 'Use a ref from snapshot, or a more specific selector');
  }
  return found[0] ?? null;
}

export function resolve(refs: Refs, selector: string): Element {
  const el = find(refs, selector);
  if (el) return el;
  if (selector.startsWith('@e')) throw new AgentError('STALE_REF', `${selector} is no longer in the page`, 'Take a new snapshot');
  throw new AgentError('ELEMENT_NOT_FOUND', `no element matches ${selector}`, 'Take a snapshot to see what is on the page');
}

export function hasBox(el: Element): boolean {
  for (const r of el.getClientRects()) if (r.width > 0 && r.height > 0) return true;
  return false;
}

/** Rendered and taking up space: not display:none, not visibility:hidden, not zero-sized. */
export function isVisible(el: Element): boolean {
  return el.isConnected && el.checkVisibility({ visibilityProperty: true }) && hasBox(el);
}

export function isDisabled(el: Element): boolean {
  return el.matches(':disabled') || el.getAttribute('aria-disabled') === 'true';
}

export const collapse = (s: string) => s.replace(/\s+/g, ' ').trim();

export const cut = (s: string, max = 200) => (s.length > max ? s.slice(0, max) + '…' : s);

/** A short description such as <div#overlay.modal "Accept cookies"> for error messages. */
export function describe(el: Element): string {
  let s = el.localName;
  if (el.id) s += '#' + el.id;
  for (const c of [...el.classList].slice(0, 2)) s += '.' + c;
  const text = cut(collapse((el as HTMLElement).innerText ?? el.textContent ?? ''), 40);
  return text ? `<${s} "${text}">` : `<${s}>`;
}

/** Whether b is a or inside it, looking through shadow roots. */
export function composedContains(a: Element, b: Node | null): boolean {
  for (let n = b; n; n = n.parentNode ?? (n instanceof ShadowRoot ? n.host : null)) {
    if (n === a) return true;
  }
  return false;
}

/** The innermost element at a point, looking into open shadow roots. */
export function deepElementFromPoint(x: number, y: number): Element | null {
  let hit = document.elementFromPoint(x, y);
  while (hit?.shadowRoot) {
    const inner = hit.shadowRoot.elementFromPoint(x, y);
    if (!inner || inner === hit) break;
    hit = inner;
  }
  return hit;
}

export function deepActiveElement(): Element | null {
  let a = document.activeElement;
  while (a?.shadowRoot?.activeElement) a = a.shadowRoot.activeElement;
  return a;
}

/** The outermost contenteditable element: the editor itself rather than a paragraph in it. */
export function editableRoot(el: Element): HTMLElement | null {
  if (!(el instanceof HTMLElement) || !el.isContentEditable) return null;
  let root = el;
  while (root.parentElement?.isContentEditable) root = root.parentElement;
  return root;
}

export function scrollToCenter(el: Element): void {
  el.scrollIntoView({ block: 'center', inline: 'center', behavior: 'instant' });
}
