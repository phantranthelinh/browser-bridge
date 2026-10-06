import { join } from 'node:path';

const root = join(import.meta.dirname, '..');

// A fixed port that is not the default 9876, so a run never talks to the daemon the user runs.
// The E2E build of the extension has this address compiled in (WXT_DAEMON_URL).
export const DAEMON_PORT = 19876;
export const BRIDGE_BIN = join(root, 'e2e', '.cache', process.platform === 'win32' ? 'bridge.exe' : 'bridge');
export const DAEMON_DIR = join(root, 'daemon');
export const EXTENSION_ROOT = join(root, 'extension');
export const EXTENSION_DIR = join(root, 'extension', '.output', 'chrome-mv3-e2e');
export const TESTPAGE_DIR = join(root, 'testpage');
export const EXTENSION_ID = 'nfjidhefdgblbbfhnmbcogkbphipngif';
