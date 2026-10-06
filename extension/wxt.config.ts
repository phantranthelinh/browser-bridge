import { readFileSync } from 'node:fs';
import { defineConfig } from 'wxt';

export default defineConfig({
  srcDir: 'src',
  modules: ['@wxt-dev/module-react'],
  manifest: {
    name: 'Browser Bridge',
    description: 'Lets local agents drive this browser through the bridge daemon.',
    // The public key pins the extension ID across unpacked loads; the daemon accepts that ID by default.
    key: readFileSync('manifest-key.txt', 'utf8').trim(),
    permissions: ['debugger', 'tabs', 'tabGroups', 'storage', 'sidePanel', 'alarms'],
    action: { default_title: 'Browser Bridge' },
  },
});
