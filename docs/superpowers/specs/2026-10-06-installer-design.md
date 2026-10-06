# Installer và Release — Design Spec

- **Ngày:** 2026-10-06
- **Trạng thái:** Draft, chờ review
- **Spec gốc:** `2026-10-06-browser-bridge-design.md` (gọi tắt là "spec gốc"). Spec này chỉ thêm phần phát hành và cài đặt; API, CLI và thư mục home vẫn định nghĩa ở spec gốc.

## 1. Mục tiêu

Cài browser-bridge lên máy Windows của người khác bằng một lệnh, giống cách Kimi WebBridge làm:

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex
```

Lệnh này cài daemon và extension; người dùng chỉ phải Load unpacked extension **một lần**.

### 1.1 Cách Kimi làm (để đối chiếu)

`install.ps1` của Kimi (đã đọc bản thật) chỉ tải `kimi-webbridge.exe` về `%USERPROFILE%\.kimi-webbridge\bin\`, chạy `start`, rồi `install-skill -y`. Extension của Kimi người dùng tự cài từ Chrome Web Store. Không script nào tự cài được extension trên máy cá nhân: Chrome chỉ cho cài không cần người bấm qua enterprise policy, trên máy có quản lý tập trung.

### 1.2 Quyết định đã chốt

- **Extension load unpacked**, không lên Store (giữ nguyên spec gốc §1.3). Script tải extension về một thư mục cố định và hướng dẫn Load unpacked.
- **Làm ngay** trên những gì đã có (8 action về tab, gọi qua HTTP). `install-skill` và cấu hình MCP được thêm vào script khi plan CLI hoàn thành.
- **Không tự khởi động khi đăng nhập**, giống Kimi. Script bật daemon lúc cài; sau khi khởi động lại máy, người dùng (sau này là agent, theo `SKILL.md` hoặc `bridge mcp`) chạy `bridge start`.
- **Build bằng GitHub Actions, phát qua GitHub Releases.** Repo public nên tải không cần đăng nhập.
- Chỉ Windows amd64 (máy Windows ARM chạy bản amd64 qua giả lập). Script chạy được trên Windows PowerShell 5.1 và PowerShell 7.

### 1.3 Tiêu chí hoàn thành

Trên một máy Windows có Chrome, không có Go hay Node:
1. Chạy lệnh cài một dòng.
2. Load unpacked thư mục mà script chỉ.
3. Script báo extension đã kết nối, và `curl` tới `POST /command` với `navigate` mở được trang.

Chạy lại lệnh cài khi daemon đang chạy thì cập nhật được mà không lỗi. Chạy `-Uninstall` thì gỡ sạch.

### 1.4 Không làm

- Tự cài extension, publish lên Store.
- Ký số file `.exe`.
- Tự khởi động daemon khi đăng nhập.
- `install-skill`, `bridge mcp`, `SKILL.md` (thuộc plan CLI).
- macOS/Linux (`install.sh`).

## 2. File phát hành

Mỗi GitHub Release `vX.Y.Z` có đúng 3 file, tên cố định để URL `releases/latest/download/<tên>` luôn đúng:

| File | Nội dung |
|---|---|
| `install.ps1` | Script cài (§3) |
| `bridge-windows-amd64.exe` | Daemon và CLI, `go build -trimpath -ldflags "-s -w"` |
| `browser-bridge-extension.zip` | Bản production của extension (`wxt zip`), gốc zip là thư mục có `manifest.json` |

## 3. `install.ps1`

### 3.1 Cách gọi

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex   # bản mới nhất
$env:BRIDGE_VERSION = '0.1.0'; irm <url>/install.ps1 | iex                                        # ghim version
iex "& { $(irm <url>/install.ps1) } -Uninstall"                                                   # gỡ
```

`<url>` là `https://github.com/phantranthelinh/browser-bridge/releases/latest/download`.

| Cờ | Ý nghĩa |
|---|---|
| `-Help` | In hướng dẫn |
| `-NoStart` | Cài nhưng không bật daemon (và vì vậy không chờ extension) |
| `-NoPath` | Không sửa `PATH` |
| `-NoWait` | Không chờ extension kết nối (cho script và CI) |
| `-Uninstall` | Gỡ (§3.4) |

| Biến môi trường | Ý nghĩa |
|---|---|
| `BRIDGE_VERSION` | Ghim version, ví dụ `0.1.0`. Mặc định: bản mới nhất |
| `BRIDGE_HOME` | Thư mục cài, mặc định `%USERPROFILE%\.browser-bridge`. Cùng biến mà daemon dùng (spec gốc §9.1) |
| `BRIDGE_INSTALL_BASE` | Nguồn tải thay cho GitHub Releases: một URL, hoặc một thư mục local (khi đó file được chép thay vì tải). Dùng để test |

Không cần quyền admin. Script bật TLS 1.2 cho Windows PowerShell 5.1.

### 3.2 Các bước cài

Nguồn tải: `BRIDGE_INSTALL_BASE` nếu có; nếu không thì `https://github.com/phantranthelinh/browser-bridge/releases/download/v<BRIDGE_VERSION>` khi ghim version, còn lại `…/releases/latest/download`.

1. Nếu `bin\bridge.exe` đã có: chạy `bridge stop`. Windows khoá file `.exe` đang chạy, nên không dừng trước thì lần cài lại sẽ hỏng ở bước 2.
2. Tải `bridge-windows-amd64.exe` vào file tạm (thử lại 3 lần, cách nhau 5 giây), rồi chuyển thành `bin\bridge.exe`.
3. Tải `browser-bridge-extension.zip`, giải nén vào thư mục tạm, kiểm tra có `manifest.json`, xoá `extension\` cũ rồi chuyển thư mục mới vào. Đường dẫn `extension\` luôn giữ nguyên, nên extension đã Load unpacked một lần sẽ dùng tiếp qua các lần cập nhật.
4. Trừ khi có `-NoPath`: thêm `bin\` vào `PATH` của user (HKCU), và vào `$env:Path` của cửa sổ đang chạy.
5. Trừ khi có `-NoStart`: chạy `bridge start`.
6. Trừ khi có `-NoStart` hoặc `-NoWait`: đọc `bridge status`.
   - Extension đã kết nối (cập nhật): báo xong, nhắc bấm Reload trong `chrome://extensions` vì code extension vừa đổi.
   - Chưa kết nối (cài lần đầu): in 3 bước (mở `chrome://extensions`, bật Developer mode, Load unpacked → `<BRIDGE_HOME>\extension`), chép đường dẫn đó vào clipboard, rồi chờ tối đa 3 phút, kiểm tra mỗi 2 giây, và báo khi extension kết nối. Ctrl+C bỏ qua việc chờ; mọi thứ đã cài xong.
   - `/status` cho thấy lệch protocol version: báo rằng extension cần Reload.
7. In tóm tắt: version đã cài, địa chỉ daemon, lệnh `bridge status`, và cảnh báo bảo mật một dòng (§6).

### 3.3 Lỗi

- Mỗi bước thất bại in một dòng đỏ nêu bước và cách xử lý; khi tải hỏng thì kèm URL. Bước 1–4 hỏng thì script dừng với exit code 1.
- `bridge start` hỏng chỉ là cảnh báo, kèm đường dẫn `logs\daemon.log`, vì binary và extension đã cài xong.
- Hết 3 phút chờ mà extension chưa kết nối thì nhắc lại 3 bước. Không coi là lỗi.

### 3.4 Gỡ (`-Uninstall`)

1. `bridge stop` (nếu có `bin\bridge.exe`).
2. Gỡ `bin\` khỏi `PATH` của user.
3. Xoá đúng những gì bridge tạo trong `BRIDGE_HOME`: `bin\`, `extension\`, `logs\`, `artifacts\`, `config.json`, `daemon.pid`, `daemon.addr`. Sau đó xoá `BRIDGE_HOME` chỉ khi nó đã rỗng. Không xoá đệ quy cả `BRIDGE_HOME`, vì một `BRIDGE_HOME` trỏ nhầm (ví dụ thư mục user) sẽ mất sạch dữ liệu.
4. Nhắc người dùng gỡ extension trong `chrome://extensions`; Chrome sẽ báo lỗi cho extension có thư mục đã bị xoá.

## 4. Lệnh CLI cần cho installer

Script dùng `start`, `stop`, `status`. Ba lệnh này cùng `restart` được làm theo spec gốc §9.2 (đã cập nhật). Tóm tắt:

- **Địa chỉ:** đọc `daemon.addr` nếu có; nếu không thì `--addr` > `config.json` > `127.0.0.1:9876`.
- **`start`:** `/status` đã trả lời thì in "already running" và exit 0. Nếu chưa, chạy chính exe này với `serve` thành tiến trình tách rời (`DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`, không cửa sổ, stdout/stderr bỏ đi) và chờ tối đa 5 giây cho `/status`. Hỏng thì in các dòng cuối của `logs\daemon.log` và exit 1.
- **`stop`:** `POST /shutdown`, chờ tối đa 5 giây. Daemon treo thì kill theo `pid` của `/status`, không bao giờ theo file `daemon.pid`. Không ai trả lời thì xoá `daemon.pid`/`daemon.addr` còn sót. Exit 0.
- **`restart`:** `stop` rồi `start`.
- **`status`:** in JSON của `/status` (đã có `pid`), hoặc `{"running": false, "addr": …}`. Exit 0 khi chạy, 1 khi không.

Phía daemon thêm `POST /shutdown` (spec gốc §4) và trường `pid` trong `/status` (spec gốc §9.3).

## 5. Release

### 5.1 Version

Version nằm ở hai chỗ: `protocol.Version` (`daemon/internal/protocol/protocol.go`) và `version` trong `extension/package.json`. Ra bản mới:
1. Sửa cả hai, commit.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`.

Workflow từ chối build khi tag, Go và `package.json` không cùng một version.

### 5.2 Workflow `.github/workflows/release.yml`

Chạy khi push tag `v*`, trên `windows-latest`, quyền `contents: write`.

1. **Kiểm tra version** (§5.1).
2. **Test:**
   - daemon: `go vet ./...`, `go test ./...`;
   - extension: `npm ci`, `check:gen`, `typecheck`, `test`;
   - E2E: `npm ci`, `npx playwright install chromium`, `npm test`.
3. **Build vào `dist/`** đúng 3 file ở §2.
4. **Test script cài** trên `dist/` (§5.3), chạy bằng cả `powershell.exe` (5.1) lẫn `pwsh` (7).
5. **Publish:** `gh release create vX.Y.Z dist/* --title vX.Y.Z --generate-notes`.
6. **Smoke test sau publish:** chạy đúng lệnh cài từ GitHub (`irm …/releases/download/vX.Y.Z/install.ps1 | iex` với `BRIDGE_VERSION`), vào `BRIDGE_HOME` tạm, kèm `-NoPath -NoWait`. Kiểm tra `bridge version` khớp tag, rồi `-Uninstall`.

### 5.3 Test của script cài

`install/test-install.ps1 -Artifacts <thư mục dist>` chạy được ở máy dev lẫn CI. Nó dùng `BRIDGE_INSTALL_BASE=<dist>`, `BRIDGE_HOME` tạm, `-NoPath -NoWait`, và một cổng không phải 9876 (ghi vào `config.json` của `BRIDGE_HOME` tạm), nên không đụng gì tới bản cài thật trên máy. Các bước kiểm tra:

1. **Cài mới:** có `bin\bridge.exe`, `bridge version` đúng, `extension\manifest.json` có trường `key`, `bridge status` exit 0.
2. **Cài lại khi daemon đang chạy:** không lỗi khoá file, daemon vẫn chạy và đúng version.
3. **`-Uninstall`:** `/status` không còn trả lời, `BRIDGE_HOME` không còn.
4. **`-Uninstall` không xoá file lạ:** một file không do bridge tạo, đặt sẵn trong `BRIDGE_HOME`, vẫn còn sau khi gỡ, và `BRIDGE_HOME` cũng còn theo.

## 6. Security

| Rủi ro | Xử lý |
|---|---|
| `irm \| iex` chạy code từ GitHub với quyền của người dùng | Chỉ tải từ GitHub Releases của repo qua HTTPS. README nói rõ lệnh làm gì. Rủi ro đã chấp nhận, giống Kimi |
| `.exe` không ký số, Defender hoặc SmartScreen có thể cảnh báo nhầm | Rủi ro đã chấp nhận. README ghi cách nhận biết |
| Máy được cài: mọi tiến trình của user điều khiển được trình duyệt qua bridge (spec gốc §7) | Script in một dòng cảnh báo khi cài xong; README nhắc lại |
| `/shutdown` cho phép tiến trình local tắt daemon | Cùng mức với phần còn lại của API (spec gốc §7): ai gọi được `/command` cũng đã điều khiển được trình duyệt. Vẫn qua kiểm tra Origin, Host, Content-Type nên trang web không gọi được |

## 7. README

Viết lại `README.md` cho người nhận:
1. Lệnh cài một dòng, kèm 3 bước Load unpacked.
2. Kiểm tra (`bridge status`, side panel) và gọi thử một lệnh.
3. Cập nhật (chạy lại lệnh cài, bấm Reload extension) và gỡ (`-Uninstall`).
4. Cảnh báo bảo mật (§6).
5. Mục "Ra bản mới" cho người duy trì (§5.1).

## 8. Thứ tự triển khai

1. Daemon: `pid` trong `/status`, `POST /shutdown`.
2. CLI: `start`, `stop`, `restart`, `status`.
3. `install/install.ps1` và `install/test-install.ps1`, test ở máy dev với `dist/` build tay.
4. `.github/workflows/release.yml`.
5. README.
6. Ra bản `v0.1.0` thật và thử lệnh cài trên một máy (hoặc user Windows) chưa từng cài.
