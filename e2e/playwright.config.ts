import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  globalSetup: './global-setup.ts',
  // One daemon on a fixed port and one browser per run.
  workers: 1,
  fullyParallel: false,
  timeout: 60_000,
  reporter: 'list',
});
