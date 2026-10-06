# CDP Input Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Task 3 is done by the user** on a real Chrome. The agent does Tasks 1 and 2, stops to wait for the Task 3 results, then does Task 4.

**Goal:** Answer the question in §13 step 0 of the spec: on a background tab (`active:false`) of a real Chrome, do input via CDP (`Input.dispatchMouseEvent`, `Input.insertText`, `Input.dispatchKeyEvent`) and `Page.captureScreenshot` work correctly? The result decides whether §5.1, §8.4 and §8.5 of the spec must change before the extension is built.

**Architecture:** A static test page (a button, a plain input, a React controlled input, and a ProseMirror editor placed below the fold) served by a small Node server. A minimal MV3 extension opens that page in a new tab, attaches `chrome.debugger`, runs each check, then shows a PASS/FAIL table. Each click on the icon runs 3 variants: background tab with focus emulation, background tab without focus emulation, and an active tab as the control. The spike code lives in `spike/`, is **not committed**, and is deleted after the result is recorded in the spec.

**Tech Stack:** Node 22, esbuild, React 19, ProseMirror, Chrome stable 154 on Windows 11.

**Spec:** `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§8.2, §8.4, §13 step 0, §14)

## Global Constraints

- Spike code stays out of git: `spike/` is added to `.git/info/exclude` and is deleted in Task 4.
- Run on the user's everyday Chrome. Chrome stable dropped the `--load-extension` flag in version 137, so the extension is loaded by hand through `chrome://extensions` → "Load unpacked".
- The test page runs at `http://localhost:8765/`.
- Pass criterion: every row is PASS in the variant **"background tab — focusEmulation: true"** in all 3 window scenarios A, B and C of Task 3.
- The spec is edited only in Task 4, following the decision table there.

## Review Focus

- **A badly written check being read as "CDP does not work":** the active-tab variant is the control. A check that also fails there gets fixed and re-run, with no conclusion drawn about CDP (Task 2 `VARIANTS`, Task 4 decision table).
- **Chrome window loses OS focus or is minimized:** users often switch to another app while the agent runs. Scenarios B and C of Task 3 cover this case.
- **Element below the fold:** the ProseMirror editor sits below a 1600px gap, so the `scrollIntoView` → compute coordinates → click path is exercised for real (Task 1 `index.html`).
- **The yellow "is debugging" bar changes the viewport height:** coordinates are computed after attach, right before each click (Task 2 `click()`).
- **Accented Vietnamese text through `Input.insertText`:** the `plain` check types `xin chào 1` and compares it verbatim (Task 2).

---

### Task 1: Test page and static server

**Files:**
- Create: `spike/package.json`
- Create: `spike/page/index.html`
- Create: `spike/page/app.jsx`
- Create: `spike/serve.mjs`

**Interfaces:**
- Produces: a page at `http://localhost:8765/` with `#btn`, `#plain`, `#react` (plus `#react-state`, which shows the React state), `#keys`, `#pm .ProseMirror`, and `window.spike = { clicks: {count, trusted}, keys: [{key, trusted}], pmText(): string }`. Task 2 reads the results through exactly these names.

- [ ] **Step 1: Keep `spike/` out of git**

```bash
echo "spike/" >> .git/info/exclude
git status --short
```

Expected: `git status` lists nothing new after `spike/` is created in the later steps.

- [ ] **Step 2: Create `spike/package.json` and install dependencies**

```json
{
  "name": "bridge-spike",
  "private": true,
  "type": "module",
  "scripts": {
    "build": "esbuild page/app.jsx --bundle --outfile=page/app.js --jsx=automatic",
    "serve": "node serve.mjs"
  }
}
```

```bash
cd spike
npm i -D esbuild react react-dom prosemirror-model prosemirror-state prosemirror-view prosemirror-schema-basic
```

- [ ] **Step 3: Create `spike/page/index.html`**

```html
<!doctype html>
<html lang="vi">
<head>
  <meta charset="utf-8">
  <title>Bridge spike page</title>
  <style>
    body { font: 16px system-ui; margin: 24px; }
    section { margin: 16px 0; }
    .spacer { height: 1600px; border-left: 4px dashed #ccc; }
    .ProseMirror { white-space: pre-wrap; word-wrap: break-word; border: 1px solid #888; min-height: 3em; padding: 4px; }
  </style>
</head>
<body>
  <section><button id="btn">Click me</button></section>
  <section><input id="plain" placeholder="plain input"></section>
  <section id="react-root"></section>
  <section><input id="keys" placeholder="key events land here"></section>
  <!-- pushes the editor below the fold so the click path has to scroll -->
  <div class="spacer"></div>
  <section id="pm"></section>
  <script src="app.js"></script>
</body>
</html>
```

- [ ] **Step 4: Create `spike/page/app.jsx`**

```jsx
import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { EditorState } from 'prosemirror-state';
import { EditorView } from 'prosemirror-view';
import { schema } from 'prosemirror-schema-basic';

// The spike reads every result back through window.spike, so the page only records what happened.
window.spike = { clicks: { count: 0, trusted: false }, keys: [] };

document.querySelector('#btn').addEventListener('click', (e) => {
  window.spike.clicks.count++;
  window.spike.clicks.trusted = e.isTrusted;
});

document.querySelector('#keys').addEventListener('keydown', (e) => {
  window.spike.keys.push({ key: e.key, trusted: e.isTrusted });
});

function ControlledInput() {
  const [value, setValue] = useState('');
  return (
    <>
      <input id="react" placeholder="React controlled input" value={value} onChange={(e) => setValue(e.target.value)} />
      <output id="react-state">{value}</output>
    </>
  );
}

createRoot(document.querySelector('#react-root')).render(<ControlledInput />);

const view = new EditorView(document.querySelector('#pm'), { state: EditorState.create({ schema }) });
window.spike.pmText = () => view.state.doc.textContent;
```

- [ ] **Step 5: Create `spike/serve.mjs`**

```js
import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { extname, join } from 'node:path';

const root = join(import.meta.dirname, 'page');
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8' };

createServer(async (req, res) => {
  const name = req.url === '/' ? 'index.html' : req.url.slice(1).split('?')[0];
  try {
    const body = await readFile(join(root, name));
    res.writeHead(200, { 'Content-Type': types[extname(name)] ?? 'application/octet-stream' });
    res.end(body);
  } catch {
    res.writeHead(404).end('not found');
  }
}).listen(8765, '127.0.0.1', () => console.log('spike page on http://localhost:8765/'));
```

- [ ] **Step 6: Build, run the server and check the page renders fully**

```bash
cd spike
npm run build
npm run serve
```

In another terminal:

```bash
"/c/Program Files/Google/Chrome/Application/chrome.exe" --headless --disable-gpu --user-data-dir="$TEMP/spike-probe" --virtual-time-budget=3000 --dump-dom http://localhost:8765/ | grep -o 'id="react"\|class="ProseMirror"'
```

Expected: prints both `id="react"` and `class="ProseMirror"`. If either is missing, the bundle is broken; open the page in Chrome and check the Console.

- [ ] **Step 7: Manual check**

Open `http://localhost:8765/` in Chrome and type into the React input: the text must appear right next to it in the `<output>`. Scroll to the bottom of the page and type into the ProseMirror box: typing must work. Do not commit.

---

### Task 2: Spike extension

**Files:**
- Create: `spike/extension/manifest.json`
- Create: `spike/extension/background.js`
- Create: `spike/extension/results.html`
- Create: `spike/extension/results.js`

**Interfaces:**
- Consumes: the page and `window.spike` from Task 1.
- Produces: clicking the extension icon → wait 5 seconds → run 3 variants → save `chrome.storage.local.spike = { at, userAgent, runs: [{active, focusEmulation, visibility, click, plain, react, reactReplace, prosemirror, prosemirrorReplace, key, screenshot, tabStillBackground, userTabStillActive, pass: {...}}] }` → badge `OK`/`FAIL` → open the `results.html` tab.

- [ ] **Step 1: Create `spike/extension/manifest.json`**

```json
{
  "manifest_version": 3,
  "name": "Bridge spike",
  "version": "0.0.1",
  "permissions": ["debugger", "tabs", "storage"],
  "background": { "service_worker": "background.js" },
  "action": { "default_title": "Run the CDP input spike" }
}
```

This manifest deliberately has no `key`, so the spike does not take the ID of the real extension.

- [ ] **Step 2: Create `spike/extension/background.js`**

```js
const PAGE = 'http://localhost:8765/';
// Time to minimize the window or switch to another app after clicking the toolbar icon.
const START_DELAY_MS = 5000;

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function waitForLoad(tabId) {
  for (let i = 0; i < 100; i++) {
    if ((await chrome.tabs.get(tabId)).status === 'complete') return;
    await sleep(100);
  }
  throw new Error('spike page did not load: is `npm run serve` running?');
}

// Runs every check once on a fresh tab. Each check records its own error so one failure does not
// hide the others.
async function runOnce({ active, focusEmulation }) {
  const r = { active, focusEmulation };
  const [userTab] = await chrome.tabs.query({ active: true, lastFocusedWindow: true });
  const tab = await chrome.tabs.create({ url: PAGE, active });
  const target = { tabId: tab.id };
  const send = (method, params = {}) => chrome.debugger.sendCommand(target, method, params);
  const evaluate = async (expression) => {
    const res = await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
    if (res.exceptionDetails) throw new Error(res.exceptionDetails.text);
    return res.result.value;
  };
  const q = (sel) => `document.querySelector(${JSON.stringify(sel)})`;
  const click = async (sel) => {
    const { x, y } = await evaluate(`(() => {
      const el = ${q(sel)};
      el.scrollIntoView({ block: 'center' });
      const b = el.getBoundingClientRect();
      return { x: b.x + b.width / 2, y: b.y + b.height / 2 };
    })()`);
    await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y });
    await send('Input.dispatchMouseEvent', { type: 'mousePressed', x, y, button: 'left', buttons: 1, clickCount: 1 });
    await send('Input.dispatchMouseEvent', { type: 'mouseReleased', x, y, button: 'left', buttons: 0, clickCount: 1 });
  };
  const check = async (name, fn) => {
    try {
      r[name] = await fn();
    } catch (e) {
      r[name] = { error: String(e) };
    }
  };

  try {
    await waitForLoad(tab.id);
    await chrome.debugger.attach(target, '1.3');
    await send('Page.enable');
    await send('Runtime.enable');
    if (focusEmulation) await send('Emulation.setFocusEmulationEnabled', { enabled: true });
    r.visibility = await evaluate('document.visibilityState');

    await check('click', async () => {
      await click('#btn');
      return evaluate('window.spike.clicks');
    });
    await check('plain', async () => {
      await click('#plain');
      await send('Input.insertText', { text: 'xin chào 1' });
      return evaluate(`${q('#plain')}.value`);
    });
    await check('react', async () => {
      await click('#react');
      await send('Input.insertText', { text: 'react ok' });
      return evaluate(`${q('#react-state')}.textContent`);
    });
    // fill's strategy: select everything, then insertText replaces the selection
    await check('reactReplace', async () => {
      await evaluate(`${q('#react')}.select()`);
      await send('Input.insertText', { text: 'replaced' });
      return evaluate(`${q('#react-state')}.textContent`);
    });
    await check('prosemirror', async () => {
      await click('#pm .ProseMirror');
      await send('Input.insertText', { text: 'pm ok' });
      return evaluate('window.spike.pmText()');
    });
    await check('prosemirrorReplace', async () => {
      await evaluate(`(() => { const el = ${q('#pm .ProseMirror')}; el.focus(); getSelection().selectAllChildren(el); })()`);
      await send('Input.insertText', { text: 'pm replaced' });
      return evaluate('window.spike.pmText()');
    });
    await check('key', async () => {
      await click('#keys');
      await send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 });
      await send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13 });
      return evaluate('window.spike.keys');
    });
    await check('screenshot', async () => (await send('Page.captureScreenshot', { format: 'png' })).data);

    r.tabStillBackground = !(await chrome.tabs.get(tab.id)).active;
    r.userTabStillActive = userTab ? (await chrome.tabs.get(userTab.id)).active : null;
  } finally {
    await chrome.debugger.detach(target).catch(() => {});
    await chrome.tabs.remove(tab.id).catch(() => {});
  }
  return r;
}

function judge(r) {
  return {
    click: r.click?.count === 1 && r.click?.trusted === true,
    plain: r.plain === 'xin chào 1',
    react: r.react === 'react ok',
    reactReplace: r.reactReplace === 'replaced',
    prosemirror: r.prosemirror === 'pm ok',
    prosemirrorReplace: r.prosemirrorReplace === 'pm replaced',
    key: Array.isArray(r.key) && r.key.some((k) => k.key === 'Enter' && k.trusted),
    screenshot: typeof r.screenshot === 'string' && r.screenshot.length > 5000,
    // the active-tab baseline takes the focus on purpose
    stayedInBackground: r.active || (r.tabStillBackground === true && r.userTabStillActive !== false),
  };
}

// The active-tab run is the control: a check that fails there is a broken check, not a CDP limit.
const VARIANTS = [
  { active: false, focusEmulation: true },
  { active: false, focusEmulation: false },
  { active: true, focusEmulation: true },
];

chrome.action.onClicked.addListener(async () => {
  await chrome.action.setBadgeText({ text: '...' });
  await sleep(START_DELAY_MS);
  const runs = [];
  for (const variant of VARIANTS) {
    try {
      const r = await runOnce(variant);
      runs.push({ ...r, pass: judge(r) });
    } catch (e) {
      runs.push({ ...variant, fatal: String(e) });
    }
  }
  await chrome.storage.local.set({ spike: { at: new Date().toISOString(), userAgent: navigator.userAgent, runs } });
  const ok = runs.every((run) => run.pass && Object.values(run.pass).every(Boolean));
  await chrome.action.setBadgeText({ text: ok ? 'OK' : 'FAIL' });
  await chrome.tabs.create({ url: chrome.runtime.getURL('results.html') });
});
```

- [ ] **Step 3: Create `spike/extension/results.html`**

```html
<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Spike results</title>
  <style>
    body { font: 14px system-ui; margin: 24px; }
    table { border-collapse: collapse; margin: 12px 0; }
    td, th { border: 1px solid #aaa; padding: 4px 8px; text-align: left; }
    .ok { background: #d4f7d4; } .fail { background: #f7d4d4; }
    img { max-width: 480px; border: 1px solid #aaa; }
    pre { white-space: pre-wrap; }
  </style>
</head>
<body>
  <div id="out">No results yet. Click the extension icon.</div>
  <script src="results.js"></script>
</body>
</html>
```

- [ ] **Step 4: Create `spike/extension/results.js`**

```js
const out = document.querySelector('#out');

chrome.storage.local.get('spike').then(({ spike }) => {
  if (!spike) return;
  out.textContent = '';
  const h = document.createElement('p');
  h.textContent = `${spike.at} — ${spike.userAgent}`;
  out.append(h);
  for (const run of spike.runs) {
    const title = document.createElement('h2');
    title.textContent = `${run.active ? 'active tab (control)' : 'background tab'} — focusEmulation: ${run.focusEmulation} — visibility: ${run.visibility ?? '?'}`;
    out.append(title);
    if (run.fatal) {
      const p = document.createElement('pre');
      p.textContent = run.fatal;
      out.append(p);
      continue;
    }
    const table = document.createElement('table');
    for (const [name, pass] of Object.entries(run.pass)) {
      const tr = table.insertRow();
      tr.className = pass ? 'ok' : 'fail';
      tr.insertCell().textContent = name;
      tr.insertCell().textContent = pass ? 'PASS' : 'FAIL';
      const raw = name === 'screenshot' ? '(image below)' : JSON.stringify(run[name]);
      tr.insertCell().textContent = raw ?? '';
    }
    out.append(table);
    if (typeof run.screenshot === 'string') {
      const img = document.createElement('img');
      img.src = 'data:image/png;base64,' + run.screenshot;
      out.append(img);
    }
  }
  // Plain JSON to paste back into the conversation; the image data is left out.
  const pre = document.createElement('pre');
  pre.textContent = JSON.stringify({ ...spike, runs: spike.runs.map(({ screenshot, ...rest }) => rest) }, null, 2);
  out.append(pre);
});
```

- [ ] **Step 5: Syntax check**

```bash
node --check spike/extension/background.js && node --check spike/extension/results.js && echo OK
```

Expected: `OK`

- [ ] **Step 6: Load into Chrome**

`chrome://extensions` → turn on **Developer mode** → **Load unpacked** → pick the `spike/extension` folder. The "Bridge spike" card appears with no red **Errors** button. Pin the icon to the toolbar from the puzzle-piece menu.

---

### Task 3: Run the matrix on a real Chrome (done by the user)

**Files:** none.

**Interfaces:**
- Consumes: the extension loaded in Task 2, with `npm run serve` running.
- Produces: 3 JSON blocks (at the bottom of `results.html`) for scenarios A, B and C, pasted back into the conversation.

Each time the icon is clicked, the extension waits 5 seconds before running, and the run takes about 10–20 seconds. The `...` badge means it is running; when done, the results tab opens by itself.

- [ ] **Step 1: Scenario A, window focused, user on another tab**

Open any page (for example a news page) as the tab being viewed. Click the "Bridge spike" icon and leave everything alone until the results tab opens. Copy the JSON block at the bottom of the page.

- [ ] **Step 2: Scenario B, Chrome loses OS focus**

Click the icon, then within 5 seconds press Alt+Tab to another app (for example VS Code). Wait 20 seconds, then go back to Chrome. Copy the JSON.

- [ ] **Step 3: Scenario C, Chrome window minimized**

Click the icon, then within 5 seconds minimize the Chrome window. Wait 20 seconds, then restore it. Copy the JSON.

- [ ] **Step 4: Send the results**

Paste the 3 JSON blocks into the conversation with labels A/B/C. The agent fills in the following table in Task 4:

| Check | A: background + FE | A: background, no FE | A: active | B: background + FE | C: background + FE |
|---|---|---|---|---|---|
| click | | | | | |
| plain | | | | | |
| react | | | | | |
| reactReplace | | | | | |
| prosemirror | | | | | |
| prosemirrorReplace | | | | | |
| key | | | | | |
| screenshot | | | | | |
| stayedInBackground | | | | | |

---

### Task 4: Record the result in the spec and clean up the spike

**Files:**
- Modify: `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§13 step 0, §14, and the items in the decision table if any check fails)
- Delete: `spike/`

**Interfaces:**
- Consumes: the result table from Task 3.
- Produces: a spec that correctly reflects how the extension delivers input to the page. The extension plan (written after this plan) reads the updated spec.

- [ ] **Step 1: Apply the decision table**

Read the "background + FE" column. A check that fails in the **active** column is a wrong check: fix the check in `background.js`, ask the user to re-run Task 3, and do not change the spec because of that check.

| Result | Spec change |
|---|---|
| Every check PASSes in A, B, C | No design change. Only record the result (Step 2) |
| `click` fails | §8.4 `click`: after the actionability check, call `el.click()` in the isolated world instead of `Input.dispatchMouseEvent` |
| `plain`, `react` or `reactReplace` fails | §8.4 `fill` for input/textarea: assign through the native value setter (`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set`), then fire `input` and `change` (bubbling) |
| `prosemirror` or `prosemirrorReplace` fails | §8.4 `fill` for contenteditable: select all with the Selection API, then `document.execCommand('insertText', false, value)` |
| `key` or `screenshot` fails | No substitute exists on a background tab: §5.1 `navigate` opens the new tab with `active:true`, §8.5 `screenshot` activates the tab before capturing |
| Failures from two or more of the groups above | Change direction entirely as in §13 step 0: `click` uses `el.click()`, `fill` uses the native setter with synthetic events, tabs open with `active:true` |
| PASS in A but fail in B or C | Add a row to §14: the window condition that real input needs, and the matching error hint for the agent |
| The "no FE" variant also PASSes everything | Still keep `Emulation.setFocusEmulationEnabled` (§8.2); note that it was not seen to be required on this Chrome version |

- [ ] **Step 2: Record the result in the spec**

In §13 step 0, add one line right below the block "If it fails, change direction…":

```markdown
   **Result (2026-MM-DD, Chrome <version from userAgent>):** <all passed | the failing checks and the direction chosen>. Updated <the sections changed, or "no change needed">.
```

In §14, in the "Mitigation" column of the row "Real input through CDP does not reach the background tab": replace "Spike at step 0, a fallback direction is ready" with a short result, for example "Spike 2026-MM-DD passed on Chrome 154, even with the window minimized".

- [ ] **Step 3: Delete the spike**

```bash
rm -rf spike
sed -i '/^spike\/$/d' .git/info/exclude
git status --short
```

Expected: only the modified spec file remains. Remove the "Bridge spike" extension from `chrome://extensions`.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-10-06-browser-bridge-design.md
git commit -m "docs: record CDP input spike result"
```
