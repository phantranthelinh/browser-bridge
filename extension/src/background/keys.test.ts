import { describe, expect, it } from 'vitest';
import { BridgeError } from './errors';
import { keyEvents, parseKey } from './keys';

const summary = (spec: string) => keyEvents(parseKey(spec)).map((e) => `${e.type}:${e.key}:${e.modifiers}${e.text !== undefined ? `:${JSON.stringify(e.text)}` : ''}`);

describe('press_key keys', () => {
  it('types a character with keyDown and text', () => {
    expect(summary('k')).toEqual(['keyDown:k:0:"k"', 'keyUp:k:0']);
    expect(keyEvents(parseKey('k'))[0]).toMatchObject({ code: 'KeyK', windowsVirtualKeyCode: 75 });
    expect(summary('A')).toEqual(['keyDown:A:0:"A"', 'keyUp:A:0']);
  });

  it('sends Enter with \\r so forms submit, and keys that type nothing as rawKeyDown', () => {
    expect(summary('Enter')).toEqual(['keyDown:Enter:0:"\\r"', 'keyUp:Enter:0']);
    expect(summary('ArrowDown')).toEqual(['rawKeyDown:ArrowDown:0', 'keyUp:ArrowDown:0']);
  });

  it('holds modifiers around the key and types nothing with Control', () => {
    expect(summary('Control+A')).toEqual(['rawKeyDown:Control:2', 'rawKeyDown:a:2', 'keyUp:a:2', 'keyUp:Control:0']);
    expect(summary('Control+Shift+Tab')).toEqual([
      'rawKeyDown:Control:2',
      'rawKeyDown:Shift:10',
      'rawKeyDown:Tab:10',
      'keyUp:Tab:10',
      'keyUp:Shift:2',
      'keyUp:Control:0',
    ]);
  });

  it('lets Shift change what a letter types', () => {
    expect(summary('Shift+a')).toEqual(['rawKeyDown:Shift:8', 'keyDown:A:8:"A"', 'keyUp:A:8', 'keyUp:Shift:0']);
  });

  it('accepts common aliases and a plus key', () => {
    expect(parseKey('ctrl+esc')).toMatchObject({ mask: 2, key: { key: 'Escape' } });
    expect(parseKey('Space').key).toMatchObject({ key: ' ', text: ' ' });
    expect(parseKey('Control++').key.key).toBe('+');
    expect(parseKey('+').key.key).toBe('+');
  });

  it('refuses what it cannot press', () => {
    for (const bad of ['Hyper+K', 'NotAKey', 'a+b']) {
      expect(() => parseKey(bad), bad).toThrow(BridgeError);
    }
  });
});
