import { computeAccessibleName, getRole } from 'dom-accessibility-api';
import { collapse, cut, editableRoot, hasBox, isDisabled, type Refs } from './dom';

export interface SnapshotOptions {
  maxChars: number;
  /** First ref number this tab has not used yet, from the service worker. */
  start: number;
}

export interface FrameInfo {
  frame: number;
  url: string;
  width: number;
  height: number;
}

export interface Snapshot {
  url: string;
  title: string;
  tree: string;
  frames: FrameInfo[];
  truncated: boolean;
  /** The tab's ref counter after this snapshot, for the service worker to keep. */
  next: number;
}

interface Line {
  role: string;
  name: string;
  states: string[];
  ref?: number;
  children: Item[];
}

type Item = { text: string } | Line;

interface Visited {
  items: Item[];
  /** Set when the element is inline and only holds text, so it joins the text around it. */
  inline: string | null;
}

// Inline and empty, so a skipped element neither adds text nor splits the text around it.
const NOTHING: Visited = { items: [], inline: '' };

const SKIP = new Set(['script', 'style', 'noscript', 'template', 'head', 'meta', 'link', 'title']);

// Roles an agent acts on. Each gets a ref.
const INTERACTIVE = new Set([
  'button', 'link', 'checkbox', 'radio', 'switch', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'option',
  'combobox', 'textbox', 'searchbox', 'slider', 'spinbutton', 'treeitem', 'listbox',
]);

// Roles that structure the page. They get a line, without a ref.
const STRUCTURE = new Set([
  'heading', 'list', 'listitem', 'navigation', 'main', 'banner', 'contentinfo', 'complementary', 'form', 'search',
  'dialog', 'alertdialog', 'alert', 'status', 'table', 'grid', 'treegrid', 'row', 'cell', 'gridcell', 'columnheader',
  'rowheader', 'img', 'figure', 'article', 'tablist', 'tabpanel', 'menu', 'menubar', 'toolbar', 'tree', 'progressbar',
  'meter', 'region', 'group',
]);

// Roles whose name is their text. Without interactive elements inside, the text is shown as the
// name and not repeated below it.
const NAMED_BY_CONTENT = new Set([
  'button', 'link', 'heading', 'tab', 'menuitem', 'menuitemcheckbox', 'menuitemradio', 'option', 'treeitem',
  'checkbox', 'radio', 'switch', 'cell', 'gridcell', 'columnheader', 'rowheader', 'clickable',
]);

const INTERACTIVE_INSIDE = [
  'a[href]', 'button', 'input:not([type=hidden])', 'select', 'textarea', '[contenteditable]:not([contenteditable=false])',
  '[tabindex]:not([tabindex^="-"])', ...[...INTERACTIVE].map((r) => `[role=${r}]`),
].join(',');

const NO_VALUE_TYPES = new Set(['password', 'checkbox', 'radio', 'button', 'submit', 'reset', 'image', 'file', 'hidden']);

const esc = (s: string) => s.replace(/\\/g, '\\\\').replace(/"/g, '\\"');

/**
 * Reads the page as an accessibility tree, one line per element that matters to an agent:
 * `- <role> "<name>" [state] @e<n>`, indented two spaces per level. Elements that only group
 * others are left out and their content moves up a level. Only interactive elements get a ref.
 */
export function snapshot(refs: Refs, opts: SnapshotOptions): Snapshot {
  refs.next = Math.max(refs.next, opts.start);
  const walker = new Walker(refs, opts.maxChars);
  const root = document.body ?? document.documentElement;
  const items = root ? walker.children(root, false, true) : [];
  const lines: string[] = [];
  render(items, 0, lines);
  let tree = '';
  let truncated = walker.full;
  for (const line of lines) {
    if (tree.length + line.length + 1 > opts.maxChars) {
      truncated = true;
      break;
    }
    tree += line + '\n';
  }
  return { url: location.href, title: document.title, tree, frames: walker.frames, truncated, next: refs.next };
}

function render(items: Item[], depth: number, out: string[]): void {
  const pad = '  '.repeat(depth);
  for (const it of items) {
    if ('text' in it) {
      out.push(`${pad}- text "${esc(cut(it.text))}"`);
      continue;
    }
    let name = it.name;
    let children = it.children;
    // "- listitem" over a lone "- text" line reads better as one line.
    if (!name && children.length === 1 && 'text' in children[0]!) {
      name = children[0].text;
      children = [];
    }
    let head = `${pad}- ${it.role}`;
    if (name) head += ` "${esc(cut(name))}"`;
    for (const s of it.states) head += ` [${s}]`;
    if (it.ref !== undefined) head += ` @e${it.ref}`;
    out.push(head);
    render(children, depth + 1, out);
  }
}

class Walker {
  frames: FrameInfo[] = [];
  /** Set once the output is surely longer than maxChars: the rest of the page is not walked. */
  full = false;
  private size = 0;

  constructor(
    private readonly refs: Refs,
    private readonly maxChars: number,
  ) {}

  /** The items of an element's children, in the flat tree (open shadow roots and slots resolved). */
  children(el: Element, pointer: boolean, textVisible: boolean): Item[] {
    const items: Item[] = [];
    let run = '';
    const flush = () => {
      const text = collapse(run);
      run = '';
      if (text) this.add(items, { text });
    };
    for (const child of flatChildren(el)) {
      if (this.full) break;
      if (child.nodeType === Node.TEXT_NODE) {
        if (textVisible) run += (child as Text).data;
        continue;
      }
      if (child.nodeType !== Node.ELEMENT_NODE) continue;
      const v = this.visit(child as Element, pointer);
      if (v.inline !== null) {
        run += v.inline;
        continue;
      }
      flush();
      items.push(...v.items);
    }
    flush();
    return items;
  }

  private add(items: Item[], item: Item): void {
    items.push(item);
    // What render prints: names and text are cut at 200 characters there.
    this.size += 'text' in item ? Math.min(item.text.length, 201) + 12 : item.role.length + Math.min(item.name.length, 201) + 16;
    if (this.size > this.maxChars * 1.2) this.full = true;
  }

  private visit(el: Element, parentPointer: boolean): Visited {
    const tag = el.localName;
    if (SKIP.has(tag) || el.getAttribute('aria-hidden') === 'true') return NOTHING;
    if (tag === 'br') return { items: [], inline: ' ' };
    const style = getComputedStyle(el);
    if (style.display === 'none') return NOTHING;
    // No box at all: closed <details>, content-visibility:hidden. display:contents has no box
    // either but its children do.
    if (style.display !== 'contents' && !el.checkVisibility()) return NOTHING;
    if (tag === 'iframe' || tag === 'frame') return this.frame(el);
    if (tag === 'svg') return this.svg(el);

    const shown = style.visibility === 'visible' && hasBox(el);
    const pointer = style.cursor === 'pointer';
    const editable = editableRoot(el) === el;
    let role = roleOf(el, editable);
    const interactive = shown && isInteractive(el, role, editable, pointer && !parentPointer);
    if (interactive && (role === null || !(INTERACTIVE.has(role) || STRUCTURE.has(role) || role === 'fileinput'))) role = 'clickable';

    if (tag === 'label' && shown) {
      const control = (el as HTMLLabelElement).control;
      // A label that holds nothing to click but its control.
      if (control && el.querySelectorAll(INTERACTIVE_INSIDE).length === (el.contains(control) ? 1 : 0)) {
        // The control's line is named by this label, so the label's own text would repeat it.
        if (control.checkVisibility({ visibilityProperty: true }) && hasBox(control)) {
          return el.contains(control) ? { items: this.visit(control, pointer).items, inline: null } : NOTHING;
        }
        // A styled checkbox hides its input and shows the label: the label is what to click.
        return this.standIn(el as HTMLLabelElement, control);
      }
    }

    if (!shown || role === null || !(interactive || STRUCTURE.has(role))) {
      const items = this.children(el, pointer, style.visibility === 'visible');
      const inline = isInline(style.display) && items.every((i) => 'text' in i) ? items.map((i) => (i as { text: string }).text).join(' ') : null;
      return { items, inline };
    }

    const leaf = isLeaf(el, role, interactive);
    const name = nameOf(el, role, leaf);
    if ((role === 'region' || role === 'group' || role === 'img') && !name && !interactive) {
      return { items: this.children(el, pointer, true), inline: null };
    }
    const line: Line = { role, name, states: statesOf(el, role), children: [] };
    if (interactive) line.ref = this.refs.refOf(el);
    const items: Item[] = [];
    this.add(items, line);
    if (tag === 'select') line.children = options(el as HTMLSelectElement);
    else if (!leaf) line.children = this.children(el, pointer, true);
    return { items, inline: null };
  }

  private standIn(label: HTMLLabelElement, control: HTMLElement): Visited {
    const role = roleOf(control, false) ?? 'clickable';
    const items: Item[] = [];
    this.add(items, { role, name: collapse(label.innerText), states: statesOf(control, role), ref: this.refs.refOf(label), children: [] });
    return { items, inline: null };
  }

  private frame(el: Element): Visited {
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return NOTHING;
    const index = this.frames.length;
    this.frames.push({ frame: index, url: frameUrl(el as HTMLIFrameElement), width: Math.round(r.width), height: Math.round(r.height) });
    const items: Item[] = [];
    this.add(items, { role: 'iframe', name: collapse(el.getAttribute('title') ?? ''), states: [`frame=${index}`], children: [] });
    return { items, inline: null };
  }

  private svg(el: Element): Visited {
    const name = collapse(el.getAttribute('aria-label') ?? el.querySelector(':scope > title')?.textContent ?? '');
    if (!name || !hasBox(el)) return NOTHING;
    const items: Item[] = [];
    this.add(items, { role: 'img', name, states: [], children: [] });
    return { items, inline: null };
  }
}

function* flatChildren(el: Element): Iterable<Node> {
  if (el.shadowRoot) {
    yield* el.shadowRoot.childNodes;
    return;
  }
  if (el instanceof HTMLSlotElement) {
    const assigned = el.assignedNodes({ flatten: true });
    yield* assigned.length > 0 ? assigned : el.childNodes;
    return;
  }
  yield* el.childNodes;
}

const isInline = (display: string) => display.startsWith('inline') || display === 'contents';

// Input types the ARIA mapping leaves without a role.
const INPUT_ROLES: Record<string, string> = {
  password: 'textbox', date: 'textbox', time: 'textbox', 'datetime-local': 'textbox', month: 'textbox', week: 'textbox',
  color: 'button',
  // Not a button: clicking it would open the native file chooser. upload is the way in.
  file: 'fileinput',
};

function roleOf(el: Element, editable: boolean): string | null {
  if (el instanceof HTMLInputElement && el.type in INPUT_ROLES) return INPUT_ROLES[el.type]!;
  return getRole(el) ?? (editable ? 'textbox' : null);
}

function isInteractive(el: Element, role: string | null, editable: boolean, pointer: boolean): boolean {
  if (role !== null && INTERACTIVE.has(role)) return role !== 'option' || !el.closest('select');
  const tag = el.localName;
  if (tag === 'a' ? el.hasAttribute('href') : tag === 'button' || tag === 'select' || tag === 'textarea' || tag === 'summary') return true;
  if (tag === 'input') return (el as HTMLInputElement).type !== 'hidden';
  if (editable || el.hasAttribute('onclick')) return true;
  const tabindex = el.getAttribute('tabindex');
  if (tabindex !== null && Number(tabindex) >= 0) return true;
  // Sites build buttons out of divs; the pointer cursor is what tells them apart. Only the
  // element that sets it counts, not the children that inherit it.
  return pointer;
}

/** A leaf shows its text as its name and nothing below it. */
function isLeaf(el: Element, role: string, interactive: boolean): boolean {
  if (el.localName === 'select') return true;
  if (interactive && !NAMED_BY_CONTENT.has(role)) return true; // text fields, sliders
  return NAMED_BY_CONTENT.has(role) && el.querySelector(INTERACTIVE_INSIDE) === null;
}

function nameOf(el: Element, role: string, leaf: boolean): string {
  // Below a non-leaf the text is listed anyway; only a name the author gave is worth showing. A
  // row is named by its cells' text, which its cells repeat.
  if ((NAMED_BY_CONTENT.has(role) && !leaf) || role === 'row') {
    return collapse(el.getAttribute('aria-label') ?? el.getAttribute('title') ?? '');
  }
  let name = '';
  try {
    name = collapse(computeAccessibleName(el));
  } catch {
    // a malformed aria-labelledby and the like: fall back below
  }
  if (!name && role === 'clickable') name = collapse((el as HTMLElement).innerText ?? '');
  if (!name) name = collapse(el.getAttribute('placeholder') ?? '');
  return name;
}

function statesOf(el: Element, role: string): string[] {
  const s: string[] = [];
  if (role === 'heading') {
    const level = /^h([1-6])$/.exec(el.localName)?.[1] ?? el.getAttribute('aria-level');
    if (level) s.push(`level=${level}`);
  }
  const checked = el instanceof HTMLInputElement && (el.type === 'checkbox' || el.type === 'radio')
    ? (el.indeterminate ? 'mixed' : String(el.checked))
    : el.getAttribute('aria-checked');
  if (checked === 'true') s.push('checked');
  else if (checked === 'mixed') s.push('checked=mixed');
  if (isDisabled(el)) s.push('disabled');
  const expanded = el.getAttribute('aria-expanded');
  if (expanded === 'true') s.push('expanded');
  else if (expanded === 'false') s.push('expanded=false');
  if (el.getAttribute('aria-selected') === 'true') s.push('selected');
  const value = valueOf(el);
  if (value) s.push(`value="${esc(cut(collapse(value)))}"`);
  return s;
}

function valueOf(el: Element): string {
  // A password never reaches the agent, whatever it asks for. Some sites mask a plain text field
  // with -webkit-text-security instead of using type=password.
  if (el instanceof HTMLInputElement || el instanceof HTMLTextAreaElement) {
    const masked = getComputedStyle(el).getPropertyValue('-webkit-text-security');
    if ((masked && masked !== 'none') || (el instanceof HTMLInputElement && NO_VALUE_TYPES.has(el.type))) return '';
    return el.value;
  }
  if (el instanceof HTMLSelectElement) return [...el.selectedOptions].map((o) => o.label).join(', ');
  if (editableRoot(el) === el) return (el as HTMLElement).innerText;
  return el.getAttribute('aria-valuetext') ?? el.getAttribute('aria-valuenow') ?? '';
}

const MAX_OPTIONS = 30;

function options(select: HTMLSelectElement): Item[] {
  const items: Item[] = [...select.options].slice(0, MAX_OPTIONS).map((o) => ({
    role: 'option',
    name: collapse(o.label),
    states: [...(o.selected ? ['selected'] : []), ...(o.disabled ? ['disabled'] : [])],
    children: [],
  }));
  if (select.options.length > MAX_OPTIONS) items.push({ text: `… ${select.options.length - MAX_OPTIONS} more options` });
  return items;
}

function frameUrl(frame: HTMLIFrameElement): string {
  try {
    const href = frame.contentWindow?.location.href;
    if (href) return href; // same origin only; a cross-origin frame throws
  } catch {
    // fall back to the attribute
  }
  return frame.src || (frame.hasAttribute('srcdoc') ? 'about:srcdoc' : 'about:blank');
}
