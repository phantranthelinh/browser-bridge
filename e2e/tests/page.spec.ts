import { existsSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { type Bridge, expect, ok, tabInfo, test } from '../fixtures';

/** The ref on the snapshot line that matches, e.g. refOf(tree, 'button "Go"') gives "@e12". */
function refOf(tree: string, line: string): string {
  const found = tree.split('\n').find((l) => l.includes(line));
  const ref = found?.match(/@e\d+/)?.[0];
  if (!ref) throw new Error(`no ref on a line with ${line} in:\n${tree}`);
  return ref;
}

async function open(bridge: Bridge, session: string, url: string) {
  await ok(bridge.command(session, 'navigate', { url }));
}

async function read(bridge: Bridge, session: string, selector: string): Promise<string> {
  const r = await ok(bridge.command(session, 'evaluate', { code: `document.querySelector(${JSON.stringify(selector)}).textContent` }));
  return r.value;
}

async function valueOf(bridge: Bridge, session: string, selector: string): Promise<string> {
  return (await ok(bridge.command(session, 'evaluate', { code: `document.querySelector(${JSON.stringify(selector)}).value` }))).value;
}

test.describe('page actions', () => {
  test.beforeEach(async ({ bridge, session, site }) => {
    await open(bridge, session, site.url('/actions.html'));
  });

  test('snapshot reads the page as a tree with refs on what can be used', async ({ bridge, session, site }) => {
    const snap = await ok(bridge.command(session, 'snapshot'));
    expect(snap).toMatchObject({ url: site.url('/actions.html'), title: 'Bridge actions page', truncated: false });
    const tree: string = snap.tree;
    expect(tree).toContain('- heading "Actions page" [level=1]');
    expect(tree).toContain('- text "Some bold text in a paragraph."');
    // the spaces at the edges of inline elements separate words; where there are none, nothing is added
    expect(tree).toContain('- text "Runs of styled words,joined"');
    // a link wrapped around a link that says the same is one line
    expect(tree).toMatch(/- link "Nested menu" @e\d+\n(?! +- link "Nested menu")/);
    expect(tree).toMatch(/- textbox "Name" @e\d+/);
    expect(tree).toMatch(/- combobox "Color" \[value="Red"\] @e\d+/);
    expect(tree).toContain('- option "Green"');
    expect(tree).toMatch(/- checkbox "I agree" @e\d+/);
    expect(tree).toMatch(/- button "Go" @e\d+/);
    expect(tree).toMatch(/- clickable "Clickable card" @e\d+/);
    expect(tree).toMatch(/- fileinput "Attachment" @e\d+/);
    expect(tree).toContain('- iframe "Inner page" [frame=0]');
    // a row is not named after all its cells: the cells say it once
    expect(tree).toContain('- row\n');
    expect(tree).toContain('- cell "Basic"');
    // long paragraphs are cut per line, and do not crowd out the rest of the page
    expect(tree).toContain('- text "Paragraph 30. Lorem');
    expect(tree).toMatch(/- button "Far away" @e\d+/);
    expect(snap.frames).toEqual([{ frame: 0, url: site.url('/page2.html'), width: 304, height: 104 }]); // 300x100 plus the default 2px border
    // hidden content stays out, and a password never comes back
    expect(tree).not.toContain('Secret hidden');
    expect(tree).not.toContain('Decoration only');
    expect(tree).not.toContain('hunter2');
    expect(tree).toMatch(/- textbox "Password" @e\d+/);
    expect(tree).toMatch(/- textbox "Day" @e\d+/);
    // a label's text is the name of its control, not a line of its own
    expect(tree).not.toContain('- text "Name"');

    const again = await ok(bridge.command(session, 'snapshot'));
    expect(refOf(again.tree, 'button "Go"')).toBe(refOf(tree, 'button "Go"'));
  });

  test('a file input hidden behind its label takes uploads through the label, and is never clicked', async ({ bridge, session }) => {
    const resume = refOf((await ok(bridge.command(session, 'snapshot'))).tree, 'fileinput "Resume"');
    const file = join(mkdtempSync(join(tmpdir(), 'bridge-upload-')), 'cv.pdf');
    writeFileSync(file, '%PDF');
    expect(await ok(bridge.command(session, 'upload', { selector: resume, files: [file] }))).toEqual({ fileCount: 1 });
    expect(await read(bridge, session, '#resume-result')).toBe('cv.pdf');
    const clicked = await bridge.command(session, 'click', { selector: resume });
    expect(clicked.error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
    expect(clicked.error?.hint).toContain('upload');
  });

  test('click never reports what is typed in a field', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'click', { selector: '#pw' }))).toEqual({ tag: 'input', text: '' });
  });

  test('a checkbox styled through its label is shown, and clicked, as the label', async ({ bridge, session }) => {
    const notify = refOf((await ok(bridge.command(session, 'snapshot'))).tree, 'checkbox "Notify me"');
    expect(await ok(bridge.command(session, 'click', { selector: notify }))).toMatchObject({ tag: 'label' });
    expect((await ok(bridge.command(session, 'snapshot'))).tree).toContain(`- checkbox "Notify me" [checked] ${notify}`);
  });

  test('snapshot stops at maxChars', async ({ bridge, session }) => {
    const snap = await ok(bridge.command(session, 'snapshot', { maxChars: 200 }));
    expect(snap.truncated).toBe(true);
    expect(snap.tree.length).toBeLessThanOrEqual(200);
  });

  test('click by ref and by CSS selector, and the errors of each', async ({ bridge, session }) => {
    const tree = (await ok(bridge.command(session, 'snapshot'))).tree;
    expect(await ok(bridge.command(session, 'click', { selector: refOf(tree, 'button "Go"') }))).toEqual({ tag: 'button', text: 'Go' });
    await ok(bridge.command(session, 'click', { selector: '#go' }));
    expect(await read(bridge, session, '#result')).toBe('clicked 2');

    await ok(bridge.command(session, 'click', { selector: refOf(tree, 'clickable "Clickable card"') }));
    expect(await read(bridge, session, '#result')).toBe('card');
    await ok(bridge.command(session, 'click', { selector: '#far' })); // below the fold
    expect(await read(bridge, session, '#result')).toBe('far');

    expect((await bridge.command(session, 'click', { selector: '.dup' })).error?.code).toBe('AMBIGUOUS_SELECTOR');
    expect((await bridge.command(session, 'click', { selector: '#nope' })).error?.code).toBe('ELEMENT_NOT_FOUND');
    expect((await bridge.command(session, 'click', { selector: 'button[' })).error?.code).toBe('INVALID_REQUEST');
    const covered = await bridge.command(session, 'click', { selector: '#covered' });
    expect(covered.error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
    expect(covered.error?.message).toContain('overlay');

    const doomed = refOf(tree, 'button "Doomed"');
    await ok(bridge.command(session, 'click', { selector: '#remove' }));
    expect((await bridge.command(session, 'click', { selector: doomed })).error?.code).toBe('STALE_REF');
  });

  test('a ref from an earlier document is stale, never another element', async ({ bridge, session, site }) => {
    const go = refOf((await ok(bridge.command(session, 'snapshot'))).tree, 'button "Go"');
    await open(bridge, session, site.url('/actions.html'));
    const fresh = (await ok(bridge.command(session, 'snapshot'))).tree;
    expect(refOf(fresh, 'button "Go"')).not.toBe(go);
    expect((await bridge.command(session, 'click', { selector: go })).error?.code).toBe('STALE_REF');
  });

  test('fill types into inputs, textareas and rich editors, and clears them', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'fill', { selector: '#name', value: 'xin chào' }))).toEqual({ mode: 'value' });
    expect(await valueOf(bridge, session, '#name')).toBe('xin chào');
    await ok(bridge.command(session, 'fill', { selector: '#name', value: 'replaced' }));
    expect(await valueOf(bridge, session, '#name')).toBe('replaced');
    await ok(bridge.command(session, 'fill', { selector: '#name', value: '' }));
    expect(await valueOf(bridge, session, '#name')).toBe('');

    await ok(bridge.command(session, 'fill', { selector: '#bio', value: 'line one\nline two' }));
    expect(await valueOf(bridge, session, '#bio')).toBe('line one\nline two');
    expect(await ok(bridge.command(session, 'fill', { selector: '#editor', value: 'new text' }))).toEqual({ mode: 'contenteditable' });
    expect(await read(bridge, session, '#editor')).toBe('new text');
    await ok(bridge.command(session, 'fill', { selector: '#date', value: '2026-10-07' }));
    expect(await valueOf(bridge, session, '#date')).toBe('2026-10-07');

    expect((await bridge.command(session, 'fill', { selector: '#agree', value: 'x' })).error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
    expect((await bridge.command(session, 'fill', { selector: '#color', value: 'x' })).error?.hint).toContain('select');
  });

  test('select picks an option by value or label', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'select', { selector: '#color', value: 'g' }))).toEqual({ selected: { value: 'g', label: 'Green' } });
    expect(await read(bridge, session, '#color-result')).toBe('color g');
    expect(await ok(bridge.command(session, 'select', { selector: '#color', label: 'Blue' }))).toEqual({ selected: { value: 'b', label: 'Blue' } });
    // the page asks to confirm: the agent hears about the dialog, not a timeout
    const confirming = await bridge.command(session, 'select', { selector: '#size', label: 'Huge' }, 5000);
    expect(confirming.error?.code).toBe('DIALOG_OPEN');
    expect(confirming.error?.hint).toContain('Huge costs extra');
    await ok(bridge.command(session, 'handle_dialog', { accept: true }));
    expect(await valueOf(bridge, session, '#size')).toBe('Huge');

    const missing = await bridge.command(session, 'select', { selector: '#color', label: 'Purple' });
    expect(missing.error?.code).toBe('ELEMENT_NOT_FOUND');
    expect(missing.error?.hint).toContain('"Red"');
    expect((await bridge.command(session, 'select', { selector: '#name', value: 'x' })).error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
  });

  test('press_key sends real keys, with modifiers', async ({ bridge, session }) => {
    await ok(bridge.command(session, 'fill', { selector: '#q', value: 'hello' }));
    expect(await ok(bridge.command(session, 'press_key', { key: 'Enter', selector: '#q' }))).toEqual({});
    expect(await read(bridge, session, '#search-result')).toBe('submitted hello');

    await ok(bridge.command(session, 'press_key', { key: 'Control+A', selector: '#q' }));
    await ok(bridge.command(session, 'press_key', { key: 'Backspace' }));
    expect(await valueOf(bridge, session, '#q')).toBe('');

    await ok(bridge.command(session, 'press_key', { key: 'k', selector: '#keys' }));
    await ok(bridge.command(session, 'press_key', { key: 'Shift+Tab' }));
    expect(await read(bridge, session, '#key-log')).toBe('k;Shift;Tab;');
    expect(await valueOf(bridge, session, '#keys')).toBe('k');

    expect((await bridge.command(session, 'press_key', { key: 'Hyper+K' })).error?.code).toBe('INVALID_REQUEST');
    expect((await bridge.command(session, 'press_key', { key: 'Enter', selector: '#result' })).error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
  });

  test('scroll moves the page or brings an element into view', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'scroll', { direction: 'down', amount: 300 }))).toEqual({ scrollX: 0, scrollY: 300 });
    expect((await ok(bridge.command(session, 'scroll', { selector: '#far' }))).scrollY).toBeGreaterThan(1000);
    expect((await ok(bridge.command(session, 'scroll', { direction: 'up' }))).scrollY).toBeGreaterThan(0);
    expect((await bridge.command(session, 'scroll', { selector: '#nope' })).error?.code).toBe('ELEMENT_NOT_FOUND');
  });

  test('upload sets the files of a file input', async ({ bridge, session }) => {
    const file = join(mkdtempSync(join(tmpdir(), 'bridge-upload-')), 'resume.txt');
    writeFileSync(file, 'hello');
    expect(await ok(bridge.command(session, 'upload', { selector: '#file', files: [file] }))).toEqual({ fileCount: 1 });
    expect(await read(bridge, session, '#file-result')).toBe('resume.txt');
    expect((await bridge.command(session, 'upload', { selector: '#name', files: [file] })).error?.code).toBe('ELEMENT_NOT_INTERACTABLE');
    expect((await bridge.command(session, 'upload', { selector: '#file', files: [file, file] })).error?.code).toBe('INVALID_REQUEST');
    expect((await bridge.command(session, 'upload', { selector: '#file', files: [file + '.missing'] })).error?.code).toBe('INVALID_REQUEST');
  });

  test('wait_for waits for elements, text, URLs and the load event', async ({ bridge, session, site }) => {
    await ok(bridge.command(session, 'click', { selector: '#show-later' }));
    const shown = await ok(bridge.command(session, 'wait_for', { selector: '#later' }));
    expect(shown.matched).toBe(true);
    expect(shown.elapsedMs).toBeGreaterThan(500);
    expect((await ok(bridge.command(session, 'wait_for', { text: 'Arrived' }))).matched).toBe(true);

    await ok(bridge.command(session, 'click', { selector: '#hide-soon' }));
    expect((await ok(bridge.command(session, 'wait_for', { selector: '#soon-gone', state: 'hidden' }))).matched).toBe(true);

    expect((await bridge.command(session, 'wait_for', { selector: '#never' }, 500)).error?.code).toBe('TIMEOUT');
    expect((await bridge.command(session, 'wait_for', { selector: '.dup' })).error?.code).toBe('AMBIGUOUS_SELECTOR');

    await ok(bridge.command(session, 'click', { selector: '#to-page2' }));
    expect((await ok(bridge.command(session, 'wait_for', { urlContains: 'page2' }))).matched).toBe(true);
    expect((await ok(bridge.command(session, 'wait_for', { load: true }))).matched).toBe(true);
    expect((await ok(bridge.command(session, 'snapshot'))).url).toBe(site.url('/page2.html'));
  });

  test('screenshot writes the viewport, the full page or one element', async ({ bridge, session }) => {
    const shot = await ok(bridge.command(session, 'screenshot'));
    expect(shot.mimeType).toBe('image/png');
    expect(existsSync(shot.path)).toBe(true);
    expect(readFileSync(shot.path).subarray(1, 4).toString()).toBe('PNG');
    expect(shot.width).toBeGreaterThan(100);

    const full = await ok(bridge.command(session, 'screenshot', { fullPage: true, format: 'jpeg', quality: 50 }));
    expect(full.mimeType).toBe('image/jpeg');
    expect(full.height).toBeGreaterThan(2000);

    const go = await ok(bridge.command(session, 'screenshot', { selector: '#go' }));
    expect(go.width).toBeLessThan(200);
    expect(go.height).toBeLessThan(100);
    expect((await bridge.command(session, 'screenshot', { selector: '#nope' })).error?.code).toBe('ELEMENT_NOT_FOUND');
  });

  test('dialogs: reported by the click that opened them, blocking until handled', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'click', { selector: '#alert' }))).toEqual({
      tag: 'button',
      text: 'Alert',
      dialog: { type: 'alert', message: 'Hello from alert' },
    });
    const blocked = await bridge.command(session, 'snapshot');
    expect(blocked.error?.code).toBe('DIALOG_OPEN');
    expect(blocked.error?.hint).toContain('Hello from alert');
    expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toHaveLength(1);
    expect(await ok(bridge.command(session, 'handle_dialog', { accept: true }))).toEqual({ type: 'alert', message: 'Hello from alert' });
    expect(await read(bridge, session, '#dialog-result')).toBe('alert closed');
    expect((await bridge.command(session, 'handle_dialog', { accept: true })).error?.code).toBe('NO_DIALOG');

    await ok(bridge.command(session, 'click', { selector: '#confirm' }));
    await ok(bridge.command(session, 'handle_dialog', { accept: false }));
    expect(await read(bridge, session, '#dialog-result')).toBe('confirm false');

    expect((await ok(bridge.command(session, 'click', { selector: '#prompt' }))).dialog.type).toBe('prompt');
    await ok(bridge.command(session, 'handle_dialog', { accept: true, promptText: 'Linh' }));
    expect(await read(bridge, session, '#dialog-result')).toBe('prompt Linh');
  });

  test('evaluate runs in the page and returns JSON values', async ({ bridge, session }) => {
    expect(await ok(bridge.command(session, 'evaluate', { code: '1 + 1' }))).toEqual({ type: 'number', value: 2 });
    expect(await ok(bridge.command(session, 'evaluate', { code: 'await new Promise((r) => setTimeout(() => r("done"), 50))' }))).toEqual({ type: 'string', value: 'done' });
    expect(await ok(bridge.command(session, 'evaluate', { code: '[...document.querySelectorAll(".dup")].length' }))).toEqual({ type: 'number', value: 2 });
    // replMode lets a later call declare the same const again
    await ok(bridge.command(session, 'evaluate', { code: 'const x = 1' }));
    await ok(bridge.command(session, 'evaluate', { code: 'const x = 2' }));
    expect(await ok(bridge.command(session, 'evaluate', { code: 'undefined' }))).toEqual({ type: 'undefined', value: null });
    const thrown = await bridge.command(session, 'evaluate', { code: 'throw new Error("nope")' });
    expect(thrown.error?.code).toBe('EVAL_ERROR');
    expect(thrown.error?.message).toContain('nope');
  });

  test('cdp sends raw commands to the current tab, except Browser and Target', async ({ bridge, session }) => {
    const r = await ok(bridge.command(session, 'cdp', { method: 'Runtime.evaluate', params: { expression: '6 * 7', returnByValue: true } }));
    expect(r.result.value).toBe(42);
    expect((await bridge.command(session, 'cdp', { method: 'Browser.getVersion' })).error?.code).toBe('CDP_ERROR');
    expect((await bridge.command(session, 'cdp', { method: 'Nope.nothing' })).error?.code).toBe('CDP_ERROR');
  });

  test('network capture records requests with their bodies', async ({ bridge, session }) => {
    expect((await ok(bridge.command(session, 'network_requests'))).capturing).toBe(false);
    await ok(bridge.command(session, 'network_start', { filter: '/api/' }));
    await ok(bridge.command(session, 'click', { selector: '#fetch' }));
    await ok(bridge.command(session, 'wait_for', { text: 'fetched hello' }));
    const listed = await ok(bridge.command(session, 'network_requests'));
    expect(listed).toMatchObject({ capturing: true, count: 1 });
    expect(listed.requests[0]).toMatchObject({ method: 'POST', status: 200, mimeType: 'application/json', completed: true });
    expect(listed.requests[0].url).toContain('/api/data.json');

    const detail = await ok(bridge.command(session, 'network_request_detail', { requestId: listed.requests[0].requestId }));
    expect(detail.request).toMatchObject({ method: 'POST', postData: '{"q":1}' });
    expect(detail.response.status).toBe(200);
    expect(JSON.parse(detail.body)).toEqual({ message: 'hello' });
    expect(detail.bodyBase64Encoded).toBe(false);

    expect((await bridge.command(session, 'network_request_detail', { requestId: 'nope' })).error?.code).toBe('INVALID_REQUEST');
    await ok(bridge.command(session, 'network_stop'));
    expect(await ok(bridge.command(session, 'network_requests'))).toEqual({ capturing: false, count: 0, requests: [] });
  });

  test('activate_tab brings the session tab to the front', async ({ bridge, session, site, sw }) => {
    const r = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
    expect((await tabInfo(sw, r.tabId))!.active).toBe(false);
    expect(await ok(bridge.command(session, 'activate_tab'))).toEqual({ tabId: r.tabId, url: site.url('/page2.html'), title: 'Bridge test page 2' });
    expect((await tabInfo(sw, r.tabId))!.active).toBe(true);
  });
});

test('GET /tools marks every action as available with this extension connected', async ({ bridge }) => {
  const tools: { name: string; available: boolean }[] = await (await fetch(`${bridge.base}/tools`)).json();
  expect(tools.filter((t) => !t.available).map((t) => t.name)).toEqual([]);
  expect(tools.map((t) => t.name)).toContain('activate_tab');
});
