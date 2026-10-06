import { EXTENSION_ID } from '../paths';
import { expect, ok, test } from '../fixtures';

test('the side panel shows the connection, the session and the last command', async ({ bridge, browserCtx, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  const panel = await browserCtx.newPage();
  await panel.goto(`chrome-extension://${EXTENSION_ID}/sidepanel.html`);
  await expect(panel.getByTestId('connection-state')).toHaveText('connected');
  await expect(panel.getByText(session).first()).toBeVisible();
  await expect(panel.locator('tr').first()).toContainText('navigate');
  await panel.close();
});
