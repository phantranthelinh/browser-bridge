import { describe, expect, it } from 'vitest';
import { RefCounters } from './refs';
import { memoryStorage } from './test-support';

describe('RefCounters', () => {
  it('starts every tab at 1 and only moves forward', async () => {
    const refs = new RefCounters(memoryStorage());
    expect(await refs.next(7)).toBe(1);
    await refs.advance(7, 40);
    await refs.advance(7, 12); // a slower snapshot finishing late must not hand numbers out again
    expect(await refs.next(7)).toBe(40);
    expect(await refs.next(8)).toBe(1);
  });

  it('survives a restart through storage, and forgets closed tabs', async () => {
    const area = memoryStorage();
    await new RefCounters(area).advance(7, 40);
    const restarted = new RefCounters(area);
    expect(await restarted.next(7)).toBe(40);
    await restarted.forget(7);
    expect(await restarted.next(7)).toBe(1);
  });

  it('drops the counters of tabs that no longer exist', async () => {
    const refs = new RefCounters(memoryStorage());
    await refs.advance(7, 40);
    await refs.advance(8, 9);
    await refs.keepOnly([8, 99]);
    expect(await refs.next(7)).toBe(1);
    expect(await refs.next(8)).toBe(9);
  });
});
