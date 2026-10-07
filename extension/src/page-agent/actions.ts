import {
  AgentError,
  collapse,
  composedContains,
  cut,
  deepActiveElement,
  deepElementFromPoint,
  describe,
  editableRoot,
  find,
  isDisabled,
  isVisible,
  resolve,
  scrollToCenter,
  type Refs,
} from './dom';

export interface Point {
  x: number;
  y: number;
  tag: string;
  text: string;
}

/** Text-like inputs that take typed text; select() works on all of them. */
const TYPED = new Set(['', 'text', 'search', 'url', 'tel', 'email', 'password', 'number']);
/** Inputs whose value is a format rather than text, such as a date picker: set directly. */
const SET_DIRECTLY = new Set(['date', 'time', 'datetime-local', 'month', 'week', 'color', 'range']);

function notInteractable(el: Element, why: string, hint?: string): AgentError {
  return new AgentError('ELEMENT_NOT_INTERACTABLE', `${describe(el)} ${why}`, hint);
}

const BUTTON_TYPES = new Set(['button', 'submit', 'reset']);

/** What the agent is told it clicked. A field's value is never part of it: it may be a password. */
function label(el: Element): string {
  const caption = el instanceof HTMLInputElement && BUTTON_TYPES.has(el.type) ? el.value : (el as HTMLElement).innerText;
  return cut(collapse(caption || el.getAttribute('aria-label') || el.getAttribute('title') || ''), 100);
}

/** A label stands in for a hidden control in the snapshot, so its ref has to reach the control. */
function controlOf(el: Element): Element {
  return el instanceof HTMLLabelElement && el.control ? el.control : el;
}

/**
 * Scrolls the element to the middle of the viewport and returns the point a real mouse click
 * would land on, after checking that the click would reach this element and not something
 * covering it.
 */
export function clickPoint(refs: Refs, selector: string): Point {
  const el = resolve(refs, selector);
  const control = controlOf(el);
  if (control instanceof HTMLInputElement && control.type === 'file') {
    throw notInteractable(el, 'would open the native file chooser in front of the user', 'Use upload with this selector instead of click');
  }
  scrollToCenter(el);
  if (!isVisible(el)) throw notInteractable(el, 'is not visible', 'Take a snapshot: it may be inside a closed menu or a hidden panel');
  if (isDisabled(el)) throw notInteractable(el, 'is disabled');
  const box = el.getBoundingClientRect();
  // An inline element broken over two lines has an empty middle; try each line box as well.
  const candidates = [box, ...el.getClientRects()].map((r) => ({ x: r.left + r.width / 2, y: r.top + r.height / 2 }));
  let blocker: Element | null = null;
  for (const p of candidates) {
    if (p.x < 0 || p.y < 0 || p.x >= innerWidth || p.y >= innerHeight) continue;
    const hit = deepElementFromPoint(p.x, p.y);
    if (composedContains(el, hit) || isLabelOf(hit, el)) return { ...p, tag: el.localName, text: label(el) };
    blocker ??= hit;
  }
  if (!blocker) throw notInteractable(el, 'is outside the visible part of the page', 'Scroll it into view with scroll, or take a snapshot');
  throw notInteractable(el, `is covered by ${describe(blocker)}`, 'Close the popup, dialog or banner in front of it first');
}

/** A styled checkbox is often an invisible input under its label: clicking the label is the way. */
function isLabelOf(hit: Element | null, el: Element): boolean {
  const labels = (el as HTMLInputElement).labels;
  return !!hit && !!labels && [...labels].some((l) => composedContains(l, hit));
}

export interface FillPlan {
  mode: 'value' | 'contenteditable';
  /** The value is already in place; nothing needs typing. */
  done: boolean;
}

/**
 * Focuses a text field and selects its whole content, so the text typed next replaces it. Fields
 * that take no typing, such as a date picker, get the value here.
 */
export function prepareFill(refs: Refs, selector: string, value: string): FillPlan {
  const el = controlOf(resolve(refs, selector));
  scrollToCenter(el);
  if (el instanceof HTMLSelectElement) throw notInteractable(el, 'is a <select>', 'Use select to pick an option');
  if (el instanceof HTMLInputElement && !TYPED.has(el.type) && !SET_DIRECTLY.has(el.type)) {
    const hint = el.type === 'file' ? 'Use upload' : el.type === 'checkbox' || el.type === 'radio' ? 'Use click to change it' : undefined;
    throw notInteractable(el, `is an input of type ${el.type}, which takes no text`, hint);
  }
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    if (isDisabled(el)) throw notInteractable(el, 'is disabled');
    if (el.readOnly) throw notInteractable(el, 'is read-only');
    if (el instanceof HTMLInputElement && SET_DIRECTLY.has(el.type)) {
      setNativeValue(el, value);
      return { mode: 'value', done: true };
    }
    el.focus();
    el.select();
    return { mode: 'value', done: false };
  }
  const root = editableRoot(el);
  if (!root) throw notInteractable(el, 'is not a text field', 'Take a snapshot and fill a textbox');
  root.focus();
  getSelection()?.selectAllChildren(root);
  return { mode: 'contenteditable', done: false };
}

// Frameworks such as React wrap the value property to track changes. Their wrapper lives in the
// page's world, not this one, so this assignment goes around it and the events read as a change.
function setNativeValue(el: HTMLInputElement, value: string): void {
  el.value = value;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
}

/** Whether an editor holds the text, ignoring the whitespace and line breaks editors add. */
export function hasText(refs: Refs, selector: string, value: string): boolean {
  const el = controlOf(resolve(refs, selector));
  const root = editableRoot(el) ?? el;
  const strip = (s: string) => s.replace(/\s+/g, '');
  return strip((root as HTMLElement).innerText ?? root.textContent ?? '') === strip(value);
}

export function selectOption(refs: Refs, selector: string, want: { value?: string; label?: string }): { selected: { value: string; label: string } } {
  const el = controlOf(resolve(refs, selector));
  if (!(el instanceof HTMLSelectElement)) {
    throw notInteractable(el, 'is not a <select>', 'For a custom dropdown, click it, then click the option in a new snapshot');
  }
  if (isDisabled(el)) throw notInteractable(el, 'is disabled');
  const options = [...el.options];
  const option = want.value !== undefined
    ? options.find((o) => o.value === want.value)
    : options.find((o) => collapse(o.label) === collapse(want.label ?? ''));
  if (!option) {
    const which = want.value !== undefined ? `value "${want.value}"` : `label "${want.label}"`;
    const listed = options.slice(0, 20).map((o) => `"${collapse(o.label)}"`).join(', ');
    throw new AgentError('ELEMENT_NOT_FOUND', `${describe(el)} has no option with ${which}`, `Its options: ${listed}`);
  }
  if (option.disabled) throw notInteractable(option, 'is disabled');
  option.selected = true;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  el.dispatchEvent(new Event('change', { bubbles: true }));
  return { selected: { value: option.value, label: collapse(option.label) } };
}

export function focus(refs: Refs, selector: string): void {
  const el = resolve(refs, selector);
  scrollToCenter(el);
  (el as HTMLElement).focus?.();
  if (!composedContains(el, deepActiveElement())) {
    throw notInteractable(el, 'cannot take the keyboard focus', 'Click it instead, or call press_key without a selector');
  }
}

export interface ScrollArgs {
  selector?: string;
  direction?: 'up' | 'down' | 'left' | 'right';
  amount?: number;
}

/**
 * Scrolls an element into view, or scrolls the page. Many apps scroll an inner panel rather than
 * the page; when the page does not move, the scrollable panel in the middle of the view does.
 */
export function scroll(refs: Refs, args: ScrollArgs): { scrollX: number; scrollY: number } {
  if (args.selector) {
    scrollToCenter(resolve(refs, args.selector));
    return { scrollX: scrollX, scrollY: scrollY };
  }
  const amount = args.amount ?? 600;
  const dx = args.direction === 'left' ? -amount : args.direction === 'right' ? amount : 0;
  const dy = args.direction === 'up' ? -amount : args.direction === 'down' ? amount : 0;
  const before = { x: scrollX, y: scrollY };
  scrollBy({ left: dx, top: dy, behavior: 'instant' });
  if (scrollX !== before.x || scrollY !== before.y) return { scrollX, scrollY };
  for (let el = deepElementFromPoint(innerWidth / 2, innerHeight / 2); el; el = el.parentElement ?? shadowHost(el)) {
    if (!canScroll(el, dx, dy)) continue;
    el.scrollBy({ left: dx, top: dy, behavior: 'instant' });
    return { scrollX: el.scrollLeft, scrollY: el.scrollTop };
  }
  return { scrollX, scrollY };
}

function shadowHost(el: Element): Element | null {
  const root = el.getRootNode();
  return root instanceof ShadowRoot ? root.host : null;
}

function canScroll(el: Element, dx: number, dy: number): boolean {
  const style = getComputedStyle(el);
  const scrolls = (overflow: string) => overflow === 'auto' || overflow === 'scroll' || overflow === 'overlay';
  return (dy !== 0 && scrolls(style.overflowY) && el.scrollHeight > el.clientHeight) ||
    (dx !== 0 && scrolls(style.overflowX) && el.scrollWidth > el.clientWidth);
}

export interface Clip {
  x: number;
  y: number;
  width: number;
  height: number;
  /** The element fits in the viewport, so the capture needs no resize. */
  fits: boolean;
}

/** The element's box in page coordinates, which is what Page.captureScreenshot clips by. */
export function clip(refs: Refs, selector: string): Clip {
  const el = resolve(refs, selector);
  scrollToCenter(el);
  if (!isVisible(el)) throw notInteractable(el, 'is not visible, so there is nothing to capture');
  const r = el.getBoundingClientRect();
  return { x: r.left + scrollX, y: r.top + scrollY, width: r.width, height: r.height, fits: r.width <= innerWidth && r.height <= innerHeight };
}

/** Checks the element is a file input; the service worker then picks it up as a remote object. */
export function fileInput(refs: Refs, selector: string, count: number): Element {
  const el = controlOf(resolve(refs, selector));
  if (!(el instanceof HTMLInputElement) || el.type !== 'file') {
    throw notInteractable(el, 'is not an <input type=file>', 'Take a snapshot and upload to a fileinput');
  }
  if (isDisabled(el)) throw notInteractable(el, 'is disabled');
  if (count > 1 && !el.multiple) throw new AgentError('INVALID_REQUEST', `${describe(el)} takes one file, not ${count}`);
  return el;
}

export interface WaitCondition {
  selector?: string;
  state?: 'visible' | 'hidden';
  text?: string;
}

/** One check of a wait_for selector or text; the service worker polls it, and watches URLs and loading itself. */
export function check(refs: Refs, c: WaitCondition): boolean {
  if (c.selector !== undefined) {
    // Not there (yet) is a normal state here, not an error; only an ambiguous selector throws.
    const el = find(refs, c.selector);
    const visible = !!el && isVisible(el);
    return (c.state ?? 'visible') === 'visible' ? visible : !visible;
  }
  return (document.body?.innerText ?? '').includes(c.text ?? '');
}
