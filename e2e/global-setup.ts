import { execFileSync } from 'node:child_process';
import { BRIDGE_BIN, DAEMON_DIR, DAEMON_PORT, EXTENSION_ROOT } from './paths';

// Builds what the tests drive: the daemon binary and the E2E build of the extension.
export default function globalSetup() {
  execFileSync(process.env.GO ?? 'go', ['-C', DAEMON_DIR, 'build', '-o', BRIDGE_BIN, './cmd/bridge'], { stdio: 'inherit' });
  execFileSync('npm run build -- --mode e2e', {
    cwd: EXTENSION_ROOT,
    stdio: 'inherit',
    shell: true,
    env: { ...process.env, WXT_DAEMON_URL: `ws://127.0.0.1:${DAEMON_PORT}/ws` },
  });
}
