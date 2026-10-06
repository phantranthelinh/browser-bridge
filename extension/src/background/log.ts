import { KEYS, type LogEntry, type StorageLike } from '../shared/state';

/** The side panel's list of recent commands, newest first. Only the selector of the args is kept. */
export class CommandLog {
  private chain: Promise<void> = Promise.resolve();

  constructor(
    private readonly area: StorageLike,
    private readonly max = 50,
  ) {}

  add(entry: LogEntry): Promise<void> {
    this.chain = this.chain
      .then(async () => {
        const current = ((await this.area.get(KEYS.log))[KEYS.log] as LogEntry[] | undefined) ?? [];
        await this.area.set({ [KEYS.log]: [entry, ...current].slice(0, this.max) });
      })
      .catch(() => {});
    return this.chain;
  }
}
