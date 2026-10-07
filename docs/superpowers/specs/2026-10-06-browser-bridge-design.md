# Browser Bridge — Design Spec

- **Date:** 2026-10-06
- **Status:** Draft, awaiting review
- **Working name:** project `browser-bridge`, binary `bridge`

## 1. Goals

A self-owned replacement for Kimi WebBridge: install an extension into Chrome/Edge and let **any agent** (Claude Code, Codex, a harness running a local Ollama model) control the user's real browser, using their existing logged-in sessions. No third-party account is needed, and we own the whole source.

### 1.1 Use cases the MVP must cover

1. **Read pages that require login:** open a page, take a snapshot, read the content, take a screenshot.
2. **Operate forms:** click, type text (including React controlled inputs and rich text editors), select from dropdowns, press keys, upload files.
3. **Capture network traffic:** view the requests/responses (headers and body) of a page.
4. **Serve as a base for writing a dedicated CLI per site:** a stable HTTP API, `evaluate` running in the page's main world, element selection by CSS selector.

### 1.2 MVP completion criteria

All 4 use cases work on the user's everyday Chrome (Windows), through 3 consumers:

- Claude Code, using `SKILL.md`;
- Codex, using MCP;
- a harness running a local Ollama model, using MCP.

### 1.3 Out of scope for the MVP

- AI chat in the side panel (the side panel is only for viewing status and debugging).
- Operating inside iframes (the snapshot only *lists* frames), `save_as_pdf`, hover.
- Multiple browsers connected at the same time.
- Recorder/workflow, agent loop.
- Publishing the extension to a store (load unpacked only). How to distribute to other machines is covered in the installer spec (`2026-10-06-installer-design.md`).
- Authentication token (reason in §7).
- Official macOS/Linux support. The Go code is written to be portable but is only tested on Windows.

## 2. Architecture

```text
Claude Code ──┐ bridge call / curl (per SKILL.md)
Codex ────────┼───────────────────────────▶ HTTP 127.0.0.1:9876   ┐
Ollama harness┘ MCP stdio: `bridge mcp` ──HTTP──▶                  │ bridge daemon (Go)
                                                                    │  · validate against JSON Schema
                                                                    │  · route by session
                                                                    │  · timeouts, standard error codes
                                                                    │  · write screenshot files
                                                                    ┘
                                      ▲ WebSocket /ws (the extension connects actively)
                                      │
                         MV3 extension (TypeScript, WXT)
                          · service worker: WS client, session/tab group,
                            CDP executor (chrome.debugger)
                          · page agent injected into an isolated world: snapshot, ref, actionability
                          · side panel (React): status, log
```

**Responsibility boundaries:**
- The daemon never touches CDP or the DOM. It only validates requests, routes them to the extension, manages timeouts and writes artifact files.
- All browser operations live in the extension.
- The agent only knows the HTTP API (or MCP tools). It knows nothing about the Chrome API, CDP or WebSocket.

## 3. Repo structure

```text
browser-bridge/
├── daemon/                      Go module, builds the `bridge` binary
│   ├── cmd/bridge/              the subcommands (§9)
│   ├── cmd/schemagen/           generates schema/ and the tool table in SKILL.md from Go structs
│   ├── skill/browser-bridge/    SKILL.md, embedded in the binary for install-skill
│   └── internal/
│       ├── protocol/            Go structs + descriptions for each action: the single source of the schema
│       ├── server/              HTTP API, security checks, WebSocket hub
│       ├── session/             per-session queue, map of request id ↔ pending response
│       ├── client/              HTTP client for POST /command, shared by bridge call and bridge mcp
│       ├── mcp/                 stdio MCP server (official Go SDK), calls back into the HTTP API
│       └── home/                the ~/.browser-bridge directory, config, pid, log
├── schema/                      JSON Schema generated from Go, committed to the repo
├── extension/                   WXT + TypeScript
│   └── src/
│       ├── entrypoints/         WXT entry points: background.ts, sidepanel/ (React)
│       ├── background/          service worker logic: connection, router, sessions, cdp, actions/
│       ├── page-agent/          IIFE bundle injected into the page
│       ├── shared/              types and storage keys shared between the service worker and the side panel
│       └── generated/           TS types generated from schema/
├── testpage/                    static HTML page + fake API for testing
└── e2e/                         Playwright (TypeScript)
```

### 3.1 Codegen (define once)

```text
daemon/internal/protocol (Go struct + description)
   └─ go run ./daemon/cmd/schemagen
        ├─▶ schema/protocol.schema.json ──json-schema-to-typescript──▶ extension/src/generated/protocol.ts
        ├─▶ tool list for GET /tools and MCP (Go reads it directly from the protocol package at runtime)
        └─▶ tool table in daemon/skill/browser-bridge/SKILL.md (between the two markers <!-- tools:begin --> / <!-- tools:end -->)
```

- The skill lives inside the daemon module, not at the repo root, because Go can only embed files below its module. Embedded, the skill that `install-skill` writes always describes the binary that runs its commands.

- The daemon validates requests against the generated JSON Schema (`santhosh-tekuri/jsonschema`).
- The extension only receives commands from the daemon, so it uses TS types only and does not validate again at runtime.
- A `check:gen` script reruns the codegen and fails if any generated file differs from the committed version.

## 4. HTTP API

| Endpoint | Description |
|---|---|
| `POST /command` | Run one action |
| `GET /tools` | `[{name, description, inputSchema, available}]`, usable as a function definition for any LLM. `available` is whether the connected extension implements the action (it lists them in `hello`, §10); with no extension connected every action is `false` |
| `GET /status` | Daemon and extension status (§9.3) |
| `GET /ws` | WebSocket, for the extension only (§6) |
| `POST /shutdown` | The daemon returns 200, shuts down gracefully and deletes `daemon.pid`/`daemon.addr` itself. Used by `bridge stop`; goes through the same security checks as `/command` |

### 4.1 Request and response

```json
{ "action": "click", "args": { "selector": "@e12" }, "session": "jira-report", "timeoutMs": 15000 }
```

- `session`: required, matches the regex `^[a-z0-9][a-z0-9_-]{0,63}$`.
- `timeoutMs`: optional, at most 120000. Defaults to 30000 for `navigate`/`reload`/`go_back`/`go_forward`, and 15000 for all other actions.

```json
{ "ok": true,  "data": { } }
{ "ok": false, "error": { "code": "STALE_REF", "message": "@e12 is no longer in the page", "hint": "Take a new snapshot" } }
```

- `hint` is optional. It is a suggestion for the next step the agent should take, written so that small models can understand it too.
- HTTP status:

  | Status | When |
  |---|---|
  | 200 | Every result that was handled, including `ok:false` |
  | 400 | Malformed JSON, wrong schema, nonexistent action |
  | 403 | Fails the security checks (§7), error code `FORBIDDEN` |

  Every case returns the envelope above.

## 5. MVP actions

`selector` is either a ref `@e<n>` or a CSS selector. A string starting with `@e` is a ref; anything else is treated as CSS. A CSS selector must match **exactly one** element:
- if it matches no element, return `ELEMENT_NOT_FOUND` (except `wait_for`, which keeps waiting, §5.4);
- if it matches more than one element, return `AMBIGUOUS_SELECTOR`.

### 5.1 Tabs and navigation

| Action | Args | Returns `data` | Notes |
|---|---|---|---|
| `navigate` | `url` (required), `newTab` (bool, default false), `groupTitle` | `tabId, url, title` | If the session has no current tab, it always creates a new tab. New tabs open in the background (`active:false`). `groupTitle` is only used when creating the group, and defaults to the session name. Only `http`, `https`, `about:blank` are accepted. Returns only after the load event completes |
| `find_tab` | `url` (matched by host, `example.com` also matches `www.example.com`, path ignored), `active` (bool) | `tabId, url, title, borrowed` | Requires `url`, `active:true`, or both. By default it only searches the session's own tabs. With `active:true` it borrows the tab the user is currently viewing, and that tab is not pulled into the group. The result becomes the session's current tab |
| `activate_tab` | — | `tabId, url, title` | Makes the current tab the active tab of its window and focuses the window. Tabs stay in the background otherwise, and Chrome defers some things, such as starting media, in a tab that has never been visible |
| `list_tabs` | — | `tabs: [{tabId, url, title, current, borrowed}]` | Session tabs only |
| `close_tab` | — | `closed, released` | A session tab is closed. A borrowed tab is only released (detached, not closed). Afterwards the session has no current tab |
| `close_session` | — | `closed` (number of tabs) | Closes every tab of the session, releases borrowed tabs, removes the group |
| `go_back` / `go_forward` / `reload` | — | `url, title` | If there is no history to go back/forward, return `NAVIGATION_FAILED` |

### 5.2 Reading

| Action | Args | Returns `data` |
|---|---|---|
| `snapshot` | `maxChars` (default 20000) | `url, title, tree, frames: [{frame, url, width, height}], truncated` |

Format of `tree`:
- Each line has the form `- <role> "<name>" [state…] @e<n>`, indented 2 spaces per level.
- Refs are only attached to interactive elements.
- Static text appears as `- text "…"`, with each paragraph cut at 200 characters.
- Text runs through inline elements as the page shows it: the whitespace at the edge of a `<span>` or `<b>` still separates words (`Những Bản <b>Lofi</b>` gives `Những Bản Lofi`), and no space is added where the page has none.
- A control whose only content is a control of the same role with the same name (YouTube wraps each menu link in a second link) is one line, with the outer element's ref.

```text
- heading "Sign in" [level=1]
- textbox "Email" [value="a@b.com"] @e3
- textbox "Password" @e4
- checkbox "Remember me" [checked] @e6
- button "Continue" @e5
```

### 5.3 Operations

| Action | Args | Returns `data` |
|---|---|---|
| `click` | `selector` | `tag, text, dialog?` |
| `fill` | `selector`, `value` (an empty string clears the field) | `mode`: `value` or `contenteditable` |
| `select` | `selector`, and exactly one of: `value` or `label` | `selected: {value, label}` |
| `press_key` | `key` (`Enter`, `Escape`, `Tab`, `ArrowDown`, `Control+A`…), `selector?` (focus it first) | `dialog?` |
| `scroll` | `selector` (scroll to the element) **or** `direction` (`up`/`down`/`left`/`right`) + `amount` (px, default 600) | `scrollX, scrollY` |
| `upload` | `selector` (must be `input[type=file]`), `files` (absolute paths) | `fileCount` |

`dialog?` has the form `{type, message}` and appears when the action caused a JS dialog to open (§8.6).

### 5.4 Waiting

| Action | Args | Returns `data` |
|---|---|---|
| `wait_for` | Exactly one of: `selector` (with `state`: `visible` by default, or `hidden`), `text` (a substring of the body's `innerText`), `urlContains`, `load: true` | `matched: true, elapsedMs` |

- The maximum wait time is taken from the request's `timeoutMs`. If it is exceeded, return `TIMEOUT`.
- With `selector`, matching no element yet means waiting rather than an error, so `wait_for` does not return `ELEMENT_NOT_FOUND`:
  - `visible`: done when the selector matches a rendered element: a non-empty box, not `display:none` or `visibility:hidden`. Unlike the snapshot (§8.3), `aria-hidden` does not count, since a modal often marks the page behind it `aria-hidden` while it stays on screen;
  - `hidden`: done when no element matches any more, or the matching element is hidden. A ref that no longer resolves to an element (removed, or the page moved to another document) also counts as hidden and does not return `STALE_REF`.
- Whenever a CSS selector matches more than one element, return `AMBIGUOUS_SELECTOR` immediately, in both states.

### 5.5 Screenshots

| Action | Args | Returns `data` |
|---|---|---|
| `screenshot` | `format` (`png` by default, or `jpeg`), `quality` (0–100, applies to jpeg only, default 80), `selector?`, `fullPage` (bool), `path?` | `path, sizeBytes, mimeType, width, height` |

- If `path` is not passed, the file is written to `~/.browser-bridge/artifacts/<session>-<timestamp>.<ext>`.
- A `path` passed by the caller must be an absolute path, because the daemon's working directory is not the agent's. The path is used verbatim: parent directories are created automatically, and an existing file is overwritten.
- `width` and `height` are the image's pixel size, which the daemon reads from the image header (device pixels, so twice the CSS size on a 200% display).

### 5.6 Network

| Action | Args | Returns `data` |
|---|---|---|
| `network_start` | `filter?` (substring of the URL) | — (starts capturing again from scratch with an empty buffer) |
| `network_requests` | `filter?` | `capturing, count, requests: [{requestId, url, method, status, mimeType, completed}]` |
| `network_request_detail` | `requestId` | `request: {url, method, headers, postData}`, `response: {status, headers, mimeType}`, `body`, `bodyBase64Encoded`, `bodyError?` |
| `network_stop` | — | — (clears the buffer) |

- Capture is per tab: the session's current tab, starting from the moment `network_start` is called.
- The buffer holds at most 500 requests per tab; when full, the oldest request is dropped.
- A body is at most 10 MB; if larger, `bodyError` is returned.

### 5.7 Dialogs and the low-level escape hatches

| Action | Args | Returns `data` |
|---|---|---|
| `handle_dialog` | `accept` (bool), `promptText?` | `type, message` |
| `evaluate` | `code` | `type, value` |
| `cdp` | `method`, `params?` | the raw CDP response |

- `evaluate`:
  - runs in the page's **main world**, with `replMode: true` (allows top-level `await` and redeclaring `const`/`let` between calls), `awaitPromise`, `returnByValue`;
  - if the script throws an exception, return `EVAL_ERROR`;
  - a result larger than 4 MB after serialization also returns `EVAL_ERROR`.
- `cdp`: runs on the current tab. Methods under `Browser.*` and `Target.*` are blocked; calling them returns `CDP_ERROR`.

## 6. Sessions, tabs and refs

### 6.1 Sessions

- One session corresponds to one task and one tab group.
- Each session has one **current tab**. Every single-tab action runs on that tab; if there is none, return `NO_CURRENT_TAB`.
- Commands within the same session run sequentially through a queue in the daemon. Different sessions run in parallel.
- The state in the extension consists of: `{session → groupId, tabIds, currentTabId, borrowedTabIds}`, stored in `chrome.storage.session`, and the ref counter of each tab (§6.2).
- When the user closes a tab that belongs to a session, that tab is removed from the state (by listening to `tabs.onRemoved`). If it was the current tab, the session has no current tab.
- **Cleanup commands always work:** `list_tabs`, `close_tab`, `close_session` are never blocked by `DIALOG_OPEN`, `DETACHED_BY_USER` or `BLOCKED_HOST`, so the agent can always get out of a stuck tab.
- **MCP:** each `bridge mcp` process generates a session named `mcp-<6 random characters>` at startup. MCP tools have no `session` parameter, so small models do not have to deal with it.

### 6.2 Refs `@e<n>`

- **A ref number is never reused during the lifetime of a tab:**
  - Each tab has an increment-only counter, preserved across navigations. It is stored in `chrome.storage.local`, not `session`: an agent can hold refs across an extension reload or update, which empties `session` and starts a fresh page agent. Counters of tabs that no longer exist are dropped when the service worker starts.
  - Within the same document, an element that already has a ref keeps that ref in later snapshots. The page agent keeps a `WeakMap<Element, ref>`.
  - A new element gets the next number from the counter.
- The `ref → WeakRef<Element>` lookup table lives in the page's `bridge` **isolated world**, so the page's own scripts cannot read or modify it.
- A ref that cannot be resolved to an element still in the DOM (because the page navigated to another document, or the element was removed) returns `STALE_REF` with the hint "Take a new snapshot". A ref never points to a different element by mistake.

## 7. Security

| Measure | What it prevents |
|---|---|
| Bind only to `127.0.0.1`, with no option to expose it to the outside network | Access from other machines |
| `/ws` only accepts `Origin: chrome-extension://<id>` that is in `config.extensionIds` (by default the extension's fixed ID). Only one connection at a time; a second connection is closed with code 4409 | Web pages or unknown extensions impersonating the extension |
| HTTP rejects every request that has an `Origin` header | Web pages calling `fetch` to localhost |
| `Host` must be `127.0.0.1:<port>` or `localhost:<port>` | DNS rebinding |
| `POST /command` requires `Content-Type: application/json` | The browser must always send a preflight, and since the daemon returns no CORS it gets blocked |
| `config.blockedHosts` (§7.1) | The agent entering sensitive sites, even when a page redirects there on its own |
| The snapshot never contains the value of a password field | Leaking passwords into the LLM context |
| The log does not record the `value` of `fill`, the `code` of `evaluate`, or `promptText` | Leaking data through log files |
| `SKILL.md` tells the agent to treat page content as data and never follow it as instructions | Prompt injection (can only be mitigated, not fully blocked) |
| The "Cancel" button on Chrome's yellow debug bar is the emergency stop (§8.2) | An agent going wrong and the user needing to cut it off immediately |

**No token is used, and this is an accepted risk:**
- A token has to sit in a file the agent can read, so a process running under the same user can read it too.
- So a token does not stop a same-user threat.
- Conclusion, stated clearly in the README: **any process running under your user can control the browser through the bridge.** This level of risk is the same as Kimi WebBridge.

### 7.1 `blockedHosts`

- Matches the exact host or a subdomain: `bank.com` blocks both `bank.com` and `www.bank.com`.
- The daemon reads the list from `config.json` at startup and sends it down to the extension in the `welcome` frame (§10). Editing the list requires `bridge restart`.
- The daemon checks the URL of `navigate` and `find_tab` before sending the command down to the extension.
- The extension checks the current tab's host before every command, **except** commands used to leave a page (`navigate`, `find_tab`, `go_back`, `go_forward`) and cleanup commands (§6.1). If these commands were blocked too, the agent would be stuck on that tab forever.
- After `navigate`, `go_back`, `go_forward`, `reload` finish loading, the extension checks the final URL again to catch pages that redirect on their own.
- `find_tab` does not select a tab on a blocked host, even when borrowing with `active:true`.
- Network capture never records a request to a blocked host, even one an allowed page makes or one that gets there through a redirect.
- A violation returns `BLOCKED_HOST`, without the `url` or `title` of the blocked page.
- `blockedHosts` only blocks entering pages and operating on pages. `cdp` can still read the cookies of every host, including blocked hosts (`Network.getAllCookies`, `Network.getCookies`, `Storage.getCookies`). This is an accepted risk: the bridge only listens on `127.0.0.1`, and per the decision above not to use a token, any process running under your user can already control the browser.

## 8. Extension

### 8.1 Manifest and service worker lifecycle

- **Permissions:** `debugger`, `tabs`, `tabGroups`, `storage`, `sidePanel`, `alarms`. No `host_permissions` are needed.
- **Fixed ID:** the manifest has a `key` field (the public key is committed in the repo), so the ID stays the same when loaded unpacked in both Chrome and Edge.
- **Keeping the SW alive:** as long as a `chrome.debugger` session is attached, Chrome does not shut down the SW (since Chrome 118). Add a WebSocket ping every 20 seconds.
- **Reconnecting:** backoff from 1 second up to a maximum of 30 seconds. Add a `chrome.alarms` every 30 seconds to wake the SW and retry connecting when the daemon is not running.
- **When the SW restarts:**
  - re-read the state from `chrome.storage.session`;
  - cross-check with `chrome.debugger.getTargets()` to learn which tabs are still attached.

  Requests in flight are lost. When the daemon side sees the connection close, it returns `EXTENSION_NOT_CONNECTED` for every pending request.

### 8.2 CDP executor

- **Attach:** the first command on a new tab attaches (`chrome.debugger.attach`, version `1.3`), and the attachment is kept until the tab is closed, released, or `close_session` is called.
- **Right after attaching:** enable `Page.enable`, `Runtime.enable`, `Emulation.setFocusEmulationEnabled {enabled:true}`. The last step lets background tabs still receive real input.
- **On detach (`chrome.debugger.onDetach`):**

  | Reason | Handling |
  |---|---|
  | `canceled_by_user` (the user clicked Cancel on the yellow bar) | The session enters a stopped state. Every command other than cleanup commands (§6.1) returns `DETACHED_BY_USER`, until the agent calls `navigate` or `find_tab` |
  | `target_closed` | Remove the tab from the state |

### 8.3 Page agent

- **Injection:** an IIFE bundle (WXT unlisted script `page-agent.js`), which the service worker reads from the extension package and puts into the page with `Page.createIsolatedWorld {frameId: <main frame>, worldName: "bridge"}` followed by `Runtime.evaluate`.
  - Injected once per document (marked by `globalThis.__bridge`).
  - The service worker remembers each tab's world; `Runtime.executionContextDestroyed`/`executionContextsCleared` and a main-frame `Page.frameNavigated` forget it, and the next command makes a new one. A command that loses the race with a navigation retries once in the new document.
  - The world is addressed by its `uniqueContextId` (from `Runtime.executionContextCreated`), not its `contextId`: context ids are reused across renderer processes, so a stale one could name a context of the next site.
  - Making a world re-checks the frame's URL against `blockedHosts` (§7.1): the router checked the tab before the command, but a navigation may have committed since.
- **Page agent API:** `globalThis.__bridge.call(method, args)` answers `{value}` or `{error: {code, message, hint}}`, never throws. Methods: `snapshot`, `clickPoint` (scroll, actionability, the point to click), `prepareFill`, `hasText`, `select`, `focus`, `scroll`, `clip` (an element's box for `screenshot`), `fileInput`, `check` (one poll of `wait_for`). `__bridge.take()` hands the element `fileInput` checked to CDP as a remote object.
- **Snapshot:**
  - Walks the flat tree: open shadow roots, and slots by their assigned nodes.
  - Skips elements that are `display:none`, `aria-hidden`, or have no box at all (closed `<details>`, `content-visibility:hidden`). An element that is `visibility:hidden` or zero-sized gets no line of its own, but its children are still walked, since they can be visible.
  - Role and accessible name come from `dom-accessibility-api`. Input types it leaves without a role get one: `password` and the date/time types are `textbox`, `file` is `fileinput` (it is not a button to click: that would open the native file chooser).
  - Only elements that matter to an agent get a line: interactive ones (with a ref) and structural roles (headings, lists, landmarks, tables, dialogs …). Generic containers are left out and their content moves up a level. Text runs inside a block become one `- text` line.
  - An element is interactive when its role is (button, link, textbox …), when it is a native control, has `tabindex >= 0`, `onclick`, is contenteditable, or sets `cursor: pointer` itself (sites build buttons from divs). The last ones have no ARIA role and are shown as `clickable`.
  - A button, link, heading or cell with nothing interactive inside is one line, its text as its name. A `<select>` lists its options below it.
  - A `<label>` whose control is visible is not repeated: the control's line carries its text. When the control is hidden (a checkbox styled through its label), the label stands in for it: `- checkbox "Notify me" [checked] @e9`. The ref clicks the label, and `fill`, `select` and `upload` given the label's ref act on its control (a file input hidden behind a styled "Upload" label is common).
  - State includes: `checked`, `disabled`, `expanded`, `selected`, `level`, `value`. For password fields specifically, `value` is never included.
  - The walk stops once the output is surely longer than `maxChars`, so a huge page costs no more than the part returned.

### 8.4 Real input via CDP

| Action | How it works |
|---|---|
| `click` | `scrollIntoView({block:"center"})` → check the element: still in the DOM, has size, not disabled, `elementFromPoint(center)` is the element itself or its descendant. If not, return `ELEMENT_NOT_INTERACTABLE` with a description of the element covering it → `Input.dispatchMouseEvent` (`mouseMoved`, `mousePressed`, `mouseReleased`) at the element's center. A file input, or a label for one, is refused with a hint to use `upload`: the click would open the native file chooser in front of the user. The `text` returned is the element's text, never a field's value |
| `fill` | focus → select all content (`select()` for input/textarea, the Selection API for contenteditable) → `Input.insertText(value)`, or press Delete when `value` is empty. For contenteditable, read the text back after inserting, ignoring whitespace (editors turn line breaks into paragraphs); if it differs, select all and insert again once; if it still differs, return `INTERNAL`. Inputs that take no typing (`date`, `time`, `color`, `range` …) get the value set directly, then `input` and `change` |
| `select` | Set `value` on the `<select>` from the isolated world → fire the `input` and `change` events (bubbles) |
| `press_key` | `Input.dispatchKeyEvent`: modifiers down, the key (`keyDown` with `text` when it types something, which also fires `keypress`; `rawKeyDown` otherwise), key up, modifiers up. Key combinations are split on the `+` sign. `Enter` types `
`, so it submits forms; with Control, Alt or Meta held a key types nothing |
| `scroll` | `scrollIntoView` with `selector`, or `window.scrollBy` |
| `upload` | Get the element's `objectId` from the isolated world → `DOM.setFileInputFiles {objectId, files}`. The daemon checks that every path in `files` exists before sending down |

### 8.5 The remaining actions

- **`navigate`:** check the URL and `blockedHosts` → `chrome.tabs.create({active:false})` or `chrome.tabs.update` → wait for `Page.loadEventFired` → check `blockedHosts` again with the final URL (§7.1). Create the tab group with `chrome.tabs.group` + `tabGroups.update({title})`.
- **`wait_for`:** `MutationObserver` plus 100ms polling in the page agent. `urlContains` and `load` specifically are watched in the service worker.
- **`screenshot`:** `Page.captureScreenshot`.
  - With `selector`, use a `clip` matching the element's frame (after `scrollIntoView`).
  - With `fullPage`, enable `captureBeyondViewport: true`.
  - The image is sent to the daemon as base64. The daemon decodes it and writes it to a file.
- **Network:** `Network.enable` when `network_start` is called, reading the events `requestWillBeSent`, `responseReceived`, `loadingFinished`, `loadingFailed`. The body is only fetched with `Network.getResponseBody` when `network_request_detail` is called. `Network.disable` on `network_stop`.

### 8.6 JS dialogs

- Listen to `Page.javascriptDialogOpening` and store the open dialog per tab.
- If a dialog pops up **while** a `click` or `press_key` is running, the action stops waiting for CDP to return (CDP would hang until the dialog is closed) and immediately returns `ok` with `dialog: {type, message}`.
- `fill` and `select` can make the page open a dialog too (from a focus, input or change handler), but their results have no `dialog` field: they return `DIALOG_OPEN` with the message in the hint, instead of hanging until `TIMEOUT`.
- While a tab has an open dialog, every command other than `handle_dialog` and cleanup commands (§6.1) returns `DIALOG_OPEN` with `{type, message}` in the hint.
- `handle_dialog` when there is no dialog returns `NO_DIALOG`.

### 8.7 Side panel (React)

Consists of four parts:
- connection status: daemon address, daemon and extension versions;
- the list of sessions, the tabs of each session, and the current tab;
- a log of the last 50 commands: action, selector, run time, error code;
- a field to configure the daemon address, default `ws://127.0.0.1:9876/ws`.

## 9. Daemon and CLI

### 9.1 Home directory: `%USERPROFILE%\.browser-bridge\`

```text
bin\bridge.exe
extension\          unpacked copy installed by install.ps1; fixed path, so Load unpacked is needed only once
config.json         {"addr": "127.0.0.1:9876", "blockedHosts": [], "extensionIds": ["<id>"]}
daemon.pid          daemon.addr          (deleted when the daemon exits)
logs\daemon.log     logs\daemon.log.prev
artifacts\
```

- Address precedence: `--addr` > `config.json` > the default `127.0.0.1:9876`. Port 9876 differs from Kimi's 10086, so the two can run side by side.
- If `config.json` has a syntax error, every subcommand reports the error and names the file. It never silently falls back to default values.

### 9.2 Subcommands

| Command | Description |
|---|---|
| `serve` | Run the daemon in the foreground |
| `start` | Run `serve` as a detached process (`DETACHED_PROCESS` on Windows), wait for `/status` to return ok, then print the address. If it is already running, do nothing |
| `stop` / `restart` | Call `POST /shutdown` and wait up to 5 seconds for the daemon to exit. If the daemon hangs, kill it by the `pid` taken from `/status`, never by the `daemon.pid` file (a stale PID may now belong to another process), and only when that process's exe has the same name as `bridge` itself (anyone listening at that address can claim any `pid`). If no daemon answers, delete the leftover `daemon.pid`/`daemon.addr`. Exit code 0 when stopped or when it was not running to begin with |
| `status` | Print JSON identical to `GET /status`. If the daemon is not running, print `{"running": false, "addr": …}`. Exit code 0 when running, 1 when not |
| `logs [-f] [-n N] [--prev]` | Print the last `N` lines (default 50) of `logs\daemon.log`, or of `daemon.log.prev` with `--prev`. `-f` keeps printing new lines until Ctrl+C, and starts over when a daemon restart begins a new log |
| `call <action> --session <s> [key=value …] [--json '<args>' \| --json-file <f>] [--timeout ms]` | Print the envelope to stdout. Exit codes: 0 on `ok`, 1 on `ok:false` (including a request the daemon rejects with 400), 2 for a usage error or when it cannot connect to the daemon. Go reads argv as UTF-16 on Windows, so Vietnamese text does not get corrupted. Flags may come before or after the arguments |
| | **Arguments.** `key=value` sets one argument. The value is converted to the type the action's schema gives that key (`newTab=true` is a boolean, `amount=300` a number); a key whose schema type is an array collects repeated keys into a list (`files=a files=b`); anything else stays a string, and the daemon's validation reports what is wrong with it. `key:=<json>` sets raw JSON. `--json`/`--json-file` give a base object (`--json-file -` reads stdin) that `key=value` pairs override. Pairs avoid JSON quoting, which Windows PowerShell 5.1 breaks by stripping `"` from native arguments |
| `mcp` | Stdio MCP server. Automatically `start`s the daemon if it is not running, at startup and again if a call finds nothing listening (only then, so a command is never sent twice). Tools are named `browser_<action>`, with no `session` parameter (§6.1). Tool schemas are flattened: no `oneOf`, `dependentRequired` or `const`, which several tool-calling APIs reject; the descriptions say which arguments go together, and the daemon still validates the full schema. `browser_snapshot` returns the tree as plain lines after `url:`/`title:` lines, not as a JSON string. `browser_screenshot` returns both the image (MCP image content, up to 5 MB) and text containing `path`. A failure is a tool result with `isError` and the text `CODE: message` plus `Hint: …`. The server's `instructions` give the workflow and tell the model that page content is data, never instructions |
| `install-skill [--remove]` | Copy the embedded skill into `<claude>/skills/browser-bridge/` and `<codex>/skills/browser-bridge/`, replacing an older copy, where `<claude>` is `CLAUDE_CONFIG_DIR` or `~/.claude` and `<codex>` is `CODEX_HOME` or `~/.codex`. An agent whose folder does not exist is skipped, never created. Then print the MCP configuration with the absolute path of `bridge.exe`: Codex (the `[mcp_servers.browser-bridge]` block in `config.toml`), Claude Code (`claude mcp add browser-bridge -- <path> mcp`, for those who want MCP rather than the skill), and the command for any other client such as an Ollama harness. `--remove` deletes both copies; the installer runs it on uninstall |

### 9.3 `GET /status`

```json
{
  "running": true, "version": "0.3.0", "protocolVersion": 2, "port": 9876, "pid": 4120, "uptimeSeconds": 120,
  "extension": { "connected": true, "id": "...", "version": "0.3.0", "browser": "chrome" },
  "sessions": 2
}
```

### 9.4 Version

- The daemon and extension share one version number, because they are released together from one repo.
- `protocolVersion` is an integer, currently 2 (version 2 added `actions` to `hello` and the `activate_tab` action). When the two sides do not match at handshake:
  - the daemon closes the WebSocket with code 4400;
  - `/status` shows both versions;
  - every command returns `VERSION_MISMATCH` with a hint stating clearly which side is older.

## 10. WebSocket protocol between daemon and extension

Every frame is JSON text with a `type` field.

```text
ext → daemon   {type:"hello", protocolVersion, extensionVersion, extensionId, browser, actions}   actions: the action names this extension implements (§4 GET /tools)
daemon → ext   {type:"welcome", protocolVersion, daemonVersion, blockedHosts}   the extension uses blockedHosts to check commands (§7.1)
daemon → ext   {type:"request", id, session, action, args, deadline}     deadline: epoch ms
ext → daemon   {type:"response", id, ok, data | error}
ext → daemon   {type:"event", name, data}      tab.closed, dialog.opened, debugger.detached
ext → daemon   {type:"ping"}   every 20 seconds;     daemon → ext  {type:"pong"}
```

- The daemon limits each frame to at most 64 MB (a full-page screenshot as base64).
- In the MVP, events are only used for the log and the side panel, and are not yet exposed to the agent.

## 11. Error codes

| Code | When |
|---|---|
| `INVALID_REQUEST` | Wrong schema, wrong session regex, upload file does not exist, path is not absolute |
| `UNKNOWN_ACTION` | The action is not in the list |
| `FORBIDDEN` | Fails the security checks (§7): wrong `Origin`, `Host` or `Content-Type` |
| `EXTENSION_NOT_CONNECTED` | No extension is connected yet, or the connection was lost midway |
| `VERSION_MISMATCH` | `protocolVersion` mismatch |
| `NO_CURRENT_TAB` | The session has no current tab |
| `TAB_NOT_FOUND` | `find_tab` found nothing, or the tab has been closed |
| `STALE_REF` | The ref no longer points to any element in the DOM (except `wait_for` with `state: hidden`, §5.4) |
| `ELEMENT_NOT_FOUND` | The CSS selector matches no element (except `wait_for`, §5.4) |
| `AMBIGUOUS_SELECTOR` | The CSS selector matches more than one element |
| `ELEMENT_NOT_INTERACTABLE` | The element is hidden, disabled, covered, or of the wrong type (for example `upload` into something that is not a file input) |
| `NAVIGATION_FAILED` | An error while navigating, or there is no history to go back/forward |
| `RESTRICTED_URL` | `chrome://`, `edge://`, the Web Store, unsupported schemes |
| `BLOCKED_HOST` | The URL to open, or the URL of the current tab, has a host in `blockedHosts` (§7.1) |
| `DIALOG_OPEN` | The tab has a JS dialog that has not been handled |
| `NO_DIALOG` | `handle_dialog` when there is no dialog |
| `DETACHED_BY_USER` | The user clicked Cancel on the yellow debug bar |
| `TIMEOUT` | `timeoutMs` exceeded |
| `EVAL_ERROR` | The script threw an exception, or the result is too large |
| `CDP_ERROR` | CDP returned an error, or the method is blocked |
| `INTERNAL` | An unexpected error. The message contains details for debugging |

## 12. Testing

- **Go (`go test`):**
  - schema validation;
  - the session queue and timeouts;
  - the Origin, Host and Content-Type checks;
  - failing all pending requests when the extension loses its connection;
  - `/tools` and the MCP tool list.

  Use a **fake extension written in Go** (a WebSocket client) to test the WebSocket hub without opening a browser.
- **E2E (Playwright, TypeScript):**
  - Runs on Chromium or Chrome for Testing. Regular Chrome since 137 has dropped the `--load-extension` flag.
  - Load the extension with `launchPersistentContext` using `--load-extension`, start `bridge serve` on a random port, and issue commands over HTTP.
  - The E2E daemon runs on port 19876, and the extension is built with `--mode e2e` with that address compiled in (`WXT_DAEMON_URL`), so a test run never connects to the real daemon at 9876.
  - Each action has at least one test for success and one for failure.
- **`testpage/`:**
  - the kinds of input fields: input, textarea, contenteditable (a ProseMirror-style editor), React controlled input, `<select>`, checkbox, radio, input file;
  - a link to a second page (to test back/forward);
  - buttons that trigger `alert`, `confirm`, `prompt`;
  - an element that appears after 1 second and an element that disappears after 1 second (to test `wait_for` with `visible` and `hidden`);
  - two buttons with the same class (to test `AMBIGUOUS_SELECTOR`);
  - a link that redirects to a host in the test suite's `blockedHosts` (to test `BLOCKED_HOST` when the page redirects on its own);
  - a button covered by an overlay (to test `ELEMENT_NOT_INTERACTABLE`);
  - an element removed from the DOM (to test `STALE_REF`);
  - a `fetch` to a local JSON API (to test network);
  - an iframe (to check that the snapshot lists frames);
  - a password field (to check that the snapshot does not leak the value).
- **Manual smoke test** following the checklist of the 4 use cases (§1.1) on a real, logged-in Chrome, with each consumer (§1.2).

## 13. Implementation order

0. **Spike (about 1 day, throwaway code once done):** on a background tab with focus emulation enabled, verify three things:
   - whether `Input.dispatchMouseEvent` reaches the element;
   - whether `Input.insertText` works with plain inputs, React controlled inputs and contenteditable editors;
   - whether `Page.captureScreenshot` can capture.

   If it fails, change direction: `click` uses `el.click()`, `fill` uses the native setter plus synthetic events, and tabs are opened with `active:true`. Update the spec before continuing.

   **Result (2026-10-06, Chromium 153 installed by Playwright, extension loaded with `--load-extension`, Windows 11):** all three work on a background tab, with no change of direction needed. Every check (click, typing into a plain input, React controlled input, ProseMirror, `Enter`, screenshot) PASSED, and the background tab stayed in the background, without stealing focus from the user's tab. Run 5 times, 4 were completely clean. In the remaining run, on a background tab with focus emulation, selecting all ProseMirror content with the Selection API and then calling `Input.insertText` inserted additional text instead of replacing it ("pm okpm replaced"). So `fill` for contenteditable must read the content back after inserting, and retry once if it does not match (§8.4). Not yet verified: everyday stable Chrome, and a window that has lost focus or is minimized. These two need the spike to be rerun manually on real Chrome before release.
1. Go structs for the protocol, `schemagen`, the daemon skeleton (HTTP, security checks, WebSocket hub, session queue), tested with the fake extension.
2. Extension skeleton: connection, handshake, keepalive, reconnect, session/tab group, `navigate`/`find_tab`/`list_tabs`/`close_*`/back/forward/reload, a side panel showing status.
3. Page agent: snapshot and ref, then `click`, `fill`, `select`, `press_key`, `scroll`, `wait_for`, together with the testpage and E2E.
4. `screenshot`, `upload`, dialogs, network, `evaluate`, `cdp`.
5. CLI: `start`/`stop`/`status`/`logs`/`call`, `mcp`, `SKILL.md`, `install-skill`.
6. Polish: `blockedHosts`, hiding sensitive data in logs, smoke tests with the 3 consumers.

## 14. Risks

| Risk | Mitigation |
|---|---|
| Real input via CDP does not reach background tabs | The 2026-10-06 spike passed on Chromium 153 (§13 step 0). Still to be confirmed on stable Chrome when the window loses focus or is minimized |
| The yellow "debugging this browser" bar is annoying | Accepted, since it is also the signal that a tab is being controlled and serves as the emergency stop. Do not use the `--silent-debugger-extension-api` flag |
| Small local models call tools incorrectly | One flat schema per tool, a `hint` in errors, and MCP does not require passing a session |
| Prompt injection from page content | There is `blockedHosts`, `SKILL.md` warns the agent, and the Cancel button stops it immediately. It cannot be fully blocked |
| Pages that block CDP or detect automation | Out of scope for the MVP. Record it when encountered |
