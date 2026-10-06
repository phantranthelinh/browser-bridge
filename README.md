# browser-bridge

Cho phép bất kỳ agent nào (Claude Code, Codex, harness chạy model Ollama local) điều khiển **Chrome/Edge thật** của bạn, kể cả các trang đã đăng nhập sẵn. Không cần tài khoản bên thứ ba.

```text
Agent ──HTTP 127.0.0.1:9876──▶ bridge daemon (Go) ──WebSocket──▶ Extension MV3 (TypeScript, WXT) ──CDP──▶ tab
```

- **Daemon** (`daemon/`, binary `bridge.exe`): validate request theo JSON Schema, route theo session, timeout, mã lỗi chuẩn. Không đụng vào CDP hay DOM.
- **Extension** (`extension/`): kết nối tới daemon, quản lý session và tab group, thao tác tab qua `chrome.debugger`. Có side panel xem trạng thái và log.
- **Agent** chỉ cần biết HTTP API, không cần biết gì về Chrome API hay CDP.

Chỉ hỗ trợ Windows.

## Cài đặt

**1. Chạy lệnh này trong PowerShell** (không cần quyền admin):

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex
```

Lệnh này tải `bridge.exe` và extension về `%USERPROFILE%\.browser-bridge\`, thêm `bridge` vào `PATH`, rồi bật daemon.

**2. Load extension** (chỉ một lần):

1. Mở `chrome://extensions` (Edge: `edge://extensions`).
2. Bật **Developer mode**.
3. Bấm **Load unpacked**, chọn thư mục `%USERPROFILE%\.browser-bridge\extension`. Script cài đã chép sẵn đường dẫn này vào clipboard.

Script chờ tối đa 3 phút và báo khi extension đã kết nối. Thư mục extension giữ nguyên qua các lần cập nhật, nên không phải Load unpacked lại.

Lần đầu chạy, Windows Defender có thể quét `bridge.exe` mất vài chục giây. `bridge.exe` chưa được ký số, nên Defender hoặc SmartScreen đôi khi cảnh báo nhầm.

## Dùng thử

```powershell
bridge status
curl.exe -s -X POST http://127.0.0.1:9876/command -H "Content-Type: application/json" -d '{"action":"navigate","args":{"url":"https://example.com"},"session":"demo"}'
```

Tab `example.com` mở ở nền trong group "demo", kèm thanh vàng "đang debug" của Chrome. Bấm **Cancel** trên thanh vàng là dừng ngay mọi lệnh trên tab đó. Bấm icon Browser Bridge để mở side panel xem kết nối, session và các lệnh gần nhất.

Endpoint: `POST /command`, `GET /tools` (danh sách action kèm schema), `GET /status`, `POST /shutdown`, `GET /ws` (chỉ cho extension).

**Đã chạy được:** 8 action về tab (`navigate`, `find_tab`, `list_tabs`, `close_tab`, `close_session`, `go_back`, `go_forward`, `reload`) và các lệnh `bridge start|stop|restart|status`.
**Chưa làm:** `snapshot`, `click`/`fill`/…, `screenshot`, network, `evaluate`, `bridge call|mcp`, `SKILL.md`. Lộ trình đầy đủ nằm trong spec.

## Hằng ngày

| Việc | Lệnh |
|---|---|
| Bật daemon sau khi khởi động lại máy | `bridge start` |
| Xem trạng thái daemon và extension | `bridge status` |
| Tắt / bật lại | `bridge stop`, `bridge restart` |
| Cập nhật | Chạy lại lệnh cài, rồi bấm **Reload** ở Browser Bridge trong `chrome://extensions` |
| Gỡ | `iex "& { $(irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1) } -Uninstall"`, rồi **Remove** extension |

Ghim một version: đặt `$env:BRIDGE_VERSION = '0.1.0'` trước lệnh cài. Xem mọi tuỳ chọn: chạy với `-Help`, theo cùng cách như `-Uninstall`.

Dữ liệu (config, pid, log, artifact) nằm trong `%USERPROFILE%\.browser-bridge` (đổi bằng biến `BRIDGE_HOME`).

## Bảo mật

- **Mọi chương trình chạy dưới user của bạn đều điều khiển được trình duyệt qua bridge.** Daemon không có token; đây là rủi ro đã chấp nhận (spec §7).
- Daemon chỉ bind `127.0.0.1`. `/ws` chỉ nhận `Origin` của extension có trong `config.extensionIds`; HTTP từ chối mọi request có header `Origin`, nên trang web không `fetch` được tới daemon.
- Nội dung trang web có thể dụ agent làm việc khác (prompt injection). Chặn các site nhạy cảm bằng `blockedHosts` trong `%USERPROFILE%\.browser-bridge\config.json`, ví dụ `{"blockedHosts": ["bank.com"]}`, rồi `bridge restart`.
- `irm … | iex` chạy script tải từ GitHub với quyền của bạn. Muốn thì đọc [`install/install.ps1`](install/install.ps1) trước.

## Phát triển

Yêu cầu: Go (xem `daemon/go.mod`), Node.js 22, Chrome hoặc Edge.

| Việc | Lệnh |
|---|---|
| Test daemon | `go -C daemon test ./...` |
| Chạy daemon từ source | `go -C daemon run ./cmd/bridge serve` |
| Build extension (ra `extension/.output/chrome-mv3`) | `npm --prefix extension install` rồi `npm --prefix extension run build` |
| Sinh lại schema và type TS | `go -C daemon run ./cmd/schemagen` rồi `npm --prefix extension run gen` |
| Kiểm tra file sinh ra còn khớp | `npm --prefix extension run check:gen` |
| Test và typecheck extension | `npm --prefix extension test`, `npm --prefix extension run typecheck` |
| Dev extension (hot reload) | `npm --prefix extension run dev` |
| E2E (Playwright, daemon riêng ở cổng 19876) | `npm --prefix e2e install` rồi `npm --prefix e2e test` |
| Build 3 file release | `./install/build-dist.ps1 -Out dist` |
| Test script cài (không đụng bản cài thật) | `./install/test-install.ps1 -Artifacts dist -Shell powershell` (và `-Shell pwsh`) |

Định nghĩa action viết **một lần** trong `daemon/internal/protocol` (Go struct). Từ đó sinh ra `schema/protocol.schema.json` và `extension/src/generated/protocol.ts`; đừng sửa tay file sinh ra.

### Ra bản mới

1. Đặt cùng một version ở `daemon/internal/protocol/protocol.go` (`Version`) và `extension/package.json` (`version`), commit.
2. `git tag v0.2.0 && git push origin v0.2.0`.

Workflow [`release.yml`](.github/workflows/release.yml) kiểm tra version, chạy toàn bộ test, build, thử script cài trên Windows PowerShell 5.1 và PowerShell 7, publish GitHub Release, rồi cài thử từ chính release vừa đăng.

## Cấu trúc repo

```text
daemon/       Go: binary bridge (cmd/bridge), sinh schema (cmd/schemagen), internal/{protocol,server,session,home,fakeext}
schema/       JSON Schema sinh từ Go, có commit
extension/    WXT + React: src/entrypoints, src/background, src/shared, src/generated
e2e/          Playwright, chạy extension thật với daemon riêng
testpage/     Trang HTML tĩnh để test
install/      install.ps1, build-dist.ps1, check-version.ps1, test-install.ps1
docs/         Spec và plan
```

## Tài liệu

- [Design spec](docs/superpowers/specs/2026-10-06-browser-bridge-design.md): kiến trúc, API, action, session, bảo mật.
- [Installer và release](docs/superpowers/specs/2026-10-06-installer-design.md): cài một lệnh, release từ tag.
- [Plans](docs/superpowers/plans/): kế hoạch triển khai từng phần.
