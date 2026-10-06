# browser-bridge

[![Release](https://img.shields.io/github/v/release/phantranthelinh/browser-bridge)](https://github.com/phantranthelinh/browser-bridge/releases/latest)
[![Release workflow](https://github.com/phantranthelinh/browser-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/phantranthelinh/browser-bridge/actions/workflows/release.yml)
![Platform: Windows](https://img.shields.io/badge/platform-Windows-blue)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Let any AI agent drive **your real Chrome or Edge**, including the sites you are already signed in to, through a small local HTTP API. Works with Claude Code, Codex, or a harness running a local Ollama model. No third-party account needed.

```text
Agent ──HTTP 127.0.0.1:9876──▶ bridge daemon (Go) ──WebSocket──▶ MV3 extension (TypeScript, WXT) ──CDP──▶ tab
```

## Table of contents

- [Features](#features)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
- [HTTP API](#http-api)
- [Configuration](#configuration)
- [Security](#security)
- [Troubleshooting](#troubleshooting)
- [Development](#development)
- [Releasing](#releasing)
- [Project layout](#project-layout)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Your real browser.** Agents use your existing Chrome or Edge profile, cookies and logins.
- **Plain HTTP API.** Agents only need `POST /command`. No Chrome API or CDP knowledge required.
- **Sessions.** Each agent session gets its own tab group, so parallel agents do not collide.
- **Schema-validated requests** with timeouts and stable error codes.
- **One-line install** that downloads the daemon and extension, adds `bridge` to `PATH` and starts it.
- **Side panel** in the extension showing the connection, sessions and recent commands.
- **Kill switch.** Click **Cancel** on Chrome's yellow "debugging" bar to stop every command on that tab immediately.

## How it works

| Component | Location | Role |
|---|---|---|
| Daemon | `daemon/` (binary `bridge.exe`) | Validates requests against the JSON Schema, routes them by session, enforces timeouts and returns standard error codes. Never touches CDP or the DOM. |
| Extension | `extension/` | Connects to the daemon, manages sessions and tab groups, and operates tabs through `chrome.debugger`. |
| Agent | anything | Talks to the daemon over HTTP. |

## Requirements

- Windows 10 or 11 (the only supported platform)
- Google Chrome or Microsoft Edge
- Windows PowerShell 5.1 or PowerShell 7

## Installation

**1. Run this in PowerShell** (no admin rights needed):

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex
```

This downloads `bridge.exe` and the extension into `%USERPROFILE%\.browser-bridge\`, adds `bridge` to your user `PATH`, and starts the daemon.

**2. Load the extension** (once):

1. Open `chrome://extensions` (Edge: `edge://extensions`).
2. Turn on **Developer mode**.
3. Click **Load unpacked** and choose `%USERPROFILE%\.browser-bridge\extension`. The installer has already copied this path to your clipboard.

The installer waits up to 3 minutes and reports when the extension connects. The extension folder stays the same across updates, so you never need to load it again.

> [!NOTE]
> `bridge.exe` is not code-signed yet. On first run, Windows Defender may take a few seconds to scan it, and Defender or SmartScreen may show a false-positive warning.

### Installer options

| Option | How |
|---|---|
| Pin a version | Set `$env:BRIDGE_VERSION = '0.1.0'` before running the install command |
| Show all options | `iex "& { $(irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1) } -Help"` |
| Uninstall | `iex "& { $(irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1) } -Uninstall"`, then **Remove** the extension in `chrome://extensions` |

> [!IMPORTANT]
> `irm … | iex` runs a script downloaded from GitHub with your permissions. Read [`install/install.ps1`](install/install.ps1) first if you prefer.

## Quick start

Check that the daemon is running and the extension is connected:

```powershell
bridge status
```

Open a page in a new session:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:9876/command -ContentType 'application/json' `
  -Body '{"action":"navigate","args":{"url":"https://example.com"},"session":"demo"}'
```

A background tab with `example.com` opens in a tab group named "demo", with Chrome's yellow "debugging" bar. Click the Browser Bridge icon to open the side panel.

Close the session and its tabs when you are done:

```powershell
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:9876/command -ContentType 'application/json' `
  -Body '{"action":"close_session","args":{},"session":"demo"}'
```

## Usage

### CLI

| Task | Command |
|---|---|
| Start the daemon (for example after a reboot) | `bridge start` |
| Show daemon and extension status | `bridge status` |
| Stop or restart the daemon | `bridge stop`, `bridge restart` |
| Run the daemon in the foreground | `bridge serve` |
| Print the version | `bridge version` |
| Update | Run the install command again, then click **Reload** on Browser Bridge in `chrome://extensions` |

### Actions

| Action | Description |
|---|---|
| `navigate` | Open a URL in the session's current tab, or in a new background tab (`newTab: true`). Waits for the load event. |
| `find_tab` | Make an existing tab the session's current tab: search the session's tabs by host, or borrow the tab you are looking at (`active: true`) |
| `list_tabs` | List the session's tabs |
| `close_tab` | Close the current tab. A borrowed tab is only released, never closed. |
| `close_session` | Close every tab of the session, release borrowed tabs and remove the tab group |
| `go_back`, `go_forward` | Move through the current tab's history |
| `reload` | Reload the current tab |

`GET /tools` returns each action with its argument schema.

### Roadmap

`GET /tools` also lists actions that are defined in the protocol but not implemented by the extension yet. Calling one returns `INTERNAL: <action> is not implemented in this extension version`. They are:

- Page reading and interaction: `snapshot`, `click`, `fill`, `select`, `press_key`, `scroll`, `upload`, `wait_for`, `handle_dialog`
- Capture and inspection: `screenshot`, `network_start`, `network_requests`, `network_request_detail`, `network_stop`
- Low level: `evaluate`, `cdp`

Also planned: `bridge call`, `bridge mcp` and a `SKILL.md` for agents. See the [design spec](docs/superpowers/specs/2026-10-06-browser-bridge-design.md) for the full plan.

## HTTP API

The daemon listens on `127.0.0.1:9876`.

| Method | Path | Description |
|---|---|---|
| `POST` | `/command` | Run an action |
| `GET` | `/tools` | List actions and their JSON Schemas |
| `GET` | `/status` | Daemon and extension status |
| `POST` | `/shutdown` | Stop the daemon |
| `GET` | `/ws` | WebSocket endpoint, for the extension only |

### Request

```json
{
  "action": "navigate",
  "args": { "url": "https://example.com" },
  "session": "demo",
  "timeoutMs": 30000
}
```

`timeoutMs` is optional.

### Response

Every endpoint answers with the same envelope:

```json
{ "ok": true, "data": { "tabId": 805969099, "url": "https://example.com/", "title": "Example Domain" } }
```

```json
{ "ok": false, "error": { "code": "TAB_NOT_FOUND", "message": "…", "hint": "…" } }
```

The HTTP status is `400` for `INVALID_REQUEST` and `UNKNOWN_ACTION`, `403` for `FORBIDDEN`, and `200` for everything else, including `ok: false` results from the extension. The full list of error codes is in [`schema/protocol.schema.json`](schema/protocol.schema.json).

## Configuration

Config, PID, logs and artifacts live in `%USERPROFILE%\.browser-bridge`. Set the `BRIDGE_HOME` environment variable to use another folder.

`config.json`:

| Key | Default | Description |
|---|---|---|
| `addr` | `127.0.0.1:9876` | Listen address. Must be a loopback address. |
| `blockedHosts` | `[]` | Hosts the bridge refuses to open, for example `["bank.com"]` |
| `extensionIds` | the published extension's ID | Extension IDs allowed to connect to `/ws` |

Run `bridge restart` after you edit the file.

## Security

- **Any program running as your user can control your browser through the bridge.** The daemon has no auth token. This is an accepted risk (design spec §7).
- The daemon binds only to `127.0.0.1`.
- `/ws` accepts only `Origin` values belonging to an extension listed in `extensionIds`.
- HTTP requests with an `Origin` header are rejected, so web pages cannot `fetch` the daemon.
- Page content can try to steer the agent into doing something else (prompt injection). Block sensitive sites with `blockedHosts`.

## Troubleshooting

**The extension logs `WebSocket connection to 'ws://127.0.0.1:9876/ws' failed: net::ERR_CONNECTION_REFUSED`**

The daemon is not running. Run `bridge status`. If the command is not found, install browser-bridge first. Otherwise run `bridge start`. The daemon does not start automatically after a reboot.

**`bridge` is not recognized as a command**

The installer added it to your user `PATH`. Open a new terminal window.

**The daemon is running but the extension is not connected**

Make sure the extension is loaded from `%USERPROFILE%\.browser-bridge\extension` (or from your own build). After an update, click **Reload** on Browser Bridge in `chrome://extensions`. The extension ID is pinned by `extension/manifest-key.txt`; if you build with a different key, add the new ID to `extensionIds`.

## Development

Requirements: Go (version in [`daemon/go.mod`](daemon/go.mod)), Node.js 22, Chrome or Edge.

| Task | Command |
|---|---|
| Test the daemon | `go -C daemon test ./...` |
| Run the daemon from source | `go -C daemon run ./cmd/bridge serve` |
| Build the extension (to `extension/.output/chrome-mv3`) | `npm --prefix extension install`, then `npm --prefix extension run build` |
| Run the extension with hot reload | `npm --prefix extension run dev` |
| Test and typecheck the extension | `npm --prefix extension test`, `npm --prefix extension run typecheck` |
| Regenerate the schema and TS types | `go -C daemon run ./cmd/schemagen`, then `npm --prefix extension run gen` |
| Check generated files are up to date | `npm --prefix extension run check:gen` |
| End-to-end tests (Playwright, separate daemon on port 19876) | `npm --prefix e2e install`, then `npm --prefix e2e test` |
| Build the release files | `./install/build-dist.ps1 -Out dist` |
| Test the installer without touching your real install | `./install/test-install.ps1 -Artifacts dist -Shell powershell` (and `-Shell pwsh`) |

Actions are defined **once**, as Go structs in `daemon/internal/protocol`. Both `schema/protocol.schema.json` and `extension/src/generated/protocol.ts` are generated from them. Do not edit generated files by hand.

## Releasing

1. Set the same version in `daemon/internal/protocol/protocol.go` (`Version`) and `extension/package.json` (`version`), and commit.
2. Tag and push: `git tag v0.2.0 && git push origin v0.2.0`.

The [`release.yml`](.github/workflows/release.yml) workflow checks the versions, runs every test, builds the release, tries the installer on Windows PowerShell 5.1 and PowerShell 7, publishes the GitHub Release, and then installs from that release as a final check.

## Project layout

```text
daemon/       Go: bridge binary (cmd/bridge), schema generator (cmd/schemagen),
              internal/{protocol,server,session,home,fakeext}
schema/       JSON Schema generated from Go (committed)
extension/    WXT + React: src/entrypoints, src/background, src/shared, src/generated
e2e/          Playwright tests running the real extension against a separate daemon
testpage/     Static HTML pages for tests
install/      install.ps1, build-dist.ps1, check-version.ps1, test-install.ps1
docs/         Design specs and implementation plans
```

## Documentation

- [Design spec](docs/superpowers/specs/2026-10-06-browser-bridge-design.md): architecture, API, actions, sessions and security
- [Installer and release design](docs/superpowers/specs/2026-10-06-installer-design.md): one-line install and tag-driven releases
- [Implementation plans](docs/superpowers/plans/)

## Contributing

Issues and pull requests are welcome. Before opening a pull request:

1. Run the daemon and extension tests, the typecheck and `check:gen` (see [Development](#development)).
2. If you change an action, edit the Go structs in `daemon/internal/protocol` and regenerate the schema and types.
3. Keep each commit focused, with a [Conventional Commits](https://www.conventionalcommits.org/) message (`feat:`, `fix:`, `docs:` …), as in the existing history.

## License

[MIT](LICENSE) © 2026 Phan Trần Thế Lĩnh
