# CDP Input Spike Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Task 3 do người dùng tự làm** trên Chrome thật. Agent làm Task 1, 2, dừng lại chờ kết quả Task 3, rồi làm Task 4.

**Goal:** Trả lời câu hỏi của §13 bước 0 trong spec: trên một tab nền (`active:false`) của Chrome thật, input qua CDP (`Input.dispatchMouseEvent`, `Input.insertText`, `Input.dispatchKeyEvent`) và `Page.captureScreenshot` có chạy đúng không. Kết quả quyết định có phải đổi §5.1, §8.4, §8.5 của spec trước khi làm extension hay không.

**Architecture:** Một trang test tĩnh (button, input thường, React controlled input, editor ProseMirror nằm dưới màn hình) do một server Node nhỏ phục vụ. Một extension MV3 tối giản mở trang đó trong tab mới, attach `chrome.debugger`, chạy từng check, rồi hiện bảng PASS/FAIL. Mỗi lần bấm icon chạy 3 biến thể: tab nền có focus emulation, tab nền không focus emulation, và tab active làm đối chứng. Code spike nằm trong `spike/`, **không commit**, xoá sau khi ghi kết quả vào spec.

**Tech Stack:** Node 22, esbuild, React 19, ProseMirror, Chrome stable 154 trên Windows 11.

**Spec:** `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§8.2, §8.4, §13 bước 0, §14)

## Global Constraints

- Code spike không vào git: `spike/` được thêm vào `.git/info/exclude`, và bị xoá ở Task 4.
- Chạy trên Chrome hằng ngày của người dùng. Chrome stable đã bỏ cờ `--load-extension` từ bản 137, nên extension được load bằng tay qua `chrome://extensions` → "Load unpacked".
- Trang test chạy ở `http://localhost:8765/`.
- Tiêu chí đạt: mọi dòng PASS ở biến thể **"background tab — focusEmulation: true"** trong cả 3 tình huống cửa sổ A, B, C của Task 3.
- Spec chỉ được sửa ở Task 4, theo bảng quyết định ở đó.

## Review Focus

- **Check viết sai bị đọc thành "CDP không chạy":** biến thể tab active là đối chứng. Check nào fail cả ở đó thì sửa check rồi chạy lại, không kết luận gì về CDP (Task 2 `VARIANTS`, Task 4 bảng quyết định).
- **Cửa sổ Chrome mất focus của hệ điều hành hoặc bị minimize:** người dùng hay chuyển sang app khác trong lúc agent chạy. Tình huống B và C của Task 3 bắt trường hợp này.
- **Element nằm dưới màn hình:** editor ProseMirror đặt dưới một khoảng trống 1600px, nên đường `scrollIntoView` → tính toạ độ → click được thử thật (Task 1 `index.html`).
- **Thanh vàng "đang debug" làm đổi chiều cao viewport:** toạ độ được tính sau khi attach, ngay trước mỗi lần click (Task 2 `click()`).
- **Text tiếng Việt có dấu qua `Input.insertText`:** check `plain` gõ `xin chào 1` và so khớp nguyên văn (Task 2).

---

### Task 1: Trang test và server tĩnh

**Files:**
- Create: `spike/package.json`
- Create: `spike/page/index.html`
- Create: `spike/page/app.jsx`
- Create: `spike/serve.mjs`

**Interfaces:**
- Produces: trang ở `http://localhost:8765/` có `#btn`, `#plain`, `#react` (kèm `#react-state` hiện state của React), `#keys`, `#pm .ProseMirror`, và `window.spike = { clicks: {count, trusted}, keys: [{key, trusted}], pmText(): string }`. Task 2 đọc kết quả qua đúng các tên này.

- [ ] **Step 1: Loại `spike/` khỏi git**

```bash
echo "spike/" >> .git/info/exclude
git status --short
```

Expected: `git status` không liệt kê gì mới sau khi tạo `spike/` ở các bước sau.

- [ ] **Step 2: Tạo `spike/package.json` và cài dependency**

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

- [ ] **Step 3: Tạo `spike/page/index.html`**

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

- [ ] **Step 4: Tạo `spike/page/app.jsx`**

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

- [ ] **Step 5: Tạo `spike/serve.mjs`**

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

- [ ] **Step 6: Build, chạy server và kiểm tra trang render đủ**

```bash
cd spike
npm run build
npm run serve
```

Ở terminal khác:

```bash
"/c/Program Files/Google/Chrome/Application/chrome.exe" --headless --disable-gpu --user-data-dir="$TEMP/spike-probe" --virtual-time-budget=3000 --dump-dom http://localhost:8765/ | grep -o 'id="react"\|class="ProseMirror"'
```

Expected: in ra cả `id="react"` lẫn `class="ProseMirror"`. Thiếu một trong hai nghĩa là bundle lỗi, mở trang trong Chrome và xem Console.

- [ ] **Step 7: Kiểm tra tay**

Mở `http://localhost:8765/` trong Chrome, gõ vào ô React: chữ phải hiện lại ngay bên cạnh trong `<output>`. Cuộn xuống cuối trang, gõ vào khung ProseMirror: phải gõ được. Không commit.

---

### Task 2: Extension spike

**Files:**
- Create: `spike/extension/manifest.json`
- Create: `spike/extension/background.js`
- Create: `spike/extension/results.html`
- Create: `spike/extension/results.js`

**Interfaces:**
- Consumes: trang và `window.spike` của Task 1.
- Produces: bấm icon extension → chờ 5 giây → chạy 3 biến thể → lưu `chrome.storage.local.spike = { at, userAgent, runs: [{active, focusEmulation, visibility, click, plain, react, reactReplace, prosemirror, prosemirrorReplace, key, screenshot, tabStillBackground, userTabStillActive, pass: {...}}] }` → badge `OK`/`FAIL` → mở tab `results.html`.

- [ ] **Step 1: Tạo `spike/extension/manifest.json`**

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

Manifest này cố ý không có `key`, để spike không chiếm ID của extension thật.

- [ ] **Step 2: Tạo `spike/extension/background.js`**

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

- [ ] **Step 3: Tạo `spike/extension/results.html`**

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

- [ ] **Step 4: Tạo `spike/extension/results.js`**

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

- [ ] **Step 5: Kiểm tra cú pháp**

```bash
node --check spike/extension/background.js && node --check spike/extension/results.js && echo OK
```

Expected: `OK`

- [ ] **Step 6: Load vào Chrome**

`chrome://extensions` → bật **Developer mode** → **Load unpacked** → chọn thư mục `spike/extension`. Thẻ "Bridge spike" hiện ra, không có nút **Errors** màu đỏ. Ghim icon từ menu hình mảnh ghép lên thanh công cụ.

---

### Task 3: Chạy ma trận trên Chrome thật (người dùng làm)

**Files:** không có.

**Interfaces:**
- Consumes: extension đã load ở Task 2, `npm run serve` đang chạy.
- Produces: 3 khối JSON (cuối trang `results.html`) cho 3 tình huống A, B, C, dán lại vào cuộc hội thoại.

Mỗi lần bấm icon, extension đợi 5 giây rồi mới chạy, khoảng 10–20 giây. Badge `...` nghĩa là đang chạy; xong thì tab kết quả tự mở.

- [ ] **Step 1: Tình huống A, cửa sổ đang focus, người dùng ở tab khác**

Mở một trang bất kỳ (ví dụ một trang tin tức) làm tab đang xem. Bấm icon "Bridge spike", để yên cho tới khi tab kết quả mở ra. Copy khối JSON cuối trang.

- [ ] **Step 2: Tình huống B, Chrome mất focus hệ điều hành**

Bấm icon, rồi trong 5 giây nhấn Alt+Tab sang một app khác (ví dụ VS Code). Đợi 20 giây rồi quay lại Chrome. Copy JSON.

- [ ] **Step 3: Tình huống C, cửa sổ Chrome bị minimize**

Bấm icon, rồi trong 5 giây minimize cửa sổ Chrome. Đợi 20 giây, mở lại. Copy JSON.

- [ ] **Step 4: Gửi kết quả**

Dán 3 khối JSON vào cuộc hội thoại kèm nhãn A/B/C. Agent điền bảng sau ở Task 4:

| Check | A: nền + FE | A: nền, không FE | A: active | B: nền + FE | C: nền + FE |
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

### Task 4: Ghi kết quả vào spec và dọn spike

**Files:**
- Modify: `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§13 bước 0, §14, và các mục ở bảng quyết định nếu có check fail)
- Delete: `spike/`

**Interfaces:**
- Consumes: bảng kết quả của Task 3.
- Produces: spec phản ánh đúng cách extension đưa input vào trang. Plan extension (viết sau plan này) đọc spec đã cập nhật.

- [ ] **Step 1: Áp bảng quyết định**

Đọc cột "nền + FE". Check nào fail ở cột **active** thì đó là check sai: sửa check trong `background.js`, nhờ người dùng chạy lại Task 3, không sửa spec theo check đó.

| Kết quả | Sửa spec |
|---|---|
| Mọi check PASS ở A, B, C | Không đổi thiết kế. Chỉ ghi kết quả (Step 2) |
| `click` fail | §8.4 `click`: sau khi kiểm tra actionability, gọi `el.click()` trong isolated world thay cho `Input.dispatchMouseEvent` |
| `plain`, `react` hoặc `reactReplace` fail | §8.4 `fill` cho input/textarea: gán qua native value setter (`Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set`) rồi bắn `input` và `change` (bubbles) |
| `prosemirror` hoặc `prosemirrorReplace` fail | §8.4 `fill` cho contenteditable: chọn hết bằng Selection API rồi `document.execCommand('insertText', false, value)` |
| `key` hoặc `screenshot` fail | Không có cách thay ở tab nền: §5.1 `navigate` mở tab mới với `active:true`, §8.5 `screenshot` kích hoạt tab trước khi chụp |
| Fail từ hai nhóm trên trở lên | Đổi hướng toàn bộ như §13 bước 0: `click` dùng `el.click()`, `fill` dùng native setter kèm sự kiện giả, tab mở `active:true` |
| PASS ở A nhưng fail ở B hoặc C | Thêm một dòng vào §14: điều kiện cửa sổ mà input thật cần, và hint lỗi tương ứng cho agent |
| Biến thể "không FE" cũng PASS hết | Vẫn giữ `Emulation.setFocusEmulationEnabled` (§8.2); ghi chú là chưa thấy bắt buộc trên bản Chrome này |

- [ ] **Step 2: Ghi kết quả vào spec**

Ở §13 bước 0, thêm một dòng ngay dưới khối "Thất bại thì đổi hướng…":

```markdown
   **Kết quả (2026-MM-DD, Chrome <version từ userAgent>):** <đạt hết | các check fail và hướng đã chọn>. Đã cập nhật <các mục đã sửa, hoặc "không cần sửa">.
```

Ở §14, cột "Giảm thiểu" của dòng "Input thật qua CDP không tới được tab nền": thay "Spike ở bước 0, có sẵn phương án đổi hướng" bằng kết quả ngắn gọn, ví dụ "Spike 2026-MM-DD đạt trên Chrome 154, kể cả khi cửa sổ bị minimize".

- [ ] **Step 3: Xoá spike**

```bash
rm -rf spike
sed -i '/^spike\/$/d' .git/info/exclude
git status --short
```

Expected: chỉ còn file spec bị sửa. Gỡ extension "Bridge spike" khỏi `chrome://extensions`.

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-10-06-browser-bridge-design.md
git commit -m "docs: record CDP input spike result"
```
