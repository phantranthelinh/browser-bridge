import type { StorageLike } from '../shared/state';

/** An in-memory chrome.storage area. Values are cloned like the real one, so tests catch aliasing bugs. */
export function memoryStorage(): StorageLike & { data: Record<string, unknown> } {
  const data: Record<string, unknown> = {};
  return {
    data,
    async get(key) {
      return key in data ? { [key]: structuredClone(data[key]) } : {};
    },
    async set(items) {
      for (const [k, v] of Object.entries(items)) data[k] = structuredClone(v);
    },
  };
}
