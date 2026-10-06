# Browser Bridge — Design Spec

- **Ngày:** 2026-10-06
- **Trạng thái:** Draft, chờ review
- **Tên tạm:** project `browser-bridge`, binary `bridge`

## 1. Mục tiêu

Một bản tự chủ thay cho Kimi WebBridge: cài extension vào Chrome/Edge rồi để **bất kỳ agent nào** (Claude Code, Codex, harness chạy model Ollama local) điều khiển trình duyệt thật của người dùng, với các session đăng nhập sẵn có. Không cần tài khoản bên thứ ba, toàn bộ source do mình nắm.

### 1.1 Use case phải đạt trong MVP

1. **Đọc trang cần đăng nhập:** mở trang, snapshot, đọc nội dung, chụp màn hình.
2. **Thao tác form:** click, điền text (kể cả React controlled input và rich text editor), chọn dropdown, nhấn phím, upload file.
3. **Bắt network:** xem request/response (headers và body) của trang.
4. **Làm nền để viết CLI riêng cho từng site:** HTTP API ổn định, `evaluate` chạy trong main world của trang, chọn element bằng CSS selector.

### 1.2 Tiêu chí hoàn thành MVP

Cả 4 use case chạy được trên Chrome hằng ngày của người dùng (Windows), qua 3 consumer:

- Claude Code, dùng `SKILL.md`;
- Codex, dùng MCP;
- một harness chạy model Ollama local, dùng MCP.

### 1.3 Không làm trong MVP

- Chat AI trong side panel (side panel chỉ để xem trạng thái và debug).
- Thao tác bên trong iframe (snapshot chỉ *liệt kê* các frame), `save_as_pdf`, hover.
- Nhiều trình duyệt kết nối cùng lúc.
- Recorder/workflow, agent loop.
- Publish extension lên store (chỉ load unpacked). Cách phát cho máy khác nằm ở spec installer (`2026-10-06-installer-design.md`).
- Token xác thực (lý do ở §7).
- Chính thức hỗ trợ macOS/Linux. Code Go viết portable nhưng chỉ test trên Windows.

## 2. Kiến trúc

```text
Claude Code ──┐ bridge call / curl (theo SKILL.md)
Codex ────────┼───────────────────────────▶ HTTP 127.0.0.1:9876   ┐
Ollama harness┘ MCP stdio: `bridge mcp` ──HTTP──▶                  │ bridge daemon (Go)
                                                                    │  · validate theo JSON Schema
                                                                    │  · route theo session
                                                                    │  · timeout, mã lỗi chuẩn
                                                                    │  · ghi file screenshot
                                                                    ┘
                                      ▲ WebSocket /ws (extension chủ động kết nối)
                                      │
                         Extension MV3 (TypeScript, WXT)
                          · service worker: WS client, session/tab group,
                            executor CDP (chrome.debugger)
                          · page agent inject vào isolated world: snapshot, ref, actionability
                          · side panel (React): trạng thái, log
```

**Ranh giới trách nhiệm:**
- Daemon không đụng vào CDP hay DOM. Nó chỉ validate request, route tới extension, quản lý timeout và ghi file artifact.
- Mọi thao tác với trình duyệt nằm trong extension.
- Agent chỉ biết HTTP API (hoặc tool MCP). Agent không biết gì về Chrome API, CDP hay WebSocket.

## 3. Cấu trúc repo

```text
browser-bridge/
├── daemon/                      Go module, build ra binary `bridge`
│   ├── cmd/bridge/              các subcommand (§9)
│   ├── cmd/schemagen/           sinh schema/ và bảng tool trong SKILL.md từ Go struct
│   └── internal/
│       ├── protocol/            Go struct + mô tả cho từng action: nguồn schema duy nhất
│       ├── server/              HTTP API, kiểm tra security, WebSocket hub
│       ├── session/             hàng đợi theo session, map request id ↔ response đang chờ
│       ├── mcp/                 MCP server stdio (Go SDK chính thức), gọi về HTTP API
│       └── home/                thư mục ~/.browser-bridge, config, pid, log
├── schema/                      JSON Schema sinh ra từ Go, có commit vào repo
├── extension/                   WXT + TypeScript
│   └── src/
│       ├── entrypoints/         điểm vào của WXT: background.ts, sidepanel/ (React)
│       ├── background/          logic của service worker: connection, router, sessions, cdp, actions/
│       ├── page-agent/          bundle IIFE inject vào trang
│       ├── shared/              type và key storage dùng chung giữa service worker và side panel
│       └── generated/           type TS sinh từ schema/
├── skill/browser-bridge/SKILL.md
├── testpage/                    trang HTML tĩnh + API giả để test
└── e2e/                         Playwright (TypeScript)
```

### 3.1 Codegen (định nghĩa viết một lần)

```text
daemon/internal/protocol (Go struct + description)
   └─ go run ./daemon/cmd/schemagen
        ├─▶ schema/protocol.schema.json ──json-schema-to-typescript──▶ extension/src/generated/protocol.ts
        ├─▶ danh sách tool cho GET /tools và MCP (Go đọc trực tiếp từ protocol package lúc runtime)
        └─▶ bảng tool trong SKILL.md (nằm giữa hai marker <!-- tools:begin --> / <!-- tools:end -->)
```

- Daemon validate request bằng JSON Schema đã sinh (`santhosh-tekuri/jsonschema`).
- Extension chỉ nhận lệnh từ daemon, nên chỉ dùng type TS, không validate lại lúc runtime.
- Một script `check:gen` chạy lại codegen và báo lỗi nếu có file sinh ra khác với bản đã commit.

## 4. HTTP API

| Endpoint | Mô tả |
|---|---|
| `POST /command` | Chạy một action |
| `GET /tools` | `[{name, description, inputSchema}]`, dùng được làm function definition cho bất kỳ LLM nào |
| `GET /status` | Trạng thái daemon và extension (§9.3) |
| `GET /ws` | WebSocket, chỉ dành cho extension (§6) |
| `POST /shutdown` | Daemon trả 200, tắt êm và tự xoá `daemon.pid`/`daemon.addr`. Dùng cho `bridge stop`; qua cùng các kiểm tra security như `/command` |

### 4.1 Request và response

```json
{ "action": "click", "args": { "selector": "@e12" }, "session": "jira-report", "timeoutMs": 15000 }
```

- `session`: bắt buộc, khớp regex `^[a-z0-9][a-z0-9_-]{0,63}$`.
- `timeoutMs`: không bắt buộc, tối đa 120000. Mặc định 30000 cho `navigate`/`reload`/`go_back`/`go_forward`, 15000 cho các action còn lại.

```json
{ "ok": true,  "data": { } }
{ "ok": false, "error": { "code": "STALE_REF", "message": "@e12 is no longer in the page", "hint": "Take a new snapshot" } }
```

- `hint` không bắt buộc. Đó là gợi ý bước tiếp theo agent nên làm, viết cho cả model nhỏ hiểu được.
- HTTP status:

  | Status | Khi nào |
  |---|---|
  | 200 | Mọi kết quả đã xử lý được, kể cả `ok:false` |
  | 400 | JSON hỏng, sai schema, action không tồn tại |
  | 403 | Không qua được kiểm tra security (§7), mã lỗi `FORBIDDEN` |

  Mọi trường hợp đều trả đúng envelope trên.

## 5. Action trong MVP

`selector` là ref `@e<n>` hoặc CSS selector. Chuỗi bắt đầu bằng `@e` thì là ref, còn lại coi là CSS. CSS phải khớp **đúng một** element:
- không khớp element nào thì trả `ELEMENT_NOT_FOUND` (riêng `wait_for` thì chờ tiếp, §5.4);
- khớp hơn một element thì trả `AMBIGUOUS_SELECTOR`.

### 5.1 Tab và điều hướng

| Action | Args | Trả về `data` | Ghi chú |
|---|---|---|---|
| `navigate` | `url` (bắt buộc), `newTab` (bool, mặc định false), `groupTitle` | `tabId, url, title` | Session chưa có tab hiện tại thì luôn tạo tab mới. Tab mới mở ở nền (`active:false`). `groupTitle` chỉ dùng khi tạo group, mặc định lấy tên session. Chỉ chấp nhận `http`, `https`, `about:blank`. Chờ load event xong mới trả |
| `find_tab` | `url` (khớp theo host, `kimi.com` khớp cả `www.kimi.com`, bỏ qua path), `active` (bool) | `tabId, url, title, borrowed` | Phải có `url`, `active:true`, hoặc cả hai. Mặc định chỉ tìm trong các tab của session. `active:true` thì mượn tab người dùng đang xem, tab đó không bị kéo vào group. Kết quả trở thành tab hiện tại của session |
| `list_tabs` | — | `tabs: [{tabId, url, title, current, borrowed}]` | Chỉ tab của session |
| `close_tab` | — | `closed, released` | Tab của session thì đóng. Tab đang mượn thì chỉ trả lại (detach, không đóng). Sau đó session không còn tab hiện tại |
| `close_session` | — | `closed` (số tab) | Đóng mọi tab của session, trả lại tab mượn, xoá group |
| `go_back` / `go_forward` / `reload` | — | `url, title` | Không có lịch sử để lùi/tiến thì trả `NAVIGATION_FAILED` |

### 5.2 Đọc

| Action | Args | Trả về `data` |
|---|---|---|
| `snapshot` | `maxChars` (mặc định 20000) | `url, title, tree, frames: [{frame, url, width, height}], truncated` |

Định dạng `tree`:
- Mỗi dòng có dạng `- <role> "<name>" [state…] @e<n>`, thụt lề 2 khoảng trắng cho mỗi cấp.
- Ref chỉ gắn cho element tương tác được.
- Text tĩnh hiện dưới dạng `- text "…"`, mỗi đoạn cắt ở 200 ký tự.

```text
- heading "Đăng nhập" [level=1]
- textbox "Email" [value="a@b.com"] @e3
- textbox "Mật khẩu" @e4
- checkbox "Ghi nhớ" [checked] @e6
- button "Tiếp tục" @e5
```

### 5.3 Thao tác

| Action | Args | Trả về `data` |
|---|---|---|
| `click` | `selector` | `tag, text, dialog?` |
| `fill` | `selector`, `value` (chuỗi rỗng là xoá trắng) | `mode`: `value` hoặc `contenteditable` |
| `select` | `selector`, và đúng một trong hai: `value` hoặc `label` | `selected: {value, label}` |
| `press_key` | `key` (`Enter`, `Escape`, `Tab`, `ArrowDown`, `Control+A`…), `selector?` (focus vào đó trước) | `dialog?` |
| `scroll` | `selector` (cuộn tới element) **hoặc** `direction` (`up`/`down`/`left`/`right`) + `amount` (px, mặc định 600) | `scrollX, scrollY` |
| `upload` | `selector` (phải là `input[type=file]`), `files` (đường dẫn tuyệt đối) | `fileCount` |

`dialog?` có dạng `{type, message}`, xuất hiện khi action làm bật dialog JS (§8.6).

### 5.4 Chờ

| Action | Args | Trả về `data` |
|---|---|---|
| `wait_for` | Đúng một trong: `selector` (kèm `state`: `visible` mặc định, hoặc `hidden`), `text` (chuỗi con trong `innerText` của body), `urlContains`, `load: true` | `matched: true, elapsedMs` |

- Thời gian chờ tối đa lấy theo `timeoutMs` của request. Quá hạn thì trả `TIMEOUT`.
- Với `selector`, chưa khớp element nào là đang chờ chứ không phải lỗi, nên `wait_for` không trả `ELEMENT_NOT_FOUND`:
  - `visible`: xong khi selector khớp một element đang hiển thị (cùng tiêu chí ẩn/hiện với snapshot, §8.3);
  - `hidden`: xong khi không còn element nào khớp, hoặc element khớp đang bị ẩn. Ref không còn tra ra element (bị xoá, hoặc trang đã sang document khác) cũng tính là đã ẩn, không trả `STALE_REF`.
- Lúc nào CSS khớp hơn một element thì trả `AMBIGUOUS_SELECTOR` ngay, ở cả hai state.

### 5.5 Chụp màn hình

| Action | Args | Trả về `data` |
|---|---|---|
| `screenshot` | `format` (`png` mặc định, hoặc `jpeg`), `quality` (0–100, chỉ áp dụng cho jpeg, mặc định 80), `selector?`, `fullPage` (bool), `path?` | `path, sizeBytes, mimeType, width, height` |

- Không truyền `path` thì file được ghi vào `~/.browser-bridge/artifacts/<session>-<timestamp>.<ext>`.
- `path` do caller truyền phải là đường dẫn tuyệt đối, vì thư mục làm việc của daemon không phải của agent. Đường dẫn được dùng nguyên văn: tự tạo thư mục cha, ghi đè nếu file đã tồn tại.

### 5.6 Network

| Action | Args | Trả về `data` |
|---|---|---|
| `network_start` | `filter?` (chuỗi con của URL) | — (bắt lại từ đầu với buffer rỗng) |
| `network_requests` | `filter?` | `capturing, count, requests: [{requestId, url, method, status, mimeType, completed}]` |
| `network_request_detail` | `requestId` | `request: {url, method, headers, postData}`, `response: {status, headers, mimeType}`, `body`, `bodyBase64Encoded`, `bodyError?` |
| `network_stop` | — | — (xoá buffer) |

- Bắt theo từng tab: tab hiện tại của session, tính từ lúc gọi `network_start`.
- Buffer giữ tối đa 500 request mỗi tab, đầy thì bỏ request cũ nhất.
- Body tối đa 10 MB, lớn hơn thì trả `bodyError`.

### 5.7 Dialog và lối thoát cấp thấp

| Action | Args | Trả về `data` |
|---|---|---|
| `handle_dialog` | `accept` (bool), `promptText?` | `type, message` |
| `evaluate` | `code` | `type, value` |
| `cdp` | `method`, `params?` | response CDP nguyên gốc |

- `evaluate`:
  - chạy trong **main world** của trang, bật `replMode: true` (cho phép `await` ở top-level và khai báo lại `const`/`let` giữa các lần gọi), `awaitPromise`, `returnByValue`;
  - script ném exception thì trả `EVAL_ERROR`;
  - kết quả sau khi serialize lớn hơn 4 MB cũng trả `EVAL_ERROR`.
- `cdp`: chạy trên tab hiện tại. Chặn các method thuộc `Browser.*` và `Target.*`, gọi vào thì trả `CDP_ERROR`.

## 6. Session, tab và ref

### 6.1 Session

- Một session tương ứng với một task và một tab group.
- Mỗi session có một **tab hiện tại**. Mọi action đơn tab đều chạy trên tab đó, chưa có thì trả `NO_CURRENT_TAB`.
- Lệnh trong cùng session chạy tuần tự qua một hàng đợi trong daemon. Các session khác nhau chạy song song.
- State trong extension gồm: `{session → groupId, tabIds, currentTabId, borrowedTabIds}` và bộ đếm ref của từng tab. State này lưu trong `chrome.storage.session`.
- Khi người dùng tự đóng một tab thuộc session, tab đó được gỡ khỏi state (lắng nghe `tabs.onRemoved`). Nếu đó là tab hiện tại thì session không còn tab hiện tại.
- **Lệnh dọn dẹp luôn chạy được:** `list_tabs`, `close_tab`, `close_session` không bao giờ bị chặn bởi `DIALOG_OPEN`, `DETACHED_BY_USER` hay `BLOCKED_HOST`, để agent luôn thoát được khỏi một tab đang kẹt.
- **MCP:** mỗi tiến trình `bridge mcp` tự sinh một session tên `mcp-<6 ký tự ngẫu nhiên>` lúc khởi động. Tool MCP không có tham số `session`, để model nhỏ không phải lo chuyện này.

### 6.2 Ref `@e<n>`

- **Số ref không bao giờ dùng lại trong suốt đời một tab:**
  - Mỗi tab có một bộ đếm chỉ tăng, lưu trong `chrome.storage.session` và giữ nguyên qua các lần navigate.
  - Trong cùng một document, element đã có ref giữ nguyên ref đó ở các snapshot sau. Page agent giữ một `WeakMap<Element, ref>`.
  - Element mới nhận số tiếp theo của bộ đếm.
- Bảng tra `ref → WeakRef<Element>` nằm trong **isolated world** `bridge` của trang, nên script của trang không đọc hay sửa được.
- Một ref không tra ra được element còn trong DOM (do đã navigate sang document khác, hoặc element bị xoá) thì trả `STALE_REF` kèm hint "Take a new snapshot". Ref không bao giờ trỏ nhầm sang element khác.

## 7. Security

| Biện pháp | Chặn được gì |
|---|---|
| Chỉ bind vào `127.0.0.1`, không có tuỳ chọn mở ra mạng ngoài | Truy cập từ máy khác |
| `/ws` chỉ chấp nhận `Origin: chrome-extension://<id>` có trong `config.extensionIds` (mặc định là ID cố định của extension). Mỗi lúc chỉ một kết nối, kết nối thứ hai bị đóng với code 4409 | Trang web hoặc extension lạ giả làm extension |
| HTTP từ chối mọi request có header `Origin` | Trang web gọi `fetch` tới localhost |
| `Host` phải là `127.0.0.1:<port>` hoặc `localhost:<port>` | DNS rebinding |
| `POST /command` bắt buộc `Content-Type: application/json` | Trình duyệt luôn phải gửi preflight, mà daemon không trả CORS nên bị chặn |
| `config.blockedHosts` (§7.1) | Agent đi vào trang nhạy cảm, kể cả khi trang tự chuyển hướng sang đó |
| Snapshot không bao giờ chứa giá trị ô password | Lộ mật khẩu vào context của LLM |
| Log không ghi `value` của `fill`, `code` của `evaluate`, `promptText` | Lộ dữ liệu qua file log |
| `SKILL.md` dặn agent coi nội dung trang là dữ liệu, không bao giờ làm theo như lệnh | Prompt injection (chỉ giảm thiểu được, không chặn hẳn) |
| Nút "Cancel" trên thanh vàng debug của Chrome là nút dừng khẩn cấp (§8.2) | Agent chạy sai, người dùng cần cắt ngay |

**Không dùng token, và đây là rủi ro đã chấp nhận:**
- Token phải nằm trong một file mà agent đọc được, nên tiến trình chạy dưới cùng user cũng đọc được.
- Như vậy token không chặn được mối đe doạ cùng user.
- Kết luận, ghi rõ trong README: **mọi tiến trình chạy dưới user của bạn đều điều khiển được trình duyệt qua bridge.** Mức rủi ro này giống Kimi WebBridge.

### 7.1 `blockedHosts`

- Khớp đúng host hoặc subdomain: `bank.com` chặn cả `bank.com` lẫn `www.bank.com`.
- Daemon đọc danh sách từ `config.json` lúc khởi động rồi gửi xuống extension trong frame `welcome` (§10). Sửa danh sách thì phải `bridge restart`.
- Daemon kiểm tra URL của `navigate` và `find_tab` trước khi gửi lệnh xuống extension.
- Extension kiểm tra host của tab hiện tại trước mỗi lệnh, **trừ** các lệnh dùng để rời trang (`navigate`, `find_tab`, `go_back`, `go_forward`) và lệnh dọn dẹp (§6.1). Nếu chặn cả những lệnh này thì agent bị kẹt vĩnh viễn trên tab đó.
- Sau khi `navigate`, `go_back`, `go_forward`, `reload` load xong, extension kiểm tra lại URL cuối cùng để bắt trường hợp trang tự chuyển hướng.
- `find_tab` không chọn tab ở host bị chặn, kể cả khi mượn bằng `active:true`.
- Vi phạm thì trả `BLOCKED_HOST`, không kèm `url` hay `title` của trang bị chặn.
- `blockedHosts` chỉ chặn việc vào trang và thao tác trên trang. `cdp` vẫn đọc được cookie của mọi host, kể cả host bị chặn (`Network.getAllCookies`, `Network.getCookies`, `Storage.getCookies`). Đây là rủi ro đã chấp nhận: bridge chỉ nghe trên `127.0.0.1`, và theo quyết định không dùng token ở trên, mọi tiến trình chạy dưới user của bạn vốn đã điều khiển được trình duyệt.

## 8. Extension

### 8.1 Manifest và vòng đời service worker

- **Permission:** `debugger`, `tabs`, `tabGroups`, `storage`, `sidePanel`, `alarms`. Không cần `host_permissions`.
- **ID cố định:** manifest có trường `key` (public key commit trong repo), nên ID giữ nguyên khi load unpacked trên cả Chrome lẫn Edge.
- **Giữ SW sống:** khi còn phiên `chrome.debugger` đang attach thì Chrome không tắt SW (từ Chrome 118). Thêm ping WebSocket mỗi 20 giây.
- **Kết nối lại:** backoff từ 1 giây lên tối đa 30 giây. Thêm `chrome.alarms` mỗi 30 giây để đánh thức SW và thử kết nối lại khi daemon chưa chạy.
- **Khi SW khởi động lại:**
  - đọc lại state từ `chrome.storage.session`;
  - đối chiếu với `chrome.debugger.getTargets()` để biết tab nào còn đang attach.

  Request đang chạy dở bị mất. Phía daemon thấy kết nối đóng thì trả `EXTENSION_NOT_CONNECTED` cho mọi request đang chờ.

### 8.2 Executor CDP

- **Attach:** lệnh đầu tiên trên một tab mới attach (`chrome.debugger.attach`, version `1.3`), và giữ attach cho tới khi tab bị đóng, bị trả lại hoặc `close_session`.
- **Ngay sau khi attach:** bật `Page.enable`, `Runtime.enable`, `Emulation.setFocusEmulationEnabled {enabled:true}`. Bước cuối để tab nền vẫn nhận được input thật.
- **Khi bị detach (`chrome.debugger.onDetach`):**

  | Lý do | Xử lý |
  |---|---|
  | `canceled_by_user` (người dùng bấm Cancel trên thanh vàng) | Session chuyển sang trạng thái dừng. Mọi lệnh khác ngoài lệnh dọn dẹp (§6.1) trả `DETACHED_BY_USER`, cho tới khi agent gọi `navigate` hoặc `find_tab` |
  | `target_closed` | Gỡ tab khỏi state |

### 8.3 Page agent

- **Inject:** một bundle IIFE, đưa vào trang bằng `Page.createIsolatedWorld {frameId: <main frame>, worldName: "bridge"}` rồi `Runtime.evaluate`.
  - Mỗi document inject một lần (đánh dấu bằng `globalThis.__bridge`).
  - Khi context bị huỷ thì tạo lại ở lệnh kế tiếp.
- **API của page agent:**
  - `snapshot(opts)`
  - `resolve(selector) → {ok, rect, objectId?}` hoặc lỗi
  - `checkActionable(selector)`
  - `waitFor(cond)`
- **Snapshot:**
  - Duyệt DOM, đi vào cả open shadow root.
  - Bỏ qua element `display:none`, `visibility:hidden`, `aria-hidden`, hoặc có kích thước 0.
  - Role và accessible name lấy từ `dom-accessibility-api`.
  - State gồm: `checked`, `disabled`, `expanded`, `selected`, `level`, `value`. Riêng ô password thì không bao giờ đưa `value` vào.

### 8.4 Input thật qua CDP

| Action | Cách làm |
|---|---|
| `click` | `scrollIntoView({block:"center"})` → kiểm tra element: còn trong DOM, có kích thước, không bị disable, `elementFromPoint(tâm)` là chính nó hoặc con của nó. Không đạt thì trả `ELEMENT_NOT_INTERACTABLE` kèm mô tả phần tử đang che → `Input.dispatchMouseEvent` (`mouseMoved`, `mousePressed`, `mouseReleased`) tại tâm element |
| `fill` | focus → chọn hết nội dung (`select()` với input/textarea, Selection API với contenteditable) → `Input.insertText(value)`, hoặc nhấn Delete khi `value` rỗng. Với contenteditable, đọc lại `textContent` sau khi chèn, lệch thì chọn hết và chèn lại một lần, vẫn lệch thì trả `INTERNAL` |
| `select` | Gán `value` cho `<select>` từ isolated world → bắn sự kiện `input` và `change` (bubbles) |
| `press_key` | `Input.dispatchKeyEvent` (`keyDown`, `char` nếu là ký tự in được, `keyUp`). Tổ hợp phím được tách theo dấu `+` |
| `scroll` | `scrollIntoView` với `selector`, hoặc `window.scrollBy` |
| `upload` | Lấy `objectId` của element → `DOM.describeNode` → `backendNodeId` → `DOM.setFileInputFiles`. Daemon kiểm tra mọi đường dẫn trong `files` có tồn tại trước khi gửi xuống |

### 8.5 Các action còn lại

- **`navigate`:** kiểm tra URL và `blockedHosts` → `chrome.tabs.create({active:false})` hoặc `chrome.tabs.update` → chờ `Page.loadEventFired` → kiểm tra lại `blockedHosts` với URL cuối cùng (§7.1). Tạo tab group bằng `chrome.tabs.group` + `tabGroups.update({title})`.
- **`wait_for`:** `MutationObserver` kèm polling 100ms trong page agent. Riêng `urlContains` và `load` thì theo dõi ở service worker.
- **`screenshot`:** `Page.captureScreenshot`.
  - Có `selector` thì dùng `clip` theo khung element (sau khi `scrollIntoView`).
  - Có `fullPage` thì bật `captureBeyondViewport: true`.
  - Ảnh gửi về daemon dạng base64. Daemon giải mã rồi ghi ra file.
- **Network:** `Network.enable` khi gọi `network_start`, đọc các event `requestWillBeSent`, `responseReceived`, `loadingFinished`, `loadingFailed`. Body chỉ lấy bằng `Network.getResponseBody` khi có `network_request_detail`. `Network.disable` khi `network_stop`.

### 8.6 Dialog JS

- Lắng nghe `Page.javascriptDialogOpening` và lưu dialog đang mở theo từng tab.
- Nếu dialog bật lên **trong lúc** đang chạy `click` hoặc `press_key`, action không chờ CDP trả về nữa (CDP sẽ treo cho tới khi dialog đóng), mà trả ngay `ok` kèm `dialog: {type, message}`.
- Khi tab đang có dialog mở, mọi lệnh khác ngoài `handle_dialog` và lệnh dọn dẹp (§6.1) đều trả `DIALOG_OPEN` kèm `{type, message}` trong hint.
- `handle_dialog` khi không có dialog nào thì trả `NO_DIALOG`.

### 8.7 Side panel (React)

Gồm bốn phần:
- trạng thái kết nối: địa chỉ daemon, version daemon và extension;
- danh sách session, tab của từng session, tab hiện tại;
- log 50 lệnh gần nhất: action, selector, thời gian chạy, mã lỗi;
- ô cấu hình địa chỉ daemon, mặc định `ws://127.0.0.1:9876/ws`.

## 9. Daemon và CLI

### 9.1 Thư mục home: `%USERPROFILE%\.browser-bridge\`

```text
bin\bridge.exe
extension\          bản unpacked do install.ps1 cài; đường dẫn cố định nên chỉ Load unpacked một lần
config.json         {"addr": "127.0.0.1:9876", "blockedHosts": [], "extensionIds": ["<id>"]}
daemon.pid          daemon.addr          (xoá khi daemon thoát)
logs\daemon.log     logs\daemon.log.prev
artifacts\
```

- Thứ tự ưu tiên của địa chỉ: `--addr` > `config.json` > mặc định `127.0.0.1:9876`. Port 9876 khác 10086 của Kimi nên hai bên chạy song song được.
- `config.json` sai cú pháp thì mọi subcommand báo lỗi và nêu tên file. Không âm thầm dùng giá trị mặc định.

### 9.2 Subcommand

| Lệnh | Mô tả |
|---|---|
| `serve` | Chạy daemon ở foreground |
| `start` | Chạy `serve` thành tiến trình detached (`DETACHED_PROCESS` trên Windows), chờ `/status` trả ok rồi in địa chỉ. Đã chạy rồi thì không làm gì |
| `stop` / `restart` | Gọi `POST /shutdown`, chờ tối đa 5 giây cho daemon thoát. Daemon treo thì kill theo `pid` lấy từ `/status`, không bao giờ theo file `daemon.pid` (PID cũ có thể đã thuộc tiến trình khác), và chỉ khi exe của tiến trình đó cùng tên với chính `bridge` (bất kỳ ai nghe ở địa chỉ đó đều có thể khai một `pid`). Không có daemon nào trả lời thì xoá `daemon.pid`/`daemon.addr` còn sót. Exit code 0 khi đã dừng hoặc vốn không chạy |
| `status` | In JSON y như `GET /status`. Daemon không chạy thì in `{"running": false, "addr": …}`. Exit code 0 khi đang chạy, 1 khi không |
| `logs [-f] [-n N] [--prev]` | Xem log |
| `call <action> --session <s> [--json '<args>' \| --json-file <f>] [--timeout ms]` | In envelope ra stdout. Exit code: 0 khi `ok`, 1 khi `ok:false`, 2 khi không kết nối được daemon. Go đọc argv dạng UTF-16 trên Windows nên text tiếng Việt không bị vỡ |
| `mcp` | MCP server chạy stdio. Tự `start` daemon nếu daemon chưa chạy. Tool tên `browser_<action>`, không có tham số `session` (§6.1). `browser_screenshot` trả cả ảnh (image content của MCP) lẫn text chứa `path` |
| `install-skill` | Copy `skill/browser-bridge/` vào `~/.claude/skills/` và `~/.codex/skills/` (bỏ qua runtime chưa cài), rồi in các lệnh cấu hình MCP: Claude Code (`claude mcp add browser-bridge -- bridge mcp`), Codex (khối `[mcp_servers.browser-bridge]` trong `config.toml`), và mẫu cho harness Ollama |

### 9.3 `GET /status`

```json
{
  "running": true, "version": "0.1.0", "protocolVersion": 1, "port": 9876, "pid": 4120, "uptimeSeconds": 120,
  "extension": { "connected": true, "id": "...", "version": "0.1.0", "browser": "chrome" },
  "sessions": 2
}
```

### 9.4 Version

- Daemon và extension dùng chung một số version, vì release cùng nhau từ một repo.
- `protocolVersion` là số nguyên. Khi handshake mà hai bên không khớp:
  - daemon đóng WebSocket với code 4400;
  - `/status` hiện cả hai version;
  - mọi lệnh trả `VERSION_MISMATCH` kèm hint nêu rõ phía nào cũ hơn.

## 10. Giao thức WebSocket giữa daemon và extension

Mọi frame là JSON text, có trường `type`.

```text
ext → daemon   {type:"hello", protocolVersion, extensionVersion, extensionId, browser}
daemon → ext   {type:"welcome", protocolVersion, daemonVersion, blockedHosts}   extension dùng blockedHosts để kiểm tra lệnh (§7.1)
daemon → ext   {type:"request", id, session, action, args, deadline}     deadline: epoch ms
ext → daemon   {type:"response", id, ok, data | error}
ext → daemon   {type:"event", name, data}      tab.closed, dialog.opened, debugger.detached
ext → daemon   {type:"ping"}   mỗi 20 giây;     daemon → ext  {type:"pong"}
```

- Daemon giới hạn mỗi frame tối đa 64 MB (screenshot full page dạng base64).
- Trong MVP, event chỉ dùng cho log và side panel, chưa đưa ra cho agent.

## 11. Mã lỗi

| Code | Khi nào |
|---|---|
| `INVALID_REQUEST` | Sai schema, sai regex session, file upload không tồn tại, đường dẫn không tuyệt đối |
| `UNKNOWN_ACTION` | Action không có trong danh sách |
| `FORBIDDEN` | Không qua được kiểm tra security (§7): sai `Origin`, `Host` hoặc `Content-Type` |
| `EXTENSION_NOT_CONNECTED` | Chưa có extension kết nối, hoặc kết nối mất giữa chừng |
| `VERSION_MISMATCH` | Lệch `protocolVersion` |
| `NO_CURRENT_TAB` | Session chưa có tab hiện tại |
| `TAB_NOT_FOUND` | `find_tab` không tìm thấy, hoặc tab đã bị đóng |
| `STALE_REF` | Ref không còn trỏ tới element nào trong DOM (trừ `wait_for` với `state: hidden`, §5.4) |
| `ELEMENT_NOT_FOUND` | CSS selector không khớp element nào (trừ `wait_for`, §5.4) |
| `AMBIGUOUS_SELECTOR` | CSS selector khớp nhiều hơn một element |
| `ELEMENT_NOT_INTERACTABLE` | Element bị ẩn, bị disable, bị che, hoặc sai loại (ví dụ `upload` vào thứ không phải input file) |
| `NAVIGATION_FAILED` | Lỗi khi navigate, hoặc không có lịch sử để lùi/tiến |
| `RESTRICTED_URL` | `chrome://`, `edge://`, Web Store, scheme không được hỗ trợ |
| `BLOCKED_HOST` | URL cần mở, hoặc URL của tab hiện tại, có host nằm trong `blockedHosts` (§7.1) |
| `DIALOG_OPEN` | Tab đang có dialog JS chưa xử lý |
| `NO_DIALOG` | `handle_dialog` khi không có dialog nào |
| `DETACHED_BY_USER` | Người dùng đã bấm Cancel trên thanh vàng debug |
| `TIMEOUT` | Quá `timeoutMs` |
| `EVAL_ERROR` | Script ném exception, hoặc kết quả quá lớn |
| `CDP_ERROR` | CDP trả lỗi, hoặc method bị chặn |
| `INTERNAL` | Lỗi không lường trước. Message chứa chi tiết để debug |

## 12. Testing

- **Go (`go test`):**
  - validate schema;
  - hàng đợi của session và timeout;
  - các kiểm tra Origin, Host, Content-Type;
  - fail toàn bộ request đang chờ khi extension mất kết nối;
  - `/tools` và danh sách tool MCP.

  Dùng một **extension giả viết bằng Go** (WebSocket client) để test WebSocket hub mà không cần mở trình duyệt.
- **E2E (Playwright, TypeScript):**
  - Chạy trên Chromium hoặc Chrome for Testing. Chrome bản thường từ 137 đã bỏ cờ `--load-extension`.
  - Load extension bằng `launchPersistentContext` với `--load-extension`, bật `bridge serve` trên một port ngẫu nhiên, gọi lệnh qua HTTP.
  - Daemon của E2E chạy ở cổng 19876, và extension được build bằng `--mode e2e` với địa chỉ đó compile sẵn (`WXT_DAEMON_URL`), nên một lần chạy test không bao giờ nối vào daemon thật ở 9876.
  - Mỗi action có ít nhất một test chạy đúng và một test lỗi.
- **`testpage/`:**
  - các loại ô nhập: input, textarea, contenteditable (một editor kiểu ProseMirror), React controlled input, `<select>`, checkbox, radio, input file;
  - link sang trang thứ hai (để test back/forward);
  - nút bật `alert`, `confirm`, `prompt`;
  - element hiện ra sau 1 giây và element biến mất sau 1 giây (để test `wait_for` với `visible` và `hidden`);
  - hai nút cùng class (để test `AMBIGUOUS_SELECTOR`);
  - link chuyển hướng sang một host nằm trong `blockedHosts` của bộ test (để test `BLOCKED_HOST` khi trang tự chuyển hướng);
  - nút bị overlay che (để test `ELEMENT_NOT_INTERACTABLE`);
  - element bị xoá khỏi DOM (để test `STALE_REF`);
  - `fetch` tới API JSON local (để test network);
  - một iframe (để kiểm tra snapshot liệt kê frame);
  - ô password (để kiểm tra snapshot không lộ giá trị).
- **Smoke test tay** theo checklist 4 use case (§1.1) trên Chrome thật đang đăng nhập, với từng consumer (§1.2).

## 13. Thứ tự triển khai

0. **Spike (khoảng 1 ngày, code bỏ đi sau khi xong):** trên tab nền đã bật focus emulation, kiểm tra ba điều:
   - `Input.dispatchMouseEvent` có tới được element không;
   - `Input.insertText` có chạy với input thường, React controlled input và editor contenteditable không;
   - `Page.captureScreenshot` có chụp được không.

   Thất bại thì đổi hướng: `click` dùng `el.click()`, `fill` dùng native setter kèm sự kiện giả, và tab mở ở chế độ `active:true`. Cập nhật lại spec trước khi làm tiếp.

   **Kết quả (2026-10-06, Chromium 153 do Playwright cài, load extension bằng `--load-extension`, Windows 11):** cả ba điều đều chạy trên tab nền, không cần đổi hướng. Mọi check (click, gõ vào input thường, React controlled input, ProseMirror, `Enter`, chụp màn hình) PASS, và tab nền vẫn ở nền, không cướp focus của tab người dùng. Chạy 5 lần, 4 lần sạch hoàn toàn. Lần còn lại, ở tab nền có focus emulation, chọn hết nội dung ProseMirror bằng Selection API rồi `Input.insertText` chèn thêm vào thay vì thay thế ("pm okpm replaced"). Vì vậy `fill` cho contenteditable phải đọc lại nội dung sau khi chèn, và thử lại một lần nếu chưa khớp (§8.4). Chưa kiểm chứng: Chrome stable hằng ngày, và cửa sổ mất focus hay bị minimize. Hai điều này cần chạy lại spike bằng tay trên Chrome thật trước khi phát hành.
1. Go struct cho protocol, `schemagen`, khung daemon (HTTP, các kiểm tra security, WebSocket hub, hàng đợi session), test bằng extension giả.
2. Khung extension: kết nối, handshake, keepalive, kết nối lại, session/tab group, `navigate`/`find_tab`/`list_tabs`/`close_*`/back/forward/reload, side panel hiển thị trạng thái.
3. Page agent: snapshot và ref, rồi `click`, `fill`, `select`, `press_key`, `scroll`, `wait_for`, kèm testpage và E2E.
4. `screenshot`, `upload`, dialog, network, `evaluate`, `cdp`.
5. CLI: `start`/`stop`/`status`/`logs`/`call`, `mcp`, `SKILL.md`, `install-skill`.
6. Hoàn thiện: `blockedHosts`, ẩn dữ liệu nhạy cảm trong log, smoke test với 3 consumer.

## 14. Rủi ro

| Rủi ro | Giảm thiểu |
|---|---|
| Input thật qua CDP không tới được tab nền | Spike 2026-10-06 đạt trên Chromium 153 (§13 bước 0). Còn phải xác nhận trên Chrome stable khi cửa sổ mất focus hoặc bị minimize |
| Thanh vàng "đang debug trình duyệt" gây phiền | Chấp nhận, vì nó cũng là tín hiệu cho biết tab đang bị điều khiển và đóng vai trò nút dừng khẩn cấp. Không dùng cờ `--silent-debugger-extension-api` |
| Model local nhỏ gọi tool sai | Mỗi tool một schema phẳng, có `hint` trong lỗi, MCP không bắt truyền session |
| Prompt injection từ nội dung trang | Có `blockedHosts`, `SKILL.md` cảnh báo agent, và nút Cancel để dừng ngay. Không chặn hoàn toàn được |
| Trang chặn CDP hoặc phát hiện automation | Ngoài phạm vi MVP. Ghi nhận lại khi gặp |
