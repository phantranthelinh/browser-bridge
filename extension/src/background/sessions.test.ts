import { describe, expect, it } from 'vitest';
import { SessionStore } from './sessions';
import { memoryStorage } from './test-support';

describe('SessionStore', () => {
  it('returns an empty session for an unknown name without storing it', async () => {
    const area = memoryStorage();
    const store = new SessionStore(area);
    expect(await store.get('a')).toEqual({ groupId: null, tabIds: [], borrowedTabIds: [], currentTabId: null, stopped: false });
    expect(area.data).toEqual({});
  });

  it('persists updates', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => {
      s.tabIds.push(7);
      s.currentTabId = 7;
    });
    expect((await store.get('a')).currentTabId).toBe(7);
  });

  it('loses no update when many run at once', async () => {
    const store = new SessionStore(memoryStorage());
    await Promise.all(
      Array.from({ length: 20 }, (_, i) =>
        store.update(`s${i % 4}`, (s) => {
          s.tabIds.push(i);
        }),
      ),
    );
    const all = await store.all();
    expect(Object.values(all).flatMap((s) => s.tabIds).sort((a, b) => a - b)).toEqual(Array.from({ length: 20 }, (_, i) => i));
  });

  it('forgets a closed tab in every session that held it', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => {
      s.tabIds = [1, 2];
      s.currentTabId = 1;
    });
    await store.update('b', (s) => {
      s.borrowedTabIds = [1];
      s.currentTabId = 1;
    });
    await store.update('c', (s) => {
      s.tabIds = [3];
      s.currentTabId = 3;
    });
    expect((await store.forgetTab(1)).sort()).toEqual(['a', 'b']);
    expect(await store.get('a')).toMatchObject({ tabIds: [2], currentTabId: null });
    expect(await store.get('b')).toMatchObject({ borrowedTabIds: [], currentTabId: null });
    expect(await store.get('c')).toMatchObject({ tabIds: [3], currentTabId: 3 });
  });

  it('stops only the sessions holding the cancelled tab', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => void s.tabIds.push(1));
    await store.update('b', (s) => void s.tabIds.push(2));
    expect(await store.stopSessionsOf(1)).toEqual(['a']);
    expect((await store.get('a')).stopped).toBe(true);
    expect((await store.get('b')).stopped).toBe(false);
  });

  it('removes a session', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => void s.tabIds.push(1));
    await store.remove('a');
    expect(await store.all()).toEqual({});
  });
});
