import { BridgeError } from './errors';

interface KeyDef {
  key: string;
  code: string;
  keyCode: number;
  /** What the key types; keys such as Tab and arrows type nothing. */
  text?: string;
  location?: number;
}

/** One press of press_key, e.g. Control+A: modifiers held down around one key. */
export interface Stroke {
  modifiers: KeyDef[];
  key: KeyDef;
  /** CDP bit mask: Alt 1, Control 2, Meta 4, Shift 8. */
  mask: number;
}

const MODIFIERS: Record<string, { def: KeyDef; bit: number }> = {
  Alt: { def: { key: 'Alt', code: 'AltLeft', keyCode: 18, location: 1 }, bit: 1 },
  Control: { def: { key: 'Control', code: 'ControlLeft', keyCode: 17, location: 1 }, bit: 2 },
  Meta: { def: { key: 'Meta', code: 'MetaLeft', keyCode: 91, location: 1 }, bit: 4 },
  Shift: { def: { key: 'Shift', code: 'ShiftLeft', keyCode: 16, location: 1 }, bit: 8 },
};

const NAMED: Record<string, KeyDef> = {
  // Enter types \r so that the keypress which submits a form happens.
  Enter: { key: 'Enter', code: 'Enter', keyCode: 13, text: '\r' },
  Tab: { key: 'Tab', code: 'Tab', keyCode: 9 },
  Escape: { key: 'Escape', code: 'Escape', keyCode: 27 },
  Backspace: { key: 'Backspace', code: 'Backspace', keyCode: 8 },
  Delete: { key: 'Delete', code: 'Delete', keyCode: 46 },
  Insert: { key: 'Insert', code: 'Insert', keyCode: 45 },
  Space: { key: ' ', code: 'Space', keyCode: 32, text: ' ' },
  ArrowUp: { key: 'ArrowUp', code: 'ArrowUp', keyCode: 38 },
  ArrowDown: { key: 'ArrowDown', code: 'ArrowDown', keyCode: 40 },
  ArrowLeft: { key: 'ArrowLeft', code: 'ArrowLeft', keyCode: 37 },
  ArrowRight: { key: 'ArrowRight', code: 'ArrowRight', keyCode: 39 },
  Home: { key: 'Home', code: 'Home', keyCode: 36 },
  End: { key: 'End', code: 'End', keyCode: 35 },
  PageUp: { key: 'PageUp', code: 'PageUp', keyCode: 33 },
  PageDown: { key: 'PageDown', code: 'PageDown', keyCode: 34 },
  ...Object.fromEntries(Array.from({ length: 12 }, (_, i) => [`F${i + 1}`, { key: `F${i + 1}`, code: `F${i + 1}`, keyCode: 112 + i }])),
};

const ALIASES: Record<string, string> = {
  esc: 'Escape', return: 'Enter', del: 'Delete', ins: 'Insert', spacebar: 'Space', ' ': 'Space',
  up: 'ArrowUp', down: 'ArrowDown', left: 'ArrowLeft', right: 'ArrowRight', pgup: 'PageUp', pgdn: 'PageDown',
  ctrl: 'Control', control: 'Control', cmd: 'Meta', command: 'Meta', meta: 'Meta', win: 'Meta', super: 'Meta',
  alt: 'Alt', option: 'Alt', shift: 'Shift',
};

// US layout: the code and key code of each punctuation key, unshifted and shifted.
const PUNCTUATION: Record<string, [string, number]> = {
  '-': ['Minus', 189], '_': ['Minus', 189], '=': ['Equal', 187], '+': ['Equal', 187],
  '[': ['BracketLeft', 219], '{': ['BracketLeft', 219], ']': ['BracketRight', 221], '}': ['BracketRight', 221],
  '\\': ['Backslash', 220], '|': ['Backslash', 220], ';': ['Semicolon', 186], ':': ['Semicolon', 186],
  "'": ['Quote', 222], '"': ['Quote', 222], ',': ['Comma', 188], '<': ['Comma', 188], '.': ['Period', 190],
  '>': ['Period', 190], '/': ['Slash', 191], '?': ['Slash', 191], '`': ['Backquote', 192], '~': ['Backquote', 192],
};

function canonical(name: string): string {
  const alias = ALIASES[name.toLowerCase()];
  if (alias) return alias;
  const named = Object.keys(NAMED).find((k) => k.toLowerCase() === name.toLowerCase());
  return named ?? name;
}

function keyDef(name: string, mask: number): KeyDef | null {
  const named = NAMED[canonical(name)];
  if (named) return named;
  if ([...name].length !== 1) return null;
  if (/^[a-z]$/i.test(name)) {
    // A lone "A" types an A; in a combination, Shift decides: Control+A is Control+a.
    const ch = mask === 0 ? name : mask & 8 ? name.toUpperCase() : name.toLowerCase();
    return { key: ch, code: `Key${name.toUpperCase()}`, keyCode: name.toUpperCase().charCodeAt(0), text: ch };
  }
  if (/^[0-9]$/.test(name)) return { key: name, code: `Digit${name}`, keyCode: name.charCodeAt(0), text: name };
  const p = PUNCTUATION[name];
  if (p) return { key: name, code: p[0], keyCode: p[1], text: name };
  return { key: name, code: '', keyCode: 0, text: name }; // any other character, e.g. é
}

/** Parses Enter, k, Control+A, Shift+Tab, Control++ ... */
export function parseKey(spec: string): Stroke {
  const parts = spec === '+' ? ['+'] : spec.endsWith('++') ? [...spec.slice(0, -2).split('+'), '+'] : spec.split('+');
  const last = parts.pop()!;
  const modifiers: KeyDef[] = [];
  let mask = 0;
  for (const part of parts) {
    const m = MODIFIERS[canonical(part)];
    if (!m) throw new BridgeError('INVALID_REQUEST', `${part} in ${spec} is not a modifier`, 'Combine keys like Control+A or Shift+Tab: modifiers are Control, Shift, Alt and Meta');
    modifiers.push(m.def);
    mask |= m.bit;
  }
  const lone = MODIFIERS[canonical(last)];
  const key = lone ? lone.def : keyDef(last, mask);
  if (!key) throw new BridgeError('INVALID_REQUEST', `unknown key ${last}`, 'Use a key name such as Enter, Escape, Tab, ArrowDown, F5, or a single character');
  // Control+A selects all; it must not also type an "a".
  const typing = key.text !== undefined && (mask & ~8) === 0;
  return { modifiers, key: typing ? key : { ...key, text: undefined }, mask };
}

/** The Input.dispatchKeyEvent calls for one stroke, in order. */
export function keyEvents(stroke: Stroke): Record<string, unknown>[] {
  const event = (type: string, def: KeyDef, modifiers: number) => ({
    type,
    modifiers,
    key: def.key,
    code: def.code,
    windowsVirtualKeyCode: def.keyCode,
    nativeVirtualKeyCode: def.keyCode,
    ...(def.location !== undefined && { location: def.location }),
    ...(type === 'keyDown' && { text: def.text, unmodifiedText: def.text }),
  });
  const events: Record<string, unknown>[] = [];
  let held = 0;
  for (const m of stroke.modifiers) {
    held |= MODIFIERS[m.key]!.bit;
    events.push(event('rawKeyDown', m, held));
  }
  // keyDown with text also fires keypress and input; rawKeyDown is a key that types nothing.
  events.push(event(stroke.key.text !== undefined ? 'keyDown' : 'rawKeyDown', stroke.key, stroke.mask));
  events.push(event('keyUp', stroke.key, stroke.mask));
  for (const m of [...stroke.modifiers].reverse()) {
    held &= ~MODIFIERS[m.key]!.bit;
    events.push(event('keyUp', m, held));
  }
  return events;
}
