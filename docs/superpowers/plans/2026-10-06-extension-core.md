# Extension Core Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Dựng extension MV3 (WXT + TypeScript): kết nối WebSocket tới daemon với handshake, ping và kết nối lại; quản lý session và tab group; 8 action về tab (`navigate`, `find_tab`, `list_tabs`, `close_tab`, `close_session`, `go_back`, `go_forward`, `reload`); kiểm tra `blockedHosts` phía extension; side panel hiển thị trạng thái. Tất cả được test E2E bằng Playwright trên daemon thật.

**Architecture:** Mỗi entrypoint WXT chỉ là lớp nối dây mỏng (`src/entrypoints/background.ts`, `src/entrypoints/sidepanel/`). Logic nằm ở `src/background/`:
- `connection.ts`: socket, handshake, ping, backoff.
- `router.ts`: các luật chung cho mọi action (session bị dừng, host bị chặn, lệnh dọn dẹp luôn chạy) rồi chuyển tới handler.
- `sessions.ts`: state trong `chrome.storage.session`, ghi tuần tự.
- `cdp.ts`: attach, gửi lệnh, chờ event.
- `actions/tabs.ts`: các handler.

Type của wire format được sinh từ `schema/protocol.schema.json`, nên extension và daemon dùng chung một định nghĩa. Các module thuần được unit test bằng vitest. Phần đụng tới Chrome API được test E2E: Playwright mở Chromium có load extension, nối với `bridge serve` thật và một server tĩnh phục vụ `testpage/`.

**Tech Stack:** WXT 0.21, TypeScript 7, React 19 (side panel), vitest 5, json-schema-to-typescript 16, Playwright 1.63 (`@playwright/test`, channel `chromium`), Node 22.

**Spec:** `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§2, §3, §3.1, §5.1, §6.1, §7.1, §8.1, §8.2, §8.5 `navigate`, §8.7, §10, §12)

**Không thuộc plan này:** page agent, snapshot và ref (§6.2, §8.3), các action thao tác trang, chờ, chụp màn hình, network, dialog, `evaluate`, `cdp` (plan 03, 04). Extension này trả `INTERNAL` "not implemented" cho 16 action đó. CLI và MCP thuộc plan 05.

## Global Constraints

- Node 22, npm. Mỗi package (`extension/`, `e2e/`) có `node_modules` và `package-lock.json` riêng.
- Extension ID cố định `nfjidhefdgblbbfhnmbcogkbphipngif`, lấy từ `extension/manifest-key.txt` (đã có từ plan 01).
- Permission đúng như §8.1: `debugger`, `tabs`, `tabGroups`, `storage`, `sidePanel`, `alarms`. Không có `host_permissions`.
- Địa chỉ daemon mặc định `ws://127.0.0.1:9876/ws`, đổi được trong side panel (`chrome.storage.local.daemonUrl`).
- Ping mỗi 20 giây; backoff 1s → 30s; `chrome.alarms` mỗi 30 giây.
- Tab mới mở ở nền (`active:false`), vào group đặt tên theo `groupTitle` hoặc tên session.
- `close_tab` trên tab mượn chỉ trả lại, không đóng. `list_tabs`, `close_tab`, `close_session` không bao giờ bị chặn (§6.1).
- Không nhắc ticket trong comment; comment chỉ để giải thích điều code không tự nói.
- E2E dùng daemon ở cổng 19876 và bản build `--mode e2e` có địa chỉ đó compile sẵn, để không bao giờ chạm vào daemon thật của người dùng ở 9876.

## Review Focus

- **Service worker bị Chrome tắt khi rảnh:** Chrome dừng SW rảnh sau 30 giây, còn daemon đóng socket im lặng sau 60 giây. Ping 20 giây phải giữ được cả hai. Test: `tests/keepalive.spec.ts` (Task 6), để yên 70 giây rồi kiểm tra daemon không ghi `extension disconnected`.
- **Trang tự chuyển hướng sang host bị chặn:** daemon không bắt được (URL ban đầu hợp lệ). Extension phải kiểm tra URL sau khi load, không lộ URL hay title, chặn tiếp các lệnh trên tab đó nhưng vẫn cho rời đi. Test: `blocked hosts: refused up front, caught after a redirect…` (Task 6), `refuses page actions while the current tab is on a blocked host…` (Task 5).
- **`go_back` ngay trên trang đầu tiên của tab mới:** tab được tạo từ `about:blank`; nếu không xoá lịch sử, agent quay về một trang trắng thay vì nhận `NAVIGATION_FAILED`. Test: `back and forward walk the history and stop at its ends` (Task 6).
- **Hai cập nhật session cùng lúc:** lệnh của các session khác nhau và event đóng tab chạy song song, cùng ghi một key storage. Test: `loses no update when many run at once` (Task 3).
- **Người dùng bấm Cancel trên thanh debug:** mọi lệnh trừ dọn dẹp, `navigate`, `find_tab` phải trả `DETACHED_BY_USER`. Playwright không bấm được nút đó, nên luật này được pin bằng unit test `refuses everything but cleanup…` (Task 5); phần nối `onDetach` → `stopSessionsOf` chỉ là hai dòng trong `background.ts`.

---

### Task 1: Ghim `protocolVersion` trong schema

**Files:**
- Modify: `daemon/internal/protocol/frames.go`
- Test: `daemon/internal/protocol/actions_test.go`
- Modify: `schema/protocol.schema.json` (sinh lại)

**Interfaces:**
- Produces: trong schema, `$defs.Hello.properties.protocolVersion` và `$defs.Welcome.properties.protocolVersion` có `"const": 1`. Type TS sinh ra ở Task 2 là literal `1`, nên khi Go tăng version thì extension compile lỗi cho tới khi được cập nhật theo.

- [ ] **Step 1: Viết test fail**

Thêm vào cuối `daemon/internal/protocol/actions_test.go`:

```go
func TestFramesPinProtocolVersion(t *testing.T) {
	b, _ := Document()
	var doc struct {
		Defs map[string]struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	json.Unmarshal(b, &doc)
	for _, name := range []string{"Hello", "Welcome"} {
		if got := doc.Defs[name].Properties["protocolVersion"]["const"]; got != float64(ProtocolVersion) {
			t.Errorf("%s.protocolVersion const = %v, want %d", name, got, ProtocolVersion)
		}
	}
}
```

- [ ] **Step 2: Chạy test, xác nhận fail**

Run: `go -C daemon test ./internal/protocol/ -run TestFramesPinProtocolVersion`
Expected: FAIL `Hello.protocolVersion const = <nil>, want 1`

- [ ] **Step 3: Thêm `JSONSchemaExtend` cho `Hello` và `Welcome`**

Trong `daemon/internal/protocol/frames.go`, thay `import "encoding/json"` bằng:

```go
import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)
```

và chèn ngay trước `type Welcome struct {`:

```go
// JSONSchemaExtend pins protocolVersion to the Go constant. The extension's generated type then
// becomes the literal version, so bumping it here breaks the extension's build until it follows.
func (Hello) JSONSchemaExtend(s *jsonschema.Schema) { pinProtocolVersion(s) }

func (Welcome) JSONSchemaExtend(s *jsonschema.Schema) { pinProtocolVersion(s) }

func pinProtocolVersion(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("protocolVersion"); ok {
		p.Const = ProtocolVersion
	}
}

```

- [ ] **Step 4: Sinh lại schema và chạy toàn bộ test Go**

```bash
go -C daemon run ./cmd/schemagen
go -C daemon test ./...
```

Expected: mọi package `ok`.

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/protocol schema/protocol.schema.json
git commit -m "feat(daemon): pin protocolVersion in the schema so the extension type follows it"
```

---

### Task 2: Khung extension WXT và type sinh từ schema

**Files:**
- Modify: `.gitignore`
- Create: `extension/package.json`, `extension/package-lock.json` (qua `npm install`)
- Create: `extension/wxt.config.ts`
- Create: `extension/tsconfig.json`
- Create: `extension/vitest.config.ts`
- Create: `extension/scripts/gen-types.mjs`
- Create: `extension/src/generated/protocol.ts` (sinh ra)
- Create: `extension/src/shared/state.ts`
- Create: `extension/src/entrypoints/background.ts` (bản rỗng; Task 6 thay)

**Interfaces:**
- Consumes: `schema/protocol.schema.json` (Task 1), `extension/manifest-key.txt`.
- Produces:
  - `src/generated/protocol.ts`: `ActionName`, `ErrorBody` (đổi tên từ `Error` để không che class `Error`), `Hello`, `Welcome`, `RequestFrame`, `ResponseFrame`, và các `*Args`/`*Result` như `NavigateArgs`, `FindTabArgs`, `TabResult`, `FindTabResult`, `ListTabsResult`, `CloseTabResult`, `CloseSessionResult`, `PageResult`
  - `src/shared/state.ts`: `DEFAULT_DAEMON_URL`, `KEYS = {sessions, connection, log, daemonUrl}`, `SessionState`, `Sessions`, `ConnectionStatus`, `LogEntry`, `StorageLike`
  - Script npm: `build`, `gen`, `check:gen`, `typecheck`, `test`. Build thường ra `.output/chrome-mv3`; `--mode e2e` ra `.output/chrome-mv3-e2e`, với `WXT_DAEMON_URL` lấy lúc build.

- [ ] **Step 1: Bổ sung `.gitignore`**

`.gitignore` (toàn bộ file):

```text
/daemon/bridge.exe
node_modules/
/extension/.output/
/extension/.wxt/
/e2e/.cache/
/e2e/test-results/
/e2e/playwright-report/
```

- [ ] **Step 2: Tạo `extension/package.json` rồi cài**

```json
{
  "name": "browser-bridge-extension",
  "version": "0.1.0",
  "scripts": {
    "postinstall": "wxt prepare",
    "dev": "wxt",
    "build": "wxt build",
    "gen": "node scripts/gen-types.mjs",
    "check:gen": "node scripts/gen-types.mjs --check",
    "typecheck": "tsc --noEmit",
    "test": "vitest run"
  },
  "type": "module",
  "devDependencies": {
    "@types/node": "^22.20.5",
    "@types/react": "^19.3.0",
    "@types/react-dom": "^19.3.0",
    "@wxt-dev/module-react": "^1.2.2",
    "json-schema-to-typescript": "^16.0.0",
    "react": "^19.3.0",
    "react-dom": "^19.3.0",
    "typescript": "^7.0.2",
    "vitest": "^5.0.3",
    "wxt": "^0.21.4"
  },
  "private": true
}
```

```bash
cd extension
npm install
```

Expected: `postinstall` chạy `wxt prepare`, sinh `.wxt/` (đã ignore).

- [ ] **Step 3: Tạo các file cấu hình**

`extension/wxt.config.ts`:

```ts
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
```

`extension/tsconfig.json`:

```json
{
  "extends": "./.wxt/tsconfig.json",
  "compilerOptions": {
    "jsx": "react-jsx"
  }
}
```

`extension/vitest.config.ts`:

```ts
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    include: ['src/**/*.test.ts'],
  },
});
```

- [ ] **Step 4: Viết script sinh type rồi sinh `src/generated/protocol.ts`**

`extension/scripts/gen-types.mjs`:

```js
// Generates src/generated/protocol.ts from schema/protocol.schema.json, which the daemon generates
// from its Go structs. With --check it only reports whether the committed file is stale.
import { compile } from 'json-schema-to-typescript';
import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const schemaPath = join(root, '..', 'schema', 'protocol.schema.json');
const outPath = join(root, 'src', 'generated', 'protocol.ts');

const schema = JSON.parse(readFileSync(schemaPath, 'utf8'));

// "Error" would shadow the global Error class wherever it is imported.
schema.$defs.Error.title = 'ErrorBody';

// The Go side titles each "exactly one of" branch (selector, text, ...). Those titles would become
// TypeScript types that collide with DOM globals such as Text, so the branches stay anonymous.
(function stripBranchTitles(node) {
  if (Array.isArray(node)) return node.forEach(stripBranchTitles);
  if (node === null || typeof node !== 'object') return;
  for (const key of ['oneOf', 'anyOf']) {
    for (const branch of node[key] ?? []) delete branch.title;
  }
  Object.values(node).forEach(stripBranchTitles);
})(schema);

const ts = await compile(schema, 'BrowserBridgeProtocol', {
  bannerComment: '/* Generated from schema/protocol.schema.json by scripts/gen-types.mjs. Do not edit. */',
  unreachableDefinitions: true,
  additionalProperties: false,
  style: { singleQuote: true },
});

if (process.argv.includes('--check')) {
  const current = readFileSync(outPath, 'utf8').replace(/\r\n/g, '\n');
  if (current !== ts) {
    console.error('src/generated/protocol.ts is stale: run npm run gen');
    process.exit(1);
  }
  console.log('src/generated/protocol.ts is up to date');
} else {
  writeFileSync(outPath, ts);
  console.log('wrote', outPath);
}
```

```bash
cd extension
mkdir -p src/generated
npm run gen
npm run check:gen
```

Expected: `wrote …protocol.ts`, rồi `src/generated/protocol.ts is up to date`. Trong file sinh ra, `Hello` có `protocolVersion: 1;`.

- [ ] **Step 5: Viết `src/shared/state.ts`**

`extension/src/shared/state.ts`:

```ts
// State shared by the service worker, which writes it, and the side panel, which displays it.

// import.meta.env.WXT_DAEMON_URL lets the E2E build point at its own daemon, so tests never touch
// a daemon the user is running on the default port.
export const DEFAULT_DAEMON_URL: string = import.meta.env.WXT_DAEMON_URL || 'ws://127.0.0.1:9876/ws';

// chrome.storage.session holds sessions, connection and log, so they survive a service worker
// restart but not a browser restart. chrome.storage.local holds daemonUrl.
export const KEYS = {
  sessions: 'sessions',
  connection: 'connection',
  log: 'log',
  daemonUrl: 'daemonUrl',
} as const;

export interface SessionState {
  groupId: number | null;
  tabIds: number[];
  borrowedTabIds: number[];
  currentTabId: number | null;
  // Set when the user pressed Cancel on the debugging bar; cleared by navigate or find_tab.
  stopped: boolean;
}

export type Sessions = Record<string, SessionState>;

export interface ConnectionStatus {
  state: 'connecting' | 'connected' | 'disconnected';
  url: string;
  daemonVersion?: string;
  blockedHosts?: string[];
  error?: string;
}

export interface LogEntry {
  at: number;
  session: string;
  action: string;
  selector?: string;
  ms: number;
  code?: string;
}

/** The part of chrome.storage.StorageArea the store classes use; tests pass an in-memory one. */
export interface StorageLike {
  get(key: string): Promise<Record<string, unknown>>;
  set(items: Record<string, unknown>): Promise<void>;
}
```

- [ ] **Step 6: Entrypoint background rỗng để build được**

`extension/src/entrypoints/background.ts`:

```ts
import { defineBackground } from 'wxt/utils/define-background';

export default defineBackground(() => {});
```

- [ ] **Step 7: Build và typecheck**

```bash
cd extension
npm run typecheck
npm run build
```

Expected: `tsc` không in lỗi. Build ra `.output/chrome-mv3/manifest.json` có `"version":"0.1.0"`, `"key":"MIIBIjAN…"` và đủ 6 permission.

- [ ] **Step 8: Commit**

```bash
git add .gitignore extension
git commit -m "feat(extension): scaffold WXT extension with types generated from the schema"
```

---

### Task 3: Host, lỗi, session store và command log

**Files:**
- Create: `extension/src/background/errors.ts`
- Create: `extension/src/background/hosts.ts`
- Create: `extension/src/background/sessions.ts`
- Create: `extension/src/background/log.ts`
- Create: `extension/src/background/test-support.ts`
- Test: `extension/src/background/hosts.test.ts`, `sessions.test.ts`, `log.test.ts`

**Interfaces:**
- Consumes: `ErrorBody` (Task 2), `KEYS`, `SessionState`, `Sessions`, `LogEntry`, `StorageLike` (Task 2).
- Produces:
  - `errors.ts`: `class BridgeError(code: ErrorCode, message: string, hint?: string)`, `toErrorBody(e: unknown): ErrorBody`, `blockedHost(): BridgeError`, `noCurrentTab(session: string): BridgeError`
  - `hosts.ts`: `normalizeHost`, `httpHost(url?) → string | null`, `queryHost(query) → string | null`, `hostMatches(host, base)`, `isBlocked(url?, blockedHosts)`, `isRestricted(url?)`. Luật giống hệt `checks.go` của daemon.
  - `sessions.ts`: `class SessionStore(area: StorageLike)` với `all()`, `get(name)`, `update(name, change)`, `remove(name)`, `forgetTab(tabId) → string[]`, `stopSessionsOf(tabId) → string[]`
  - `log.ts`: `class CommandLog(area: StorageLike, max = 50)` với `add(entry)`
  - `test-support.ts`: `memoryStorage()` cho unit test

- [ ] **Step 1: Viết test fail**

`extension/src/background/test-support.ts`:

```ts
import type { StorageLike } from '../shared/state';

/** An in-memory chrome.storage area. Values are cloned like the real one, so tests catch aliasing bugs. */
export function memoryStorage(): StorageLike & { data: Record<string, unknown> } {
  const data: Record<string, unknown> = {};
  return {
    data,
    async get(key) {
      return key in data ? { [key]: structuredClone(data[key]) } : {};
    },
    async set(items) {
      for (const [k, v] of Object.entries(items)) data[k] = structuredClone(v);
    },
  };
}
```

`extension/src/background/hosts.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { hostMatches, httpHost, isBlocked, isRestricted, normalizeHost, queryHost } from './hosts';

describe('hosts', () => {
  it('normalizes case and the trailing dot', () => {
    expect(normalizeHost('BANK.com.')).toBe('bank.com');
  });

  it('reads hosts only from http(s) URLs', () => {
    expect(httpHost('https://www.Kimi.com/chat')).toBe('www.kimi.com');
    expect(httpHost('http://bank.com:8443/')).toBe('bank.com');
    expect(httpHost('about:blank')).toBeNull();
    expect(httpHost('chrome://settings')).toBeNull();
    expect(httpHost('not a url')).toBeNull();
    expect(httpHost(undefined)).toBeNull();
  });

  it('reads the host from what an agent passes to find_tab', () => {
    expect(queryHost('kimi.com')).toBe('kimi.com');
    expect(queryHost('www.kimi.com/chat')).toBe('www.kimi.com');
    expect(queryHost('https://kimi.com/chat?x=1')).toBe('kimi.com');
  });

  it('matches a host and its subdomains, never a lookalike suffix', () => {
    expect(hostMatches('kimi.com', 'kimi.com')).toBe(true);
    expect(hostMatches('www.kimi.com', 'kimi.com')).toBe(true);
    expect(hostMatches('notkimi.com', 'kimi.com')).toBe(false);
    expect(hostMatches('kimi.com.evil.net', 'kimi.com')).toBe(false);
  });

  it('blocks the same URLs as the daemon', () => {
    const blocked = ['bank.com'];
    for (const url of ['https://bank.com/login', 'https://www.bank.com', 'https://BANK.com./', 'https://bank.com:8443/']) {
      expect(isBlocked(url, blocked), url).toBe(true);
    }
    for (const url of ['https://notbank.com/', 'https://bank.com.evil.net/', 'about:blank', undefined]) {
      expect(isBlocked(url, blocked), String(url)).toBe(false);
    }
  });

  it('treats browser pages and extension stores as restricted', () => {
    for (const url of ['chrome://newtab/', 'edge://settings', 'file:///C:/x.txt', 'https://chromewebstore.google.com/detail/x', 'https://chrome.google.com/webstore/x']) {
      expect(isRestricted(url), url).toBe(true);
    }
    for (const url of ['about:blank', 'https://kimi.com/', 'http://localhost:3000/']) {
      expect(isRestricted(url), url).toBe(false);
    }
  });
});
```

`extension/src/background/sessions.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { SessionStore } from './sessions';
import { memoryStorage } from './test-support';

describe('SessionStore', () => {
  it('returns an empty session for an unknown name without storing it', async () => {
    const area = memoryStorage();
    const store = new SessionStore(area);
    expect(await store.get('a')).toEqual({ groupId: null, tabIds: [], borrowedTabIds: [], currentTabId: null, stopped: false });
    expect(area.data).toEqual({});
  });

  it('persists updates', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => {
      s.tabIds.push(7);
      s.currentTabId = 7;
    });
    expect((await store.get('a')).currentTabId).toBe(7);
  });

  it('loses no update when many run at once', async () => {
    const store = new SessionStore(memoryStorage());
    await Promise.all(
      Array.from({ length: 20 }, (_, i) =>
        store.update(`s${i % 4}`, (s) => {
          s.tabIds.push(i);
        }),
      ),
    );
    const all = await store.all();
    expect(Object.values(all).flatMap((s) => s.tabIds).sort((a, b) => a - b)).toEqual(Array.from({ length: 20 }, (_, i) => i));
  });

  it('forgets a closed tab in every session that held it', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => {
      s.tabIds = [1, 2];
      s.currentTabId = 1;
    });
    await store.update('b', (s) => {
      s.borrowedTabIds = [1];
      s.currentTabId = 1;
    });
    await store.update('c', (s) => {
      s.tabIds = [3];
      s.currentTabId = 3;
    });
    expect((await store.forgetTab(1)).sort()).toEqual(['a', 'b']);
    expect(await store.get('a')).toMatchObject({ tabIds: [2], currentTabId: null });
    expect(await store.get('b')).toMatchObject({ borrowedTabIds: [], currentTabId: null });
    expect(await store.get('c')).toMatchObject({ tabIds: [3], currentTabId: 3 });
  });

  it('stops only the sessions holding the cancelled tab', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => void s.tabIds.push(1));
    await store.update('b', (s) => void s.tabIds.push(2));
    expect(await store.stopSessionsOf(1)).toEqual(['a']);
    expect((await store.get('a')).stopped).toBe(true);
    expect((await store.get('b')).stopped).toBe(false);
  });

  it('removes a session', async () => {
    const store = new SessionStore(memoryStorage());
    await store.update('a', (s) => void s.tabIds.push(1));
    await store.remove('a');
    expect(await store.all()).toEqual({});
  });
});
```

`extension/src/background/log.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { KEYS, type LogEntry } from '../shared/state';
import { CommandLog } from './log';
import { memoryStorage } from './test-support';

describe('CommandLog', () => {
  it('keeps the newest entries first, up to the limit', async () => {
    const area = memoryStorage();
    const log = new CommandLog(area, 3);
    await Promise.all([1, 2, 3, 4, 5].map((i) => log.add({ at: i, session: 's', action: 'click', ms: 1 })));
    expect((area.data[KEYS.log] as LogEntry[]).map((e) => e.at)).toEqual([5, 4, 3]);
  });
});
```

- [ ] **Step 2: Chạy test, xác nhận fail**

Run: `npm --prefix extension test`
Expected: FAIL, `Failed to resolve import "./hosts"` (tương tự cho `./sessions`, `./log`)

- [ ] **Step 3: Viết `errors.ts`**

```ts
import type { ErrorBody } from '../generated/protocol';

export type ErrorCode = ErrorBody['code'];

/** An error that reaches the agent as {code, message, hint}. Anything else becomes INTERNAL. */
export class BridgeError extends Error {
  constructor(
    readonly code: ErrorCode,
    message: string,
    readonly hint?: string,
  ) {
    super(message);
  }
}

export function toErrorBody(e: unknown): ErrorBody {
  if (e instanceof BridgeError) {
    return e.hint ? { code: e.code, message: e.message, hint: e.hint } : { code: e.code, message: e.message };
  }
  return { code: 'INTERNAL', message: e instanceof Error ? e.message : String(e) };
}

export function blockedHost(): BridgeError {
  // No URL or title in the message: the point of blocking is that the agent learns nothing of the page.
  return new BridgeError('BLOCKED_HOST', 'the tab is on a host in blockedHosts', 'The user has blocked this site. Do not try to reach it another way');
}

export function noCurrentTab(session: string): BridgeError {
  return new BridgeError('NO_CURRENT_TAB', `session ${session} has no current tab`, 'Use navigate to open a page, or find_tab to pick an existing tab');
}
```

- [ ] **Step 4: Viết `hosts.ts`**

```ts
// Host rules shared by blockedHosts and find_tab. They mirror the daemon's checks.go, so a host the
// daemon refuses at navigate is also refused when a page redirects there.

export function normalizeHost(host: string): string {
  return host.toLowerCase().replace(/\.$/, '');
}

/** Host of an http(s) URL; null for about:, chrome:, data: and anything unparsable. */
export function httpHost(url: string | undefined): string | null {
  if (!url) return null;
  try {
    const u = new URL(url);
    return u.protocol === 'http:' || u.protocol === 'https:' ? normalizeHost(u.hostname) : null;
  } catch {
    return null;
  }
}

/** Reads the host from what an agent passes to find_tab: "kimi.com", "www.kimi.com/chat" or a full URL. */
export function queryHost(query: string): string | null {
  return httpHost(query.includes('://') ? query : `http://${query}`);
}

/** True for the host itself and any subdomain of it. */
export function hostMatches(host: string, base: string): boolean {
  return host === base || host.endsWith('.' + base);
}

export function isBlocked(url: string | undefined, blockedHosts: readonly string[]): boolean {
  const host = httpHost(url);
  return host !== null && blockedHosts.some((b) => hostMatches(host, b));
}

/** Pages an extension cannot drive: browser pages and the extension stores. */
export function isRestricted(url: string | undefined): boolean {
  if (!url || url === 'about:blank') return false;
  let u: URL;
  try {
    u = new URL(url);
  } catch {
    return true;
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return true;
  const host = normalizeHost(u.hostname);
  return (
    host === 'chromewebstore.google.com' ||
    (host === 'chrome.google.com' && u.pathname.startsWith('/webstore')) ||
    (host === 'microsoftedge.microsoft.com' && u.pathname.startsWith('/addons'))
  );
}
```

- [ ] **Step 5: Viết `sessions.ts`**

```ts
import { KEYS, type SessionState, type Sessions, type StorageLike } from '../shared/state';

function emptySession(): SessionState {
  return { groupId: null, tabIds: [], borrowedTabIds: [], currentTabId: null, stopped: false };
}

/**
 * Session state in one storage key. Commands of different sessions and tab events run
 * concurrently, so every read-modify-write goes through one promise chain; without it two updates
 * would read the same snapshot and the second write would drop the first.
 */
export class SessionStore {
  private chain: Promise<unknown> = Promise.resolve();

  constructor(private readonly area: StorageLike) {}

  async all(): Promise<Sessions> {
    return ((await this.area.get(KEYS.sessions))[KEYS.sessions] as Sessions | undefined) ?? {};
  }

  async get(name: string): Promise<SessionState> {
    return (await this.all())[name] ?? emptySession();
  }

  update(name: string, change: (s: SessionState) => void): Promise<SessionState> {
    return this.exclusive(async () => {
      const all = await this.all();
      const s = all[name] ?? emptySession();
      change(s);
      all[name] = s;
      await this.area.set({ [KEYS.sessions]: all });
      return s;
    });
  }

  remove(name: string): Promise<void> {
    return this.exclusive(async () => {
      const all = await this.all();
      delete all[name];
      await this.area.set({ [KEYS.sessions]: all });
    });
  }

  /** Drops a tab from every session, e.g. after the user closed it. Returns the sessions that held it. */
  forgetTab(tabId: number): Promise<string[]> {
    return this.eachHolding(tabId, (s) => {
      s.tabIds = s.tabIds.filter((t) => t !== tabId);
      s.borrowedTabIds = s.borrowedTabIds.filter((t) => t !== tabId);
      if (s.currentTabId === tabId) s.currentTabId = null;
    });
  }

  /** Stops every session holding the tab: the user pressed Cancel on its debugging bar. */
  stopSessionsOf(tabId: number): Promise<string[]> {
    return this.eachHolding(tabId, (s) => {
      s.stopped = true;
    });
  }

  private eachHolding(tabId: number, change: (s: SessionState) => void): Promise<string[]> {
    return this.exclusive(async () => {
      const all = await this.all();
      const touched = Object.entries(all).filter(([, s]) => s.tabIds.includes(tabId) || s.borrowedTabIds.includes(tabId));
      for (const [, s] of touched) change(s);
      if (touched.length > 0) await this.area.set({ [KEYS.sessions]: all });
      return touched.map(([name]) => name);
    });
  }

  private exclusive<T>(fn: () => Promise<T>): Promise<T> {
    const run = this.chain.then(fn, fn);
    this.chain = run.catch(() => {});
    return run;
  }
}
```

- [ ] **Step 6: Viết `log.ts`**

```ts
import { KEYS, type LogEntry, type StorageLike } from '../shared/state';

/** The side panel's list of recent commands, newest first. Only the selector of the args is kept. */
export class CommandLog {
  private chain: Promise<void> = Promise.resolve();

  constructor(
    private readonly area: StorageLike,
    private readonly max = 50,
  ) {}

  add(entry: LogEntry): Promise<void> {
    this.chain = this.chain
      .then(async () => {
        const current = ((await this.area.get(KEYS.log))[KEYS.log] as LogEntry[] | undefined) ?? [];
        await this.area.set({ [KEYS.log]: [entry, ...current].slice(0, this.max) });
      })
      .catch(() => {});
    return this.chain;
  }
}
```

- [ ] **Step 7: Chạy test và typecheck**

```bash
npm --prefix extension test
npm --prefix extension run typecheck
```

Expected: 3 file test pass (13 test), `tsc` không lỗi.

- [ ] **Step 8: Commit**

```bash
git add extension/src/background
git commit -m "feat(extension): add host rules, session store and command log"
```

---

### Task 4: Kết nối WebSocket tới daemon

**Files:**
- Create: `extension/src/background/connection.ts`
- Test: `extension/src/background/connection.test.ts`

**Interfaces:**
- Consumes: `Hello`, `Welcome`, `RequestFrame`, `ResponseFrame` (Task 2), `ConnectionStatus` (Task 2).
- Produces:
  - `backoffMs(attempt: number): number`, `PING_INTERVAL_MS = 20000`
  - `interface SocketLike`, `interface ConnectionOptions { url(); hello(); onWelcome(w); onRequest(f); onStatus(s); openSocket?(url) }`
  - `class Connection(opts)` với `connect()` (không mở socket thứ hai), `reconnect()` (bỏ socket cũ, nối lại ngay), getter `connected`
  - Close code 4400/4409/1006 được đổi thành `ConnectionStatus.error` dễ hiểu

- [ ] **Step 1: Viết test fail**

`extension/src/background/connection.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { RequestFrame, ResponseFrame, Welcome } from '../generated/protocol';
import type { ConnectionStatus } from '../shared/state';
import { backoffMs, Connection, PING_INTERVAL_MS, type SocketLike } from './connection';

class FakeSocket implements SocketLike {
  readyState = 0;
  sent: string[] = [];
  onopen: SocketLike['onopen'] = null;
  onmessage: SocketLike['onmessage'] = null;
  onclose: SocketLike['onclose'] = null;
  constructor(readonly url: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    this.readyState = 3;
  }
  open() {
    this.readyState = 1;
    this.onopen?.({});
  }
  receive(frame: object) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }
  drop(code: number) {
    this.readyState = 3;
    this.onclose?.({ code });
  }
}

const welcome: Welcome = { type: 'welcome', protocolVersion: 1, daemonVersion: '0.1.0', blockedHosts: ['bank.com'] };

function setup(onRequest = async (f: RequestFrame): Promise<ResponseFrame> => ({ type: 'response', id: f.id, ok: true, data: {} })) {
  const sockets: FakeSocket[] = [];
  const statuses: ConnectionStatus[] = [];
  const welcomes: Welcome[] = [];
  const conn = new Connection({
    url: async () => 'ws://127.0.0.1:9876/ws',
    hello: () => ({ type: 'hello', protocolVersion: 1, extensionVersion: '0.1.0', extensionId: 'x', browser: 'chrome' }),
    onWelcome: (w) => welcomes.push(w),
    onRequest,
    onStatus: (s) => statuses.push(s),
    openSocket: (url) => {
      const s = new FakeSocket(url);
      sockets.push(s);
      return s;
    },
  });
  return { conn, sockets, statuses, welcomes };
}

describe('backoffMs', () => {
  it('doubles from 1s and stops at 30s', () => {
    expect([0, 1, 2, 4, 5, 10].map(backoffMs)).toEqual([1000, 2000, 4000, 16000, 30000, 30000]);
  });
});

describe('Connection', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it('says hello on open and is connected after welcome', async () => {
    const { conn, sockets, statuses, welcomes } = setup();
    await conn.connect();
    sockets[0]!.open();
    expect(JSON.parse(sockets[0]!.sent[0]!)).toMatchObject({ type: 'hello', protocolVersion: 1 });
    expect(conn.connected).toBe(false);
    sockets[0]!.receive(welcome);
    expect(conn.connected).toBe(true);
    expect(welcomes).toEqual([welcome]);
    expect(statuses.at(-1)).toEqual({ state: 'connected', url: 'ws://127.0.0.1:9876/ws', daemonVersion: '0.1.0', blockedHosts: ['bank.com'] });
  });

  it('pings every 20 seconds once connected', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    vi.advanceTimersByTime(PING_INTERVAL_MS * 2);
    expect(sockets[0]!.sent.filter((s) => s === '{"type":"ping"}')).toHaveLength(2);
  });

  it('answers requests on the same socket', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    sockets[0]!.receive({ type: 'request', id: '42', session: 's', action: 'list_tabs', args: {}, deadline: 0 });
    await vi.waitFor(() => expect(sockets[0]!.sent.some((s) => s.includes('"id":"42"'))).toBe(true));
  });

  it('drops the answer when the socket closed while the command ran', async () => {
    let finish!: (r: ResponseFrame) => void;
    const { conn, sockets } = setup(() => new Promise((r) => (finish = r)));
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    sockets[0]!.receive({ type: 'request', id: '1', session: 's', action: 'reload', args: {}, deadline: 0 });
    sockets[0]!.drop(1006);
    finish({ type: 'response', id: '1', ok: true, data: {} });
    await Promise.resolve();
    expect(sockets[0]!.sent.some((s) => s.includes('"response"'))).toBe(false);
  });

  it('explains refusals and reconnects with growing delays', async () => {
    const { conn, sockets, statuses } = setup();
    await conn.connect();
    sockets[0]!.drop(4409);
    expect(statuses.at(-1)).toMatchObject({ state: 'disconnected' });
    expect(statuses.at(-1)!.error).toContain('already connected');
    await vi.advanceTimersByTimeAsync(999);
    expect(sockets).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(2);
    sockets[1]!.drop(1006);
    expect(statuses.at(-1)!.error).toBe('cannot reach the daemon');
    await vi.advanceTimersByTimeAsync(1999);
    expect(sockets).toHaveLength(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(sockets).toHaveLength(3);
    // a welcome resets the delay
    sockets[2]!.open();
    sockets[2]!.receive(welcome);
    sockets[2]!.drop(1006);
    await vi.advanceTimersByTimeAsync(1000);
    expect(sockets).toHaveLength(4);
  });

  it('names a protocol version mismatch', async () => {
    const { conn, sockets, statuses } = setup();
    await conn.connect();
    sockets[0]!.drop(4400);
    expect(statuses.at(-1)!.error).toContain('protocol version');
  });

  it('opens one socket however often connect is called', async () => {
    const { conn, sockets } = setup();
    await Promise.all([conn.connect(), conn.connect(), conn.connect()]);
    expect(sockets).toHaveLength(1);
  });

  it('reconnect replaces the socket at once and ignores the old one closing', async () => {
    const { conn, sockets } = setup();
    await conn.connect();
    sockets[0]!.open();
    sockets[0]!.receive(welcome);
    conn.reconnect();
    await vi.waitFor(() => expect(sockets).toHaveLength(2));
    sockets[0]!.drop(1000);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(sockets).toHaveLength(2);
  });
});
```

- [ ] **Step 2: Chạy test, xác nhận fail**

Run: `npm --prefix extension test -- connection`
Expected: FAIL, `Failed to resolve import "./connection"`

- [ ] **Step 3: Viết `connection.ts`**

```ts
import type { Hello, RequestFrame, ResponseFrame, Welcome } from '../generated/protocol';
import type { ConnectionStatus } from '../shared/state';

export const PING_INTERVAL_MS = 20_000;

/** 1s, 2s, 4s ... capped at 30s. */
export function backoffMs(attempt: number): number {
  return Math.min(30_000, 1000 * 2 ** attempt);
}

/** The slice of WebSocket the connection uses, so tests can drive it without a server. */
export interface SocketLike {
  readonly readyState: number;
  send(data: string): void;
  close(): void;
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: { code: number }) => void) | null;
}

export interface ConnectionOptions {
  url(): Promise<string>;
  hello(): Hello;
  onWelcome(w: Welcome): void;
  onRequest(f: RequestFrame): Promise<ResponseFrame>;
  onStatus(s: ConnectionStatus): void;
  openSocket?(url: string): SocketLike;
}

const OPEN = 1;

const closeReasons: Record<number, string> = {
  4400: 'the daemon speaks another protocol version: update the extension or the daemon',
  4409: 'another Browser Bridge extension is already connected to the daemon',
};

/**
 * The single WebSocket to the daemon: hello/welcome handshake, a ping every 20s (an active socket
 * keeps the service worker alive), and reconnection with backoff after every close.
 */
export class Connection {
  private socket: SocketLike | null = null;
  private attempt = 0;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private pingTimer: ReturnType<typeof setInterval> | null = null;

  constructor(private readonly opts: ConnectionOptions) {}

  get connected(): boolean {
    return this.pingTimer !== null;
  }

  /** Connects unless a socket is already open or opening. Safe to call from alarms. */
  async connect(): Promise<void> {
    if (this.socket) return;
    this.clearRetry();
    const url = await this.opts.url();
    if (this.socket) return; // another connect() won while the URL was being read
    this.opts.onStatus({ state: 'connecting', url });
    const socket = (this.opts.openSocket ?? ((u) => new WebSocket(u) as unknown as SocketLike))(url);
    this.socket = socket;
    socket.onopen = () => socket.send(JSON.stringify(this.opts.hello()));
    socket.onmessage = (ev) => this.onMessage(socket, url, ev.data);
    socket.onclose = (ev) => this.onClose(socket, url, ev.code);
  }

  /** Drops the current socket and connects again at once, e.g. after the daemon URL changed. */
  reconnect(): void {
    const old = this.socket;
    this.socket = null;
    this.stopPing();
    this.attempt = 0;
    old?.close();
    void this.connect();
  }

  private onMessage(socket: SocketLike, url: string, raw: unknown): void {
    let frame: { type?: string };
    try {
      frame = JSON.parse(String(raw));
    } catch {
      return;
    }
    if (frame.type === 'welcome') {
      const w = frame as Welcome;
      this.attempt = 0;
      this.opts.onWelcome(w);
      this.opts.onStatus({ state: 'connected', url, daemonVersion: w.daemonVersion, blockedHosts: w.blockedHosts });
      this.stopPing();
      this.pingTimer = setInterval(() => socket.send('{"type":"ping"}'), PING_INTERVAL_MS);
    } else if (frame.type === 'request') {
      void this.opts.onRequest(frame as RequestFrame).then((resp) => {
        if (socket.readyState === OPEN) socket.send(JSON.stringify(resp));
      });
    }
  }

  private onClose(socket: SocketLike, url: string, code: number): void {
    if (this.socket !== socket) return; // replaced by reconnect()
    this.socket = null;
    this.stopPing();
    this.opts.onStatus({ state: 'disconnected', url, error: closeReasons[code] ?? (code === 1006 ? 'cannot reach the daemon' : undefined) });
    this.retryTimer = setTimeout(() => {
      this.retryTimer = null;
      void this.connect();
    }, backoffMs(this.attempt++));
  }

  private stopPing(): void {
    if (this.pingTimer !== null) clearInterval(this.pingTimer);
    this.pingTimer = null;
  }

  private clearRetry(): void {
    if (this.retryTimer !== null) clearTimeout(this.retryTimer);
    this.retryTimer = null;
  }
}
```

- [ ] **Step 4: Chạy test, xác nhận pass**

Run: `npm --prefix extension test -- connection`
Expected: 9 test pass

- [ ] **Step 5: Commit**

```bash
git add extension/src/background
git commit -m "feat(extension): connect to the daemon with handshake, ping and backoff"
```

---

### Task 5: Router và các luật chung của mọi action

**Files:**
- Create: `extension/src/background/router.ts`
- Test: `extension/src/background/router.test.ts`

**Interfaces:**
- Consumes: `SessionStore`, `BridgeError`, `blockedHost`, `toErrorBody`, `isBlocked` (Task 3).
- Produces:
  - `interface Ctx { session; deadline; sessions; blockedHosts() }`, `type Handler<C extends Ctx> = (ctx: C, args: any) => Promise<unknown>`
  - `createRouter<C>(deps: { sessions; handlers; makeCtx(session, deadline); tabUrl(tabId); blockedHosts() }) → (frame: RequestFrame) => Promise<ResponseFrame>`
  - Thứ tự luật: action chưa có handler → `INTERNAL`; session bị dừng → `DETACHED_BY_USER`, trừ dọn dẹp, `navigate`, `find_tab`; tab hiện tại ở host bị chặn → `BLOCKED_HOST`, trừ dọn dẹp và các lệnh rời trang (`navigate`, `find_tab`, `go_back`, `go_forward`); rồi mới gọi handler. Kết quả rỗng thành `{}`.

- [ ] **Step 1: Viết test fail**

`extension/src/background/router.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import type { RequestFrame } from '../generated/protocol';
import { BridgeError } from './errors';
import { createRouter, type Ctx, type Handler } from './router';
import { SessionStore } from './sessions';
import { memoryStorage } from './test-support';

function frame(action: string, args: unknown = {}, session = 's1'): RequestFrame {
  return { type: 'request', id: '7', session, action, args, deadline: Date.now() + 1000 };
}

function setup(tabUrls: Record<number, string> = {}) {
  const sessions = new SessionStore(memoryStorage());
  const calls: { action: string; ctx: Ctx; args: unknown }[] = [];
  const ok = (action: string): Handler => async (ctx, args) => {
    calls.push({ action, ctx, args });
    return { action };
  };
  const names = ['navigate', 'find_tab', 'list_tabs', 'close_tab', 'close_session', 'go_back', 'go_forward', 'reload'] as const;
  const handlers: Partial<Record<string, Handler>> = Object.fromEntries(names.map((n) => [n, ok(n)]));
  handlers.snapshot = async () => undefined;
  handlers.click = async () => {
    throw new BridgeError('STALE_REF', '@e1 is gone', 'Take a new snapshot');
  };
  handlers.fill = async () => {
    throw new Error('boom');
  };
  const route = createRouter({
    sessions,
    handlers,
    blockedHosts: () => ['bank.com'],
    tabUrl: async (id) => tabUrls[id],
    makeCtx: (session, deadline) => ({ session, deadline, sessions, blockedHosts: () => ['bank.com'] }),
  });
  return { sessions, calls, route };
}

describe('router', () => {
  it('answers with the handler result and the request id', async () => {
    const { route, calls } = setup();
    const resp = await route(frame('navigate', { url: 'https://a.com' }));
    expect(resp).toEqual({ type: 'response', id: '7', ok: true, data: { action: 'navigate' } });
    expect(calls[0]!.args).toEqual({ url: 'https://a.com' });
    expect(calls[0]!.ctx.session).toBe('s1');
  });

  it('turns an empty result into {}', async () => {
    const { route } = setup();
    expect((await route(frame('snapshot'))).data).toEqual({});
  });

  it('passes BridgeError codes and hints through and wraps anything else as INTERNAL', async () => {
    const { route } = setup();
    expect((await route(frame('click'))).error).toEqual({ code: 'STALE_REF', message: '@e1 is gone', hint: 'Take a new snapshot' });
    expect((await route(frame('fill'))).error).toEqual({ code: 'INTERNAL', message: 'boom' });
  });

  it('reports actions this version does not implement', async () => {
    const { route } = setup();
    const resp = await route(frame('screenshot'));
    expect(resp.ok).toBe(false);
    expect(resp.error?.code).toBe('INTERNAL');
    expect(resp.error?.message).toContain('screenshot');
  });

  it('refuses everything but cleanup, navigate and find_tab after the user pressed Cancel', async () => {
    const { route, sessions } = setup();
    await sessions.update('s1', (s) => {
      s.stopped = true;
    });
    expect((await route(frame('reload'))).error?.code).toBe('DETACHED_BY_USER');
    expect((await route(frame('go_back'))).error?.code).toBe('DETACHED_BY_USER');
    for (const action of ['list_tabs', 'close_tab', 'close_session', 'navigate', 'find_tab']) {
      expect((await route(frame(action))).ok, action).toBe(true);
    }
  });

  it('refuses page actions while the current tab is on a blocked host, but lets the agent leave', async () => {
    const { route, sessions } = setup({ 5: 'https://www.bank.com/accounts' });
    await sessions.update('s1', (s) => {
      s.tabIds = [5];
      s.currentTabId = 5;
    });
    const resp = await route(frame('reload'));
    expect(resp.error?.code).toBe('BLOCKED_HOST');
    expect(JSON.stringify(resp)).not.toContain('bank.com');
    for (const action of ['navigate', 'find_tab', 'go_back', 'go_forward', 'list_tabs', 'close_tab', 'close_session']) {
      expect((await route(frame(action))).ok, action).toBe(true);
    }
  });

  it('does not check hosts when the session has no current tab', async () => {
    const { route } = setup();
    expect((await route(frame('reload'))).ok).toBe(true);
  });
});
```

- [ ] **Step 2: Chạy test, xác nhận fail**

Run: `npm --prefix extension test -- router`
Expected: FAIL, `Failed to resolve import "./router"`

- [ ] **Step 3: Viết `router.ts`**

```ts
import type { ActionName, RequestFrame, ResponseFrame } from '../generated/protocol';
import { BridgeError, blockedHost, toErrorBody } from './errors';
import { isBlocked } from './hosts';
import type { SessionStore } from './sessions';

/** What every action handler gets besides its args. */
export interface Ctx {
  session: string;
  /** Epoch ms after which the daemon has already answered TIMEOUT. */
  deadline: number;
  sessions: SessionStore;
  blockedHosts(): readonly string[];
}

export type Handler<C extends Ctx = Ctx> = (ctx: C, args: any) => Promise<unknown>;

// Spec §6.1 and §7.1: these never get stuck behind a dialog, a Cancel or a blocked host, so an
// agent can always get out of a tab.
const CLEANUP = new Set<string>(['list_tabs', 'close_tab', 'close_session']);
// Moving away from a blocked page has to work; their result is checked after the page loads.
const LEAVES_PAGE = new Set<string>(['navigate', 'find_tab', 'go_back', 'go_forward']);
// The two actions that resume a session the user stopped with Cancel.
const RESUMES = new Set<string>(['navigate', 'find_tab']);

export interface RouterDeps<C extends Ctx> {
  sessions: SessionStore;
  handlers: Partial<Record<ActionName, Handler<C>>>;
  makeCtx(session: string, deadline: number): C;
  tabUrl(tabId: number): Promise<string | undefined>;
  blockedHosts(): readonly string[];
}

export function createRouter<C extends Ctx>(deps: RouterDeps<C>): (frame: RequestFrame) => Promise<ResponseFrame> {
  async function run(frame: RequestFrame): Promise<unknown> {
    const action = frame.action;
    const handler = deps.handlers[action as ActionName];
    if (!handler) throw new BridgeError('INTERNAL', `${action} is not implemented in this extension version`);
    const s = await deps.sessions.get(frame.session);
    if (s.stopped && !CLEANUP.has(action) && !RESUMES.has(action)) {
      throw new BridgeError(
        'DETACHED_BY_USER',
        `the user pressed Cancel on the debugging bar of session ${frame.session}`,
        'Ask the user before continuing. navigate or find_tab resumes the session',
      );
    }
    if (!CLEANUP.has(action) && !LEAVES_PAGE.has(action) && s.currentTabId !== null) {
      if (isBlocked(await deps.tabUrl(s.currentTabId), deps.blockedHosts())) throw blockedHost();
    }
    return handler(deps.makeCtx(frame.session, frame.deadline), frame.args ?? {});
  }

  return async (frame) => {
    try {
      const data = await run(frame);
      return { type: 'response', id: frame.id, ok: true, data: data ?? {} };
    } catch (e) {
      return { type: 'response', id: frame.id, ok: false, error: toErrorBody(e) };
    }
  };
}
```

- [ ] **Step 4: Chạy toàn bộ unit test và typecheck**

```bash
npm --prefix extension test
npm --prefix extension run typecheck
```

Expected: 5 file test pass (29 test), `tsc` không lỗi.

- [ ] **Step 5: Commit**

```bash
git add extension/src/background
git commit -m "feat(extension): route commands through the stopped-session and blocked-host rules"
```

---

### Task 6: Action về tab, chạy E2E trên daemon thật

**Files:**
- Create: `testpage/index.html`, `testpage/page2.html`
- Create: `e2e/package.json`, `e2e/package-lock.json` (qua `npm install`)
- Create: `e2e/playwright.config.ts`, `e2e/tsconfig.json`, `e2e/paths.ts`, `e2e/global-setup.ts`, `e2e/site.ts`, `e2e/fixtures.ts`
- Test: `e2e/tests/tabs.spec.ts`, `e2e/tests/keepalive.spec.ts`
- Create: `extension/src/background/cdp.ts`
- Create: `extension/src/background/actions/tabs.ts`
- Modify: `extension/src/entrypoints/background.ts` (thay toàn bộ)

**Interfaces:**
- Consumes: mọi module của Task 3–5, `bridge serve` (plan 01).
- Produces:
  - `cdp.ts`: `class Cdp` với `restore(tabIds)`, `attach(tabId)` (bật `Page`, `Runtime`, focus emulation), `detach(tabId)`, `send<T>(tabId, method, params?)`, `waitForEvent<T>(tabId, match, deadline) → { done, cancel }`. Lỗi attach được đổi thành `TAB_NOT_FOUND`, `CDP_ERROR` (DevTools đang mở) hoặc `RESTRICTED_URL`.
  - `actions/tabs.ts`: `interface TabCtx extends Ctx { cdp: Cdp }`, `tabHandlers` cho 8 action. `navigate` tạo tab trên `about:blank`, `Page.navigate`, chờ `Page.loadEventFired` (bỏ qua khi chỉ đổi `#fragment`), xoá lịch sử của tab mới, kiểm tra lại URL cuối với `blockedHosts`.
  - Fixture E2E: `bridge` (`command(session, action, args?, timeoutMs?)`, `status()`, `log()`), `browserCtx`, `sw` (service worker của extension, dùng để hỏi Chrome), `site` (`url(path, host?)`), `session` (tên ngẫu nhiên mỗi test); helper `ok()` và `tabInfo()`. Plan 03 dùng lại fixture này.

- [ ] **Step 1: Tạo trang test**

`testpage/index.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Bridge test page</title>
</head>
<body>
  <h1>Bridge test page</h1>
  <p><a id="to-page2" href="page2.html">Page 2</a></p>
</body>
</html>
```

`testpage/page2.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Bridge test page 2</title>
</head>
<body>
  <h1>Bridge test page 2</h1>
  <p><a id="to-index" href="index.html">Back to page 1</a></p>
</body>
</html>
```

- [ ] **Step 2: Tạo package `e2e/` và cài Chromium cho Playwright**

`e2e/package.json`:

```json
{
  "name": "browser-bridge-e2e",
  "scripts": {
    "test": "playwright test",
    "typecheck": "tsc"
  },
  "type": "module",
  "devDependencies": {
    "@playwright/test": "^1.63.0",
    "@types/node": "^22.20.5",
    "typescript": "^7.0.2"
  },
  "private": true
}
```

```bash
cd e2e
npm install
npx playwright install chromium
```

`e2e/playwright.config.ts`:

```ts
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
```

`e2e/tsconfig.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "esnext",
    "moduleResolution": "bundler",
    "strict": true,
    "noEmit": true,
    "skipLibCheck": true,
    "types": ["node"]
  },
  "include": ["**/*.ts"],
  "exclude": ["node_modules"]
}
```

- [ ] **Step 3: Viết phần hạ tầng của E2E**

`e2e/paths.ts`:

```ts
import { join } from 'node:path';

const root = join(import.meta.dirname, '..');

// A fixed port that is not the default 9876, so a run never talks to the daemon the user runs.
// The E2E build of the extension has this address compiled in (WXT_DAEMON_URL).
export const DAEMON_PORT = 19876;
export const BRIDGE_BIN = join(root, 'e2e', '.cache', process.platform === 'win32' ? 'bridge.exe' : 'bridge');
export const DAEMON_DIR = join(root, 'daemon');
export const EXTENSION_ROOT = join(root, 'extension');
export const EXTENSION_DIR = join(root, 'extension', '.output', 'chrome-mv3-e2e');
export const TESTPAGE_DIR = join(root, 'testpage');
export const EXTENSION_ID = 'nfjidhefdgblbbfhnmbcogkbphipngif';
```

`e2e/global-setup.ts`: build daemon và bản E2E của extension trước mỗi lần chạy. Biến môi trường `GO` trỏ tới `go` nếu nó không nằm trong `PATH`.

```ts
import { execFileSync } from 'node:child_process';
import { BRIDGE_BIN, DAEMON_DIR, DAEMON_PORT, EXTENSION_ROOT } from './paths';

// Builds what the tests drive: the daemon binary and the E2E build of the extension.
export default function globalSetup() {
  execFileSync(process.env.GO ?? 'go', ['-C', DAEMON_DIR, 'build', '-o', BRIDGE_BIN, './cmd/bridge'], { stdio: 'inherit' });
  execFileSync('npm run build -- --mode e2e', {
    cwd: EXTENSION_ROOT,
    stdio: 'inherit',
    shell: true,
    env: { ...process.env, WXT_DAEMON_URL: `ws://127.0.0.1:${DAEMON_PORT}/ws` },
  });
}
```

`e2e/site.ts`:

```ts
import { readFile } from 'node:fs/promises';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { extname, join, normalize, sep } from 'node:path';
import { TESTPAGE_DIR } from './paths';

export interface Site {
  port: number;
  /** The same server answers on any loopback name: 127.0.0.1, localhost, blocked.localhost. */
  url(path: string, host?: string): string;
  close(): Promise<void>;
}

const types: Record<string, string> = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json',
};

/** Serves testpage/ plus /redirect?to=<url>, which answers 302 to that URL. */
export async function startSite(): Promise<Site> {
  const server = createServer(async (req, res) => {
    const u = new URL(req.url ?? '/', 'http://site');
    if (u.pathname === '/redirect') {
      res.writeHead(302, { Location: u.searchParams.get('to') ?? '/' }).end();
      return;
    }
    const file = normalize(join(TESTPAGE_DIR, u.pathname === '/' ? 'index.html' : u.pathname));
    if (!file.startsWith(TESTPAGE_DIR + sep)) {
      res.writeHead(403).end();
      return;
    }
    try {
      const body = await readFile(file);
      res.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream' }).end(body);
    } catch {
      res.writeHead(404).end('not found');
    }
  });
  await new Promise<void>((resolve) => server.listen(0, '127.0.0.1', resolve));
  const port = (server.address() as AddressInfo).port;
  return {
    port,
    url: (path, host = '127.0.0.1') => `http://${host}:${port}${path}`,
    close: () =>
      new Promise((resolve) => {
        server.closeAllConnections();
        server.close(() => resolve());
      }),
  };
}
```

`e2e/fixtures.ts`. Hai điểm dễ sai: fixture `bridge` phụ thuộc `browserCtx`, và teardown chờ daemon thoát hẳn. Thiếu một trong hai thì test đầu tiên của worker mới (Playwright dựng lại worker sau mỗi test fail) nhận `EXTENSION_NOT_CONNECTED`.

```ts
import { test as base, chromium, expect, type BrowserContext, type Worker } from '@playwright/test';
import { spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { once } from 'node:events';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { BRIDGE_BIN, DAEMON_PORT, EXTENSION_DIR } from './paths';
import { startSite, type Site } from './site';

// Code passed to sw.evaluate runs in the extension's service worker, where chrome.* exists.
declare const chrome: any;

export interface Envelope<T = any> {
  ok: boolean;
  data?: T;
  error?: { code: string; message: string; hint?: string };
}

/** The daemon's HTTP API, the way an agent calls it. */
export class Bridge {
  constructor(
    readonly base: string,
    /** The daemon's log so far. */
    readonly log: () => string = () => '',
  ) {}

  async command<T = any>(session: string, action: string, args: object = {}, timeoutMs?: number): Promise<Envelope<T>> {
    const res = await fetch(`${this.base}/command`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, args, session, timeoutMs }),
    });
    return (await res.json()) as Envelope<T>;
  }

  async status(): Promise<any> {
    return (await fetch(`${this.base}/status`)).json();
  }
}

/** Asserts ok and returns data, printing the error when there is one. */
export async function ok<T = any>(p: Promise<Envelope<T>>): Promise<T> {
  const env = await p;
  expect(env.error, JSON.stringify(env.error)).toBeUndefined();
  expect(env.ok).toBe(true);
  return env.data as T;
}

export interface TabInfo {
  active: boolean;
  url: string;
  groupTitle: string | null;
}

/** What Chrome itself reports about a tab, or null once it is closed. */
export function tabInfo(sw: Worker, tabId: number): Promise<TabInfo | null> {
  return sw.evaluate(async (id: number) => {
    const t = await chrome.tabs.get(id).catch(() => null);
    if (!t) return null;
    const group = t.groupId === -1 ? null : await chrome.tabGroups.get(t.groupId);
    return { active: t.active, url: t.url, groupTitle: group ? group.title : null };
  }, tabId);
}

type WorkerFixtures = { site: Site; daemon: Bridge; browserCtx: BrowserContext; bridge: Bridge; sw: Worker };
type TestFixtures = { session: string };

export const test = base.extend<TestFixtures, WorkerFixtures>({
  site: [
    async ({}, use) => {
      const site = await startSite();
      await use(site);
      await site.close();
    },
    { scope: 'worker' },
  ],

  daemon: [
    async ({}, use) => {
      const home = mkdtempSync(join(tmpdir(), 'bridge-e2e-'));
      writeFileSync(join(home, 'config.json'), JSON.stringify({ blockedHosts: ['blocked.localhost'] }));
      const proc = spawn(BRIDGE_BIN, ['serve', '--addr', `127.0.0.1:${DAEMON_PORT}`], {
        env: { ...process.env, BRIDGE_HOME: home },
        stdio: ['ignore', 'ignore', 'pipe'],
      });
      let stderr = '';
      proc.stderr!.on('data', (d) => (stderr += d));
      const bridge = new Bridge(`http://127.0.0.1:${DAEMON_PORT}`, () => stderr);
      await expect
        .poll(() => bridge.status().then((s) => s.running, () => false), { timeout: 10_000, message: `daemon did not start:\n${stderr}` })
        .toBe(true);
      // A daemon left over from the previous worker would also answer /status; ours must still be alive.
      expect(proc.exitCode, `daemon exited:\n${stderr}`).toBeNull();
      await use(bridge);
      // Wait for the exit: after a failed test Playwright starts a new worker at once, and its daemon
      // needs the port this one holds.
      const exited = once(proc, 'exit');
      proc.kill();
      await exited;
    },
    { scope: 'worker' },
  ],

  browserCtx: [
    async ({ daemon }, use) => {
      const ctx = await chromium.launchPersistentContext(mkdtempSync(join(tmpdir(), 'bridge-e2e-profile-')), {
        // The "chromium" channel runs new headless mode, which loads extensions; HEADED=1 shows the window.
        channel: 'chromium',
        headless: process.env.HEADED !== '1',
        args: [`--disable-extensions-except=${EXTENSION_DIR}`, `--load-extension=${EXTENSION_DIR}`],
      });
      await expect.poll(async () => (await daemon.status()).extension.connected, { timeout: 15_000, message: 'extension never connected' }).toBe(true);
      await use(ctx);
      await ctx.close();
    },
    { scope: 'worker' },
  ],

  // Tests talk to the daemon through this fixture, so a connected browser is always behind it, even
  // in a fresh worker after a failed test.
  bridge: [
    async ({ daemon, browserCtx }, use) => {
      expect(browserCtx).toBeTruthy();
      await use(daemon);
    },
    { scope: 'worker' },
  ],

  sw: [
    async ({ browserCtx }, use) => {
      const sw = browserCtx.serviceWorkers()[0] ?? (await browserCtx.waitForEvent('serviceworker'));
      await use(sw);
    },
    { scope: 'worker' },
  ],

  // The browser is shared by every test of a worker, so each test gets its own session.
  session: async ({}, use) => {
    await use(`e2e-${randomBytes(4).toString('hex')}`);
  },
});

export { expect };
```

- [ ] **Step 4: Viết test E2E**

`e2e/tests/tabs.spec.ts`:

```ts
import { expect, ok, tabInfo, test } from '../fixtures';

declare const chrome: any;

test('the extension connects and identifies itself', async ({ bridge }) => {
  const st = await bridge.status();
  expect(st.extension).toMatchObject({ connected: true, id: 'nfjidhefdgblbbfhnmbcogkbphipngif', version: '0.1.0', browser: 'chrome' });
});

test('navigate opens one background tab per session, grouped under the session name', async ({ bridge, session, site, sw }) => {
  const first = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  expect(first).toMatchObject({ url: site.url('/index.html'), title: 'Bridge test page' });
  expect(await tabInfo(sw, first.tabId)).toMatchObject({ active: false, groupTitle: session });

  const reused = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(reused).toMatchObject({ tabId: first.tabId, title: 'Bridge test page 2' });

  const second = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html'), newTab: true }));
  expect(second.tabId).not.toBe(first.tabId);
  expect(await tabInfo(sw, second.tabId)).toMatchObject({ active: false, groupTitle: session });
});

test('groupTitle names the group when it is created', async ({ bridge, session, site, sw }) => {
  const r = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html'), groupTitle: 'Jira report' }));
  expect((await tabInfo(sw, r.tabId))!.groupTitle).toBe('Jira report');
});

test('navigate reports a site that cannot be reached', async ({ bridge, session }) => {
  const r = await bridge.command(session, 'navigate', { url: 'http://127.0.0.1:1/' });
  expect(r.error?.code).toBe('NAVIGATION_FAILED');
});

test('back and forward walk the history and stop at its ends', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(await ok(bridge.command(session, 'go_back'))).toEqual({ url: site.url('/index.html'), title: 'Bridge test page' });
  // the tab was created on about:blank; that entry must not be reachable
  expect((await bridge.command(session, 'go_back')).error?.code).toBe('NAVIGATION_FAILED');
  expect(await ok(bridge.command(session, 'go_forward'))).toEqual({ url: site.url('/page2.html'), title: 'Bridge test page 2' });
  expect((await bridge.command(session, 'go_forward')).error?.code).toBe('NAVIGATION_FAILED');
});

test('history steps inside one document finish', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html#a') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html#b') }));
  expect((await ok(bridge.command(session, 'go_back', {}, 5000))).url).toBe(site.url('/index.html#a'));
});

test('reload answers with the page', async ({ bridge, session, site }) => {
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html') }));
  expect(await ok(bridge.command(session, 'reload'))).toEqual({ url: site.url('/page2.html'), title: 'Bridge test page 2' });
});

test('list_tabs and close_tab', async ({ bridge, session, site, sw }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  const b = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  const listed = await ok(bridge.command(session, 'list_tabs'));
  expect(listed.tabs).toEqual([
    { tabId: a.tabId, url: site.url('/index.html'), title: 'Bridge test page', current: false, borrowed: false },
    { tabId: b.tabId, url: site.url('/page2.html'), title: 'Bridge test page 2', current: true, borrowed: false },
  ]);
  expect(await ok(bridge.command(session, 'close_tab'))).toEqual({ closed: true, released: false });
  expect(await tabInfo(sw, b.tabId)).toBeNull();
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toEqual([
    { tabId: a.tabId, url: site.url('/index.html'), title: 'Bridge test page', current: false, borrowed: false },
  ]);
  expect((await bridge.command(session, 'reload')).error?.code).toBe('NO_CURRENT_TAB');
});

test('find_tab picks a session tab by host', async ({ bridge, session, site }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html', 'localhost') }));
  await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  expect(await ok(bridge.command(session, 'find_tab', { url: 'localhost' }))).toMatchObject({ tabId: a.tabId, borrowed: false });
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs.find((t: any) => t.current).tabId).toBe(a.tabId);
  const missing = await bridge.command(session, 'find_tab', { url: 'example.org' });
  expect(missing.error?.code).toBe('TAB_NOT_FOUND');
  expect(missing.error?.hint).toContain('navigate');
});

test('find_tab active borrows the user tab, leaves it ungrouped, and close_tab only releases it', async ({ bridge, browserCtx, session, site, sw }) => {
  const page = await browserCtx.newPage();
  await page.goto(site.url('/page2.html'));
  await page.bringToFront();
  const r = await ok(bridge.command(session, 'find_tab', { active: true }));
  expect(r).toMatchObject({ url: site.url('/page2.html'), title: 'Bridge test page 2', borrowed: true });
  expect((await tabInfo(sw, r.tabId))!.groupTitle).toBeNull();
  expect(await ok(bridge.command(session, 'reload'))).toMatchObject({ title: 'Bridge test page 2' });
  expect(await ok(bridge.command(session, 'close_tab'))).toEqual({ closed: false, released: true });
  expect(page.isClosed()).toBe(false);
  expect(await tabInfo(sw, r.tabId)).not.toBeNull();
  await page.close();
});

test('find_tab active refuses a tab on another host', async ({ bridge, browserCtx, session, site }) => {
  const page = await browserCtx.newPage();
  await page.goto(site.url('/index.html'));
  await page.bringToFront();
  expect((await bridge.command(session, 'find_tab', { active: true, url: 'example.org' })).error?.code).toBe('TAB_NOT_FOUND');
  await page.close();
});

test('close_session closes its own tabs and gives borrowed ones back', async ({ bridge, browserCtx, session, site, sw }) => {
  const a = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  const b = await ok(bridge.command(session, 'navigate', { url: site.url('/page2.html'), newTab: true }));
  const page = await browserCtx.newPage();
  await page.goto(site.url('/index.html'));
  await page.bringToFront();
  await ok(bridge.command(session, 'find_tab', { active: true }));
  expect(await ok(bridge.command(session, 'close_session'))).toEqual({ closed: 2 });
  expect(await tabInfo(sw, a.tabId)).toBeNull();
  expect(await tabInfo(sw, b.tabId)).toBeNull();
  expect(page.isClosed()).toBe(false);
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toEqual([]);
  await page.close();
});

test('blocked hosts: refused up front, caught after a redirect, and the agent can still leave', async ({ bridge, session, site }) => {
  const direct = await bridge.command(session, 'navigate', { url: site.url('/index.html', 'blocked.localhost') });
  expect(direct.error?.code).toBe('BLOCKED_HOST');

  const viaRedirect = await bridge.command(session, 'navigate', {
    url: site.url('/redirect?to=' + encodeURIComponent(site.url('/index.html', 'blocked.localhost'))),
  });
  expect(viaRedirect.error?.code).toBe('BLOCKED_HOST');
  expect(JSON.stringify(viaRedirect)).not.toContain('Bridge test page');

  expect((await bridge.command(session, 'reload')).error?.code).toBe('BLOCKED_HOST');
  expect((await ok(bridge.command(session, 'list_tabs'))).tabs).toHaveLength(1);
  expect(await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }))).toMatchObject({ title: 'Bridge test page' });
});

test('a tab the user closes leaves the session', async ({ bridge, session, site, sw }) => {
  const r = await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
  await sw.evaluate((id: number) => chrome.tabs.remove(id), r.tabId);
  await expect.poll(async () => (await ok(bridge.command(session, 'list_tabs'))).tabs.length).toBe(0);
  expect((await bridge.command(session, 'reload')).error?.code).toBe('NO_CURRENT_TAB');
});
```

`e2e/tests/keepalive.spec.ts`:

```ts
import { expect, ok, test } from '../fixtures';

// The daemon drops a socket that is silent for 60s, and Chrome stops an idle service worker after
// 30s. The 20s ping has to prevent both.
test('stays connected while idle for longer than the daemon idle timeout', async ({ bridge, session, site }) => {
  test.slow();
  await new Promise((r) => setTimeout(r, 70_000));
  expect((await bridge.status()).extension.connected).toBe(true);
  expect(bridge.log()).not.toContain('extension disconnected');
  await ok(bridge.command(session, 'navigate', { url: site.url('/index.html') }));
});
```

- [ ] **Step 5: Chạy E2E, xác nhận fail**

```bash
npm --prefix e2e test
```

Expected: FAIL ở fixture `browserCtx` với `extension never connected`, vì background vẫn là bản rỗng của Task 2.

- [ ] **Step 6: Viết `cdp.ts`**

`extension/src/background/cdp.ts`:

```ts
import { browser } from 'wxt/browser';
import { BridgeError } from './errors';

interface Waiter {
  onEvent(method: string, params: any): void;
  onDetach(reason: string): void;
}

export interface EventWait<T> {
  done: Promise<T>;
  cancel(): void;
}

const message = (e: unknown) => (e instanceof Error ? e.message : String(e));

/** chrome.debugger for tabs: attach once per tab and keep it, send commands, wait for events. */
export class Cdp {
  private attached = new Set<number>();
  private attaching = new Map<number, Promise<void>>();
  private waiters = new Map<number, Set<Waiter>>();

  constructor() {
    browser.debugger.onEvent.addListener((source, method, params) => {
      if (source.tabId === undefined) return;
      for (const w of [...(this.waiters.get(source.tabId) ?? [])]) w.onEvent(method, params);
    });
    browser.debugger.onDetach.addListener((source, reason) => {
      if (source.tabId === undefined) return;
      this.attached.delete(source.tabId);
      for (const w of [...(this.waiters.get(source.tabId) ?? [])]) w.onDetach(reason);
    });
  }

  /**
   * After a service worker restart the debugger sessions survive but this object does not: tabs
   * of our sessions that Chrome still reports as attached are taken to be ours.
   */
  async restore(tabIds: Iterable<number>): Promise<void> {
    const ours = new Set(tabIds);
    for (const t of await browser.debugger.getTargets()) {
      if (t.attached && t.tabId !== undefined && ours.has(t.tabId)) this.attached.add(t.tabId);
    }
  }

  attach(tabId: number): Promise<void> {
    if (this.attached.has(tabId)) return Promise.resolve();
    let p = this.attaching.get(tabId);
    if (!p) {
      p = this.doAttach(tabId).finally(() => this.attaching.delete(tabId));
      this.attaching.set(tabId, p);
    }
    return p;
  }

  private async doAttach(tabId: number): Promise<void> {
    try {
      await browser.debugger.attach({ tabId }, '1.3');
    } catch (e) {
      throw attachError(e);
    }
    this.attached.add(tabId);
    await this.raw(tabId, 'Page.enable');
    await this.raw(tabId, 'Runtime.enable');
    // Makes a background tab take real input as if it had focus (spike result in spec §13).
    await this.raw(tabId, 'Emulation.setFocusEmulationEnabled', { enabled: true });
  }

  async detach(tabId: number): Promise<void> {
    this.attached.delete(tabId);
    await browser.debugger.detach({ tabId }).catch(() => {});
  }

  /** Sends a command, attaching first. A session that vanished behind our back is re-attached once. */
  async send<T = any>(tabId: number, method: string, params?: Record<string, unknown>): Promise<T> {
    await this.attach(tabId);
    try {
      return await this.raw<T>(tabId, method, params);
    } catch (e) {
      if (!/not attached/i.test(message(e))) throw commandError(method, e);
    }
    this.attached.delete(tabId);
    await this.attach(tabId);
    try {
      return await this.raw<T>(tabId, method, params);
    } catch (e) {
      throw commandError(method, e);
    }
  }

  /** Starts listening before the command that causes the event, so a fast event is not missed. */
  waitForEvent<T = any>(tabId: number, match: (method: string, params: any) => boolean, deadline: number): EventWait<T> {
    let finish = () => {};
    const done = new Promise<T>((resolve, reject) => {
      const waiter: Waiter = {
        onEvent: (method, params) => {
          if (!match(method, params)) return;
          finish();
          resolve(params as T);
        },
        onDetach: (reason) => {
          finish();
          reject(
            reason === 'canceled_by_user'
              ? new BridgeError('DETACHED_BY_USER', 'the user pressed Cancel on the debugging bar', 'Ask the user before continuing')
              : new BridgeError('TAB_NOT_FOUND', 'the tab was closed'),
          );
        },
      };
      const timer = setTimeout(() => {
        finish();
        reject(new BridgeError('TIMEOUT', 'the page did not finish loading before timeoutMs', 'Retry with a larger timeoutMs'));
      }, Math.max(0, deadline - Date.now()));
      finish = () => {
        clearTimeout(timer);
        this.waiters.get(tabId)?.delete(waiter);
      };
      if (!this.waiters.has(tabId)) this.waiters.set(tabId, new Set());
      this.waiters.get(tabId)!.add(waiter);
    });
    return { done, cancel: () => finish() };
  }

  private async raw<T>(tabId: number, method: string, params?: Record<string, unknown>): Promise<T> {
    return (await browser.debugger.sendCommand({ tabId }, method, params)) as T;
  }
}

function attachError(e: unknown): BridgeError {
  const m = message(e);
  if (/no tab with/i.test(m)) return new BridgeError('TAB_NOT_FOUND', 'the tab no longer exists');
  if (/another debugger/i.test(m)) return new BridgeError('CDP_ERROR', m, 'Close DevTools on that tab: only one debugger can attach');
  return new BridgeError('RESTRICTED_URL', `this tab cannot be controlled: ${m}`, 'Browser pages such as chrome:// cannot be automated');
}

function commandError(method: string, e: unknown): BridgeError {
  const m = message(e);
  if (/no tab with/i.test(m)) return new BridgeError('TAB_NOT_FOUND', 'the tab no longer exists');
  return new BridgeError('CDP_ERROR', `${method}: ${m}`);
}
```

- [ ] **Step 7: Viết `actions/tabs.ts`**

`extension/src/background/actions/tabs.ts`:

```ts
import { browser } from 'wxt/browser';
import type {
  CloseSessionResult,
  CloseTabResult,
  FindTabArgs,
  FindTabResult,
  ListTabsResult,
  NavigateArgs,
  PageResult,
  TabResult,
} from '../../generated/protocol';
import type { Cdp, EventWait } from '../cdp';
import { BridgeError, blockedHost, noCurrentTab } from '../errors';
import { hostMatches, httpHost, isBlocked, isRestricted, queryHost } from '../hosts';
import type { Ctx, Handler } from '../router';

export interface TabCtx extends Ctx {
  cdp: Cdp;
}

async function currentTab(ctx: TabCtx): Promise<number> {
  const s = await ctx.sessions.get(ctx.session);
  if (s.currentTabId === null) throw noCurrentTab(ctx.session);
  return s.currentTabId;
}

/** Resolves when the main frame finished loading. A back/forward-cache restore fires no load event. */
function waitForLoad(ctx: TabCtx, tabId: number): EventWait<unknown> {
  return ctx.cdp.waitForEvent(
    tabId,
    (method, p) =>
      method === 'Page.loadEventFired' ||
      (method === 'Page.frameNavigated' && !p.frame.parentId && p.type === 'BackForwardCacheRestore'),
    ctx.deadline,
  );
}

function waitForSameDocument(ctx: TabCtx, tabId: number): EventWait<unknown> {
  return ctx.cdp.waitForEvent(tabId, (method) => method === 'Page.navigatedWithinDocument', ctx.deadline);
}

/** URL and title once the page settled; a page that ended up on a blocked host reveals neither. */
async function settled(ctx: TabCtx, tabId: number): Promise<PageResult> {
  const tab = await browser.tabs.get(tabId);
  if (isBlocked(tab.url, ctx.blockedHosts())) throw blockedHost();
  return { url: tab.url ?? '', title: tab.title ?? '' };
}

/** Runs a command that starts a navigation, then waits for it unless the command failed. */
async function navigateWith(wait: EventWait<unknown>, start: () => Promise<unknown>): Promise<void> {
  try {
    await start();
  } catch (e) {
    wait.cancel();
    throw e;
  }
  await wait.done;
}

async function openSessionTab(ctx: TabCtx, groupTitle: string | undefined): Promise<number> {
  const tab = await browser.tabs.create({ url: 'about:blank', active: false });
  const tabId = tab.id!;
  const s = await ctx.sessions.get(ctx.session);
  let groupId: number | null = null;
  if (s.groupId !== null) {
    groupId = await browser.tabs.group({ groupId: s.groupId, tabIds: [tabId] }).catch(() => null); // the user removed the group
  }
  if (groupId === null) {
    groupId = await browser.tabs.group({ tabIds: [tabId] });
    await browser.tabGroups.update(groupId, { title: groupTitle || ctx.session });
  }
  await ctx.sessions.update(ctx.session, (st) => {
    st.groupId = groupId;
    st.tabIds.push(tabId);
    st.currentTabId = tabId;
    st.stopped = false;
  });
  return tabId;
}

const navigate: Handler<TabCtx> = async (ctx, args: NavigateArgs): Promise<TabResult> => {
  const s = await ctx.sessions.get(ctx.session);
  const fresh = s.currentTabId === null || args.newTab === true;
  const tabId = fresh ? await openSessionTab(ctx, args.groupTitle) : s.currentTabId!;
  if (!fresh) await ctx.sessions.update(ctx.session, (st) => void (st.stopped = false));

  const wait = waitForLoad(ctx, tabId);
  let nav: { errorText?: string; loaderId?: string };
  try {
    nav = await ctx.cdp.send(tabId, 'Page.navigate', { url: args.url });
  } catch (e) {
    wait.cancel();
    throw e;
  }
  if (nav.errorText) {
    wait.cancel();
    throw new BridgeError('NAVIGATION_FAILED', `${args.url}: ${nav.errorText}`, 'Check the URL and that the site is reachable');
  }
  // A same-document navigation (only the #fragment changed) has no loader and fires no load event.
  if (nav.loaderId) await wait.done;
  else wait.cancel();
  // The about:blank the tab was created with would otherwise be a history entry go_back can reach.
  if (fresh) await ctx.cdp.send(tabId, 'Page.resetNavigationHistory');
  return { tabId, ...(await settled(ctx, tabId)) };
};

const findTab: Handler<TabCtx> = async (ctx, args: FindTabArgs): Promise<FindTabResult> => {
  const blocked = ctx.blockedHosts();
  const wanted = args.url ? queryHost(args.url) : null;
  if (args.url && !wanted) throw new BridgeError('INVALID_REQUEST', `cannot read a host from ${args.url}`);

  if (args.active) {
    const [tab] = await browser.tabs.query({ active: true, lastFocusedWindow: true });
    if (!tab?.id) throw new BridgeError('TAB_NOT_FOUND', 'there is no active tab');
    const host = httpHost(tab.url);
    if (wanted && !(host && hostMatches(host, wanted))) {
      throw new BridgeError('TAB_NOT_FOUND', `the tab the user is looking at is not on ${wanted}`, 'Ask the user to switch to that tab, or use navigate');
    }
    if (isRestricted(tab.url)) throw new BridgeError('RESTRICTED_URL', 'the tab the user is looking at is a browser page that cannot be automated');
    if (isBlocked(tab.url, blocked)) throw blockedHost();
    const id = tab.id;
    const st = await ctx.sessions.update(ctx.session, (st) => {
      if (!st.tabIds.includes(id) && !st.borrowedTabIds.includes(id)) st.borrowedTabIds.push(id);
      st.currentTabId = id;
      st.stopped = false;
    });
    return { tabId: id, url: tab.url ?? '', title: tab.title ?? '', borrowed: st.borrowedTabIds.includes(id) };
  }

  const s = await ctx.sessions.get(ctx.session);
  for (const id of [...s.tabIds, ...s.borrowedTabIds]) {
    const tab = await browser.tabs.get(id).catch(() => null);
    const host = httpHost(tab?.url);
    if (!tab || !host || !hostMatches(host, wanted!) || isBlocked(tab.url, blocked)) continue;
    await ctx.sessions.update(ctx.session, (st) => {
      st.currentTabId = id;
      st.stopped = false;
    });
    return { tabId: id, url: tab.url ?? '', title: tab.title ?? '', borrowed: s.borrowedTabIds.includes(id) };
  }
  throw new BridgeError(
    'TAB_NOT_FOUND',
    `no tab of session ${ctx.session} is on ${wanted}`,
    'Use navigate to open it, or find_tab with active:true to borrow the tab the user is looking at',
  );
};

const listTabs: Handler<TabCtx> = async (ctx): Promise<ListTabsResult> => {
  const s = await ctx.sessions.get(ctx.session);
  const tabs: ListTabsResult['tabs'] = [];
  for (const id of [...s.tabIds, ...s.borrowedTabIds]) {
    const tab = await browser.tabs.get(id).catch(() => null);
    if (!tab) continue;
    tabs.push({ tabId: id, url: tab.url ?? '', title: tab.title ?? '', current: id === s.currentTabId, borrowed: s.borrowedTabIds.includes(id) });
  }
  return { tabs };
};

const closeTab: Handler<TabCtx> = async (ctx): Promise<CloseTabResult> => {
  const tabId = await currentTab(ctx);
  const s = await ctx.sessions.get(ctx.session);
  if (s.borrowedTabIds.includes(tabId)) {
    // A borrowed tab belongs to the user: give it back, never close it.
    await ctx.cdp.detach(tabId);
    await ctx.sessions.update(ctx.session, (st) => {
      st.borrowedTabIds = st.borrowedTabIds.filter((t) => t !== tabId);
      st.currentTabId = null;
    });
    return { closed: false, released: true };
  }
  await browser.tabs.remove(tabId).catch(() => {}); // already closed by the user is fine
  await ctx.sessions.update(ctx.session, (st) => {
    st.tabIds = st.tabIds.filter((t) => t !== tabId);
    st.currentTabId = null;
  });
  return { closed: true, released: false };
};

const closeSession: Handler<TabCtx> = async (ctx): Promise<CloseSessionResult> => {
  const s = await ctx.sessions.get(ctx.session);
  let closed = 0;
  for (const id of s.tabIds) {
    try {
      await browser.tabs.remove(id);
      closed++;
    } catch {
      // closed by the user already
    }
  }
  for (const id of s.borrowedTabIds) await ctx.cdp.detach(id);
  await ctx.sessions.remove(ctx.session); // Chrome removes the group once its last tab is gone
  return { closed };
};

const stripHash = (url: string) => url.split('#')[0];

function historyStep(delta: -1 | 1): Handler<TabCtx> {
  return async (ctx): Promise<PageResult> => {
    const tabId = await currentTab(ctx);
    const h = await ctx.cdp.send<{ currentIndex: number; entries: { id: number; url: string }[] }>(tabId, 'Page.getNavigationHistory');
    const target = h.entries[h.currentIndex + delta];
    if (!target) {
      throw new BridgeError('NAVIGATION_FAILED', delta < 0 ? 'there is no page to go back to' : 'there is no page to go forward to');
    }
    const sameDocument = stripHash(target.url) === stripHash(h.entries[h.currentIndex]?.url ?? '');
    const wait = sameDocument ? waitForSameDocument(ctx, tabId) : waitForLoad(ctx, tabId);
    await navigateWith(wait, () => ctx.cdp.send(tabId, 'Page.navigateToHistoryEntry', { entryId: target.id }));
    return settled(ctx, tabId);
  };
}

const reload: Handler<TabCtx> = async (ctx): Promise<PageResult> => {
  const tabId = await currentTab(ctx);
  await navigateWith(waitForLoad(ctx, tabId), () => ctx.cdp.send(tabId, 'Page.reload'));
  return settled(ctx, tabId);
};

export const tabHandlers = {
  navigate,
  find_tab: findTab,
  list_tabs: listTabs,
  close_tab: closeTab,
  close_session: closeSession,
  go_back: historyStep(-1),
  go_forward: historyStep(1),
  reload,
};
```

- [ ] **Step 8: Nối dây service worker**

Thay toàn bộ `extension/src/entrypoints/background.ts`:

```ts
import { browser } from 'wxt/browser';
import { defineBackground } from 'wxt/utils/define-background';
import { tabHandlers, type TabCtx } from '../background/actions/tabs';
import { Cdp } from '../background/cdp';
import { Connection } from '../background/connection';
import { CommandLog } from '../background/log';
import { createRouter } from '../background/router';
import { SessionStore } from '../background/sessions';
import type { Hello } from '../generated/protocol';
import { DEFAULT_DAEMON_URL, KEYS } from '../shared/state';

// Typed as the schema's literal: when the daemon bumps ProtocolVersion, this line stops compiling.
const PROTOCOL_VERSION: Hello['protocolVersion'] = 1;

export default defineBackground(() => {
  const sessions = new SessionStore(browser.storage.session);
  const log = new CommandLog(browser.storage.session);
  const cdp = new Cdp();
  let blockedHosts: string[] = [];

  const route = createRouter<TabCtx>({
    sessions,
    handlers: tabHandlers,
    blockedHosts: () => blockedHosts,
    tabUrl: (tabId) => browser.tabs.get(tabId).then((t) => t.url, () => undefined),
    makeCtx: (session, deadline) => ({ session, deadline, sessions, cdp, blockedHosts: () => blockedHosts }),
  });

  const connection = new Connection({
    url: async () => ((await browser.storage.local.get(KEYS.daemonUrl))[KEYS.daemonUrl] as string | undefined) || DEFAULT_DAEMON_URL,
    hello: () => ({
      type: 'hello',
      protocolVersion: PROTOCOL_VERSION,
      extensionVersion: browser.runtime.getManifest().version,
      extensionId: browser.runtime.id,
      browser: navigator.userAgent.includes('Edg/') ? 'edge' : 'chrome',
    }),
    onWelcome: (w) => {
      blockedHosts = w.blockedHosts ?? [];
    },
    onStatus: (s) => void browser.storage.session.set({ [KEYS.connection]: s }),
    onRequest: async (frame) => {
      const start = Date.now();
      const resp = await route(frame);
      const selector = (frame.args as { selector?: unknown } | undefined)?.selector;
      void log.add({
        at: start,
        session: frame.session,
        action: frame.action,
        selector: typeof selector === 'string' ? selector : undefined,
        ms: Date.now() - start,
        code: resp.error?.code,
      });
      return resp;
    },
  });

  browser.tabs.onRemoved.addListener((tabId) => void sessions.forgetTab(tabId));
  browser.debugger.onDetach.addListener((source, reason) => {
    if (source.tabId === undefined) return;
    if (reason === 'canceled_by_user') void sessions.stopSessionsOf(source.tabId);
    else void sessions.forgetTab(source.tabId);
  });
  browser.storage.onChanged.addListener((changes, area) => {
    if (area === 'local' && KEYS.daemonUrl in changes) connection.reconnect();
  });
  // Wakes the worker to retry while the daemon is down; 30s is the shortest period Chrome allows.
  void browser.alarms.create('reconnect', { periodInMinutes: 0.5 });
  browser.alarms.onAlarm.addListener((alarm) => {
    if (alarm.name === 'reconnect') void connection.connect();
  });
  void browser.sidePanel.setPanelBehavior({ openPanelOnActionClick: true });

  void sessions.all().then((all) => cdp.restore(Object.values(all).flatMap((s) => [...s.tabIds, ...s.borrowedTabIds])));
  void connection.connect();
});
```

- [ ] **Step 9: Chạy typecheck, unit test và E2E**

```bash
npm --prefix extension run typecheck
npm --prefix extension test
npm --prefix e2e run typecheck
npm --prefix e2e test
```

Expected: `tsc` sạch ở cả hai package; 29 unit test pass; E2E `15 passed` (14 test tab và 1 test keepalive). Test keepalive tốn khoảng 70 giây.

- [ ] **Step 10: Chạy E2E thêm hai lần để chắc không flaky**

Run: `npm --prefix e2e test -- tabs` hai lần.
Expected: `14 passed` cả hai lần.

- [ ] **Step 11: Commit**

```bash
git add testpage e2e extension/src
git commit -m "feat(extension): drive tabs through CDP and test them end to end against the daemon"
```

---

### Task 7: Side panel

**Files:**
- Create: `extension/src/entrypoints/sidepanel/index.html`
- Create: `extension/src/entrypoints/sidepanel/main.tsx`
- Create: `extension/src/entrypoints/sidepanel/App.tsx`
- Test: `e2e/tests/sidepanel.spec.ts`

**Interfaces:**
- Consumes: `KEYS`, `DEFAULT_DAEMON_URL`, `ConnectionStatus`, `Sessions`, `LogEntry` (Task 2); state mà service worker ghi ở Task 6.
- Produces: `sidepanel.html` gồm 4 phần của §8.7: trạng thái kết nối (`data-testid="connection-state"`), session và tab, 50 lệnh gần nhất, ô sửa địa chỉ daemon. Bấm icon extension mở side panel (`setPanelBehavior` ở Task 6). Lưu địa chỉ mới thì service worker kết nối lại ngay.

- [ ] **Step 1: Viết test fail**

`e2e/tests/sidepanel.spec.ts`:

```ts
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
```

- [ ] **Step 2: Chạy test, xác nhận fail**

Run: `npm --prefix e2e test -- sidepanel`
Expected: FAIL, `page.goto: net::ERR_FILE_NOT_FOUND` hoặc không tìm thấy `connection-state`

- [ ] **Step 3: Viết side panel**

`extension/src/entrypoints/sidepanel/index.html`:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>Browser Bridge</title>
    <style>
      body { font: 13px system-ui, sans-serif; margin: 0; padding: 12px; color: #1f2328; }
      h2 { font-size: 13px; margin: 16px 0 6px; text-transform: uppercase; letter-spacing: 0.04em; color: #59636e; }
      .state { font-weight: 600; }
      .state.connected { color: #1a7f37; }
      .state.disconnected { color: #cf222e; }
      .error { color: #cf222e; }
      .muted { color: #59636e; }
      ul { margin: 4px 0; padding-left: 18px; }
      table { border-collapse: collapse; width: 100%; }
      td { padding: 2px 4px; border-bottom: 1px solid #eaeef2; vertical-align: top; word-break: break-all; }
      form { display: flex; gap: 6px; }
      input { flex: 1; font: inherit; padding: 4px; }
      @media (prefers-color-scheme: dark) {
        body { background: #0d1117; color: #e6edf3; }
        td { border-color: #30363d; }
        .muted, h2 { color: #9198a1; }
      }
    </style>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="./main.tsx"></script>
  </body>
</html>
```

`extension/src/entrypoints/sidepanel/main.tsx`:

```tsx
import { createRoot } from 'react-dom/client';
import { App } from './App';

createRoot(document.getElementById('root')!).render(<App />);
```

`extension/src/entrypoints/sidepanel/App.tsx`:

```tsx
import { useEffect, useState, type FormEvent } from 'react';
import { browser } from 'wxt/browser';
import { DEFAULT_DAEMON_URL, KEYS, type ConnectionStatus, type LogEntry, type Sessions } from '../../shared/state';

/** A storage value that re-renders whenever the service worker changes it. */
function useStored<T>(area: 'session' | 'local', key: string): T | undefined {
  const [value, setValue] = useState<T>();
  useEffect(() => {
    void browser.storage[area].get(key).then((r) => setValue(r[key] as T));
    const onChanged = (changes: Record<string, { newValue?: unknown }>, changedArea: string) => {
      if (changedArea === area && key in changes) setValue(changes[key]?.newValue as T);
    };
    browser.storage.onChanged.addListener(onChanged);
    return () => browser.storage.onChanged.removeListener(onChanged);
  }, [area, key]);
  return value;
}

export function App() {
  const conn = useStored<ConnectionStatus>('session', KEYS.connection);
  const sessions = useStored<Sessions>('session', KEYS.sessions) ?? {};
  const log = useStored<LogEntry[]>('session', KEYS.log) ?? [];
  const savedUrl = useStored<string>('local', KEYS.daemonUrl);
  const [draft, setDraft] = useState<string | null>(null);
  const url = draft ?? savedUrl ?? DEFAULT_DAEMON_URL;

  const save = (e: FormEvent) => {
    e.preventDefault();
    void browser.storage.local.set({ [KEYS.daemonUrl]: url.trim() });
    setDraft(null);
  };

  const state = conn?.state ?? 'disconnected';
  return (
    <main>
      <h2>Connection</h2>
      <div>
        <span className={`state ${state}`} data-testid="connection-state">{state}</span> <span className="muted">{conn?.url}</span>
      </div>
      <div className="muted">
        daemon {conn?.daemonVersion ?? '?'} · extension {browser.runtime.getManifest().version}
        {conn?.blockedHosts?.length ? ` · ${conn.blockedHosts.length} blocked hosts` : ''}
      </div>
      {conn?.error && <div className="error">{conn.error}</div>}

      <h2>Sessions</h2>
      {Object.keys(sessions).length === 0 && <div className="muted">none</div>}
      {Object.entries(sessions).map(([name, s]) => (
        <div key={name}>
          <strong>{name}</strong>
          {s.stopped && <span className="error"> stopped by Cancel</span>}
          <ul>
            {[...s.tabIds, ...s.borrowedTabIds].map((id) => (
              <li key={id}>
                tab {id}
                {id === s.currentTabId && ' · current'}
                {s.borrowedTabIds.includes(id) && ' · borrowed'}
              </li>
            ))}
          </ul>
        </div>
      ))}

      <h2>Last commands</h2>
      <table>
        <tbody>
          {log.map((e, i) => (
            <tr key={i}>
              <td className="muted">{new Date(e.at).toLocaleTimeString()}</td>
              <td>{e.session}</td>
              <td>{e.action}</td>
              <td>{e.selector}</td>
              <td className="muted">{e.ms} ms</td>
              <td className={e.code ? 'error' : ''}>{e.code ?? 'ok'}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2>Daemon address</h2>
      <form onSubmit={save}>
        <input value={url} onChange={(e) => setDraft(e.target.value)} spellCheck={false} />
        <button type="submit">Save</button>
      </form>
    </main>
  );
}
```

- [ ] **Step 4: Chạy test, xác nhận pass**

```bash
npm --prefix extension run typecheck
npm --prefix e2e test -- sidepanel
```

Expected: `tsc` sạch, `1 passed`.

- [ ] **Step 5: Commit**

```bash
git add extension/src/entrypoints/sidepanel e2e/tests/sidepanel.spec.ts
git commit -m "feat(extension): add side panel with connection, sessions, log and daemon address"
```

---

### Task 8: Cập nhật spec và kiểm tra toàn bộ

**Files:**
- Modify: `docs/superpowers/specs/2026-10-06-browser-bridge-design.md` (§3, §12)

**Interfaces:**
- Produces: spec mô tả đúng cấu trúc WXT và cách E2E cô lập khỏi daemon thật.

- [ ] **Step 1: Sửa cây thư mục ở §3**

Thay khối `extension/` trong cây thư mục:

```text
├── extension/                   WXT + TypeScript
│   └── src/
│       ├── background/          service worker: connection, router, sessions, cdp, actions/
│       ├── page-agent/          bundle IIFE inject vào trang
│       ├── sidepanel/           React
│       └── generated/           type TS sinh từ schema/
```

bằng:

```text
├── extension/                   WXT + TypeScript
│   └── src/
│       ├── entrypoints/         điểm vào của WXT: background.ts, sidepanel/ (React)
│       ├── background/          logic của service worker: connection, router, sessions, cdp, actions/
│       ├── page-agent/          bundle IIFE inject vào trang
│       ├── shared/              type và key storage dùng chung giữa service worker và side panel
│       └── generated/           type TS sinh từ schema/
```

- [ ] **Step 2: Ghi cách E2E cô lập ở §12**

Trong mục **E2E (Playwright, TypeScript)**, thêm một gạch đầu dòng ngay sau dòng "Load extension bằng `launchPersistentContext`…":

```markdown
  - Daemon của E2E chạy ở cổng 19876, và extension được build bằng `--mode e2e` với địa chỉ đó compile sẵn (`WXT_DAEMON_URL`), nên một lần chạy test không bao giờ nối vào daemon thật ở 9876.
```

- [ ] **Step 3: Chạy toàn bộ kiểm tra**

```bash
go -C daemon vet ./...
go -C daemon test ./...
npm --prefix extension run check:gen
npm --prefix extension run typecheck
npm --prefix extension test
npm --prefix e2e run typecheck
npm --prefix e2e test
```

Expected: Go không lỗi và mọi package `ok`; `protocol.ts is up to date`; `tsc` sạch; 29 unit test pass; E2E `16 passed`.

- [ ] **Step 4: Thử tay trên Chrome thật**

`go -C daemon build -o bridge.exe ./cmd/bridge`, chạy `daemon\bridge.exe serve`, rồi `npm --prefix extension run build`. Vào `chrome://extensions`, Load unpacked thư mục `extension/.output/chrome-mv3`. Bấm icon: side panel hiện `connected`. Ở terminal khác:

```powershell
curl.exe -s -X POST http://127.0.0.1:9876/command -H "Content-Type: application/json" -d '{"action":"navigate","args":{"url":"https://example.com"},"session":"manual"}'
```

Expected: tab `example.com` mở ở nền trong group "manual", thanh vàng debug hiện trên tab đó. JSON trả về có `"title":"Example Domain"`. Bấm Cancel trên thanh vàng rồi gọi `reload`: nhận `DETACHED_BY_USER`. Gọi `close_session`: tab và group biến mất.

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/2026-10-06-browser-bridge-design.md
git commit -m "docs: describe the WXT layout and the isolated E2E daemon in the spec"
```
