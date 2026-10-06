import { describe, expect, it } from 'vitest';
import { KEYS, type LogEntry } from '../shared/state';
import { CommandLog } from './log';
import { memoryStorage } from './test-support';

describe('CommandLog', () => {
  it('keeps the newest entries first, up to the limit', async () => {
    const area = memoryStorage();
    const log = new CommandLog(area, 3);
    await Promise.all([1, 2, 3, 4, 5].map((i) => log.add({ at: i, session: 's', action: 'click', ms: 1 })));
    expect((area.data[KEYS.log] as LogEntry[]).map((e) => e.at)).toEqual([5, 4, 3]);
  });
});
