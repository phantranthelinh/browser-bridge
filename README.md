# browser-bridge

[![Release](https://img.shields.io/github/v/release/phantranthelinh/browser-bridge)](https://github.com/phantranthelinh/browser-bridge/releases/latest)
[![Release workflow](https://github.com/phantranthelinh/browser-bridge/actions/workflows/release.yml/badge.svg)](https://github.com/phantranthelinh/browser-bridge/actions/workflows/release.yml)
![Platform: Windows](https://img.shields.io/badge/platform-Windows-blue)
[![License: MIT](https://img.shields.io/badge/license-MIT-green)](LICENSE)

Let any AI agent drive **your real Chrome or Edge**, including the sites you are already signed in to. Claude Code uses it through a skill and the `bridge call` command, Codex or a harness running a local Ollama model through MCP, and anything else through a small local HTTP API. No third-party account needed.

```text
Agent ──bridge call / MCP / HTTP 127.0.0.1:9876──▶ bridge daemon (Go) ──WebSocket──▶ MV3 extension (TypeScript, WXT) ──CDP──▶ tab
```

## Table of contents

- [Features](#features)
- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Installation](#installation)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Using it from an agent](#using-it-from-an-agent)
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
- **Ready for agents.** A skill for Claude Code and Codex, `bridge call` for any shell, `bridge mcp` for MCP clients, and a plain HTTP API. No Chrome API or CDP knowledge required.
- **Pages as text.** `snapshot` reads a page as an accessibility tree with refs such as `@e12` that actions take as their target.
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
| CLI and MCP server | `daemon/cmd/bridge` (`bridge call`, `bridge mcp`) | Turn a command line or an MCP tool call into one HTTP request to the daemon. |
| Skill | `daemon/skill/browser-bridge/SKILL.md` | Teaches Claude Code and Codex the workflow, the errors and the safety rules. |
| Agent | anything | Talks to the daemon through the CLI, MCP or HTTP. |

## Requirements

- Windows 10 or 11 (the only supported platform)
- Google Chrome or Microsoft Edge
- Windows PowerShell 5.1 or PowerShell 7

## Installation

**1. Run this in PowerShell** (no admin rights needed):

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex
```

This downloads `bridge.exe` and the extension into `%USERPROFILE%\.browser-bridge\`, adds `bridge` to your user `PATH`, adds the browser-bridge skill to Claude Code and Codex if they are installed, and starts the daemon.

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
| Skip the agent skill | Add `-NoSkill`, as in the uninstall command below |
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
bridge call navigate --session demo url=https://example.com
```

A background tab with `example.com` opens in a tab group named "demo", with Chrome's yellow "debugging" bar. Click the Browser Bridge icon to open the side panel.

Read the page the way an agent sees it:

```powershell
bridge call snapshot --session demo
```

Close the session and its tabs when you are done:

```powershell
bridge call close_session --session demo
```

Then ask your agent to use the browser: Claude Code and Codex already have the skill (see [Using it from an agent](#using-it-from-an-agent)).

## Usage

### CLI

| Task | Command |
|---|---|
| Start the daemon (for example after a reboot) | `bridge start` |
| Show daemon and extension status | `bridge status` |
| Run one action | `bridge call <action> --session <name> [key=value ...]` (`bridge call -h` for the details) |
| Read the daemon log | `bridge logs` (`-f` to follow, `-n 200` for more lines, `--prev` for the previous run) |
| Serve the actions to an MCP client | `bridge mcp` (stdio; started by the client, see below) |
| Add or remove the agent skill | `bridge install-skill`, `bridge install-skill --remove` |
| Stop or restart the daemon | `bridge stop`, `bridge restart` |
| Run the daemon in the foreground | `bridge serve` |
| Print the version | `bridge version` |
| Show the commands | `bridge help` |
| Update | Run the install command again, then click **Reload** on Browser Bridge in `chrome://extensions` |

### Actions

Every action works on the session's **current tab**: the tab `navigate` opened or `find_tab` picked.

**Tabs and navigation**

| Action | Description |
|---|---|
| `navigate` | Open a URL in the current tab, or in a new background tab (`newTab: true`). Waits for the load event. |
| `find_tab` | Make an existing tab the current tab: search the session's tabs by host, or borrow the tab you are looking at (`active: true`) |
| `activate_tab` | Bring the current tab to the front and focus its window. Chrome holds back some things, such as starting a video, until a tab has been visible. |
| `list_tabs` | List the session's tabs |
| `close_tab` | Close the current tab. A borrowed tab is only released, never closed. |
| `close_session` | Close every tab of the session, release borrowed tabs and remove the tab group |
| `go_back`, `go_forward` | Move through the current tab's history |
| `reload` | Reload the current tab |

**Reading and acting on the page**

| Action | Description |
|---|---|
| `snapshot` | Read the page as an accessibility tree. Interactive elements get refs such as `@e12`. |
| `screenshot` | Save the viewport, the full page (`fullPage: true`) or one element (`selector`) as PNG or JPEG. Returns the file path. |
| `click` | Click an element with a real mouse event. Fails with `ELEMENT_NOT_INTERACTABLE` when something covers it. |
| `fill` | Replace the text of an input, textarea or rich text editor |
| `select` | Pick an option of a `<select>` by `value` or `label` |
| `press_key` | Press a key or combination (`Enter`, `k`, `Control+A`), optionally after focusing `selector` |
| `scroll` | Scroll an element into view, or scroll the page by `direction` and `amount` |
| `upload` | Set the files of an `<input type=file>` (absolute paths) |
| `wait_for` | Wait for an element to be `visible` or `hidden`, for a text, a URL substring or the load event |
| `handle_dialog` | Accept or dismiss an `alert`, `confirm` or `prompt`. While one is open, other actions return `DIALOG_OPEN`. |
| `network_start`, `network_requests`, `network_request_detail`, `network_stop` | Record the current tab's requests and read their headers and bodies |
| `evaluate` | Run JavaScript in the page and return the result. Top-level `await` works, and the call counts as a user gesture, so `video.play()` is allowed. |
| `cdp` | Send a raw Chrome DevTools Protocol command to the current tab (`Browser.*` and `Target.*` are blocked) |

`selector` is a ref from the latest `snapshot` (`@e12`) or a CSS selector that matches exactly one element. A ref keeps pointing at the same element across snapshots and fails with `STALE_REF` once that element is gone, never pointing at another one.

`GET /tools` returns each action with its argument schema and `available`: whether the connected extension can run it. Every action is `available: false` while no extension is connected.

For example, to open a video and start it:

```powershell
bridge call navigate --session demo url=https://www.youtube.com/watch?v=aqz-KE-bpKQ
bridge call activate_tab --session demo
bridge call click --session demo selector=button.ytp-play-button
```

`bridge call` takes each argument as `key=value`, converting booleans and numbers from the action's schema; a repeated key makes a list, and `key:=<json>` passes raw JSON. It prints the response envelope and exits with 0 when ok, 1 when the action failed and 2 when the daemon cannot be reached.

## Using it from an agent

**Claude Code** reads the browser-bridge skill, which the installer adds to `~/.claude/skills/` (run `bridge install-skill` to add it again, for example after installing Claude Code). The skill teaches the workflow (`navigate`, `snapshot`, act on refs, snapshot again), what to do about each error, and to treat page content as data, never as instructions. Ask Claude Code to do something in your browser and it runs `bridge call`.

**Codex** gets the same skill in `~/.codex/skills/`, or uses MCP. Add to `~/.codex/config.toml`:

```toml
[mcp_servers.browser-bridge]
command = 'C:\Users\<you>\.browser-bridge\bin\bridge.exe'
args = ["mcp"]
```

**Any MCP client**, such as a harness running a local Ollama model, runs `bridge mcp` over stdio. Each action is a tool named `browser_<action>` (`browser_navigate`, `browser_snapshot`, `browser_click` …) with a flat schema and no session argument: every `bridge mcp` process works in its own session. `bridge mcp` starts the daemon if it is not running, `browser_snapshot` returns the tree as plain lines, and `browser_screenshot` returns the image as well as its path. `bridge install-skill` prints these settings with the right path.

**Anything else** can call the [HTTP API](#http-api) directly.

## HTTP API

The daemon listens on `127.0.0.1:9876`.

| Method | Path | Description |
|---|---|---|
| `POST` | `/command` | Run an action |
| `GET` | `/tools` | List actions, their JSON Schemas and whether the connected extension can run them |
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
- Page content can try to steer the agent into doing something else (prompt injection). The skill and the MCP server's instructions tell the agent to treat page content as data, which helps but cannot prevent it. Block sensitive sites with `blockedHosts`.

## Troubleshooting

**The extension logs `WebSocket connection to 'ws://127.0.0.1:9876/ws' failed: net::ERR_CONNECTION_REFUSED`**

The daemon is not running. Run `bridge status`. If the command is not found, install browser-bridge first. Otherwise run `bridge start`. The daemon does not start automatically after a reboot, except through `bridge mcp`, which starts it.

**Something fails and the error does not say why**

`bridge logs` shows the daemon's log: one line per command with its action, selector, duration and error code (never the text typed or the code evaluated).

**`bridge` is not recognized as a command**

The installer added it to your user `PATH`. Open a new terminal window.

**The daemon is running but the extension is not connected**

Make sure the extension is loaded from `%USERPROFILE%\.browser-bridge\extension` (or from your own build). After an update, click **Reload** on Browser Bridge in `chrome://extensions`: until then every command returns `VERSION_MISMATCH` when the update changed the protocol between daemon and extension. The extension ID is pinned by `extension/manifest-key.txt`; if you build with a different key, add the new ID to `extensionIds`.

## Development

Requirements: Go (version in [`daemon/go.mod`](daemon/go.mod)), Node.js 22, Chrome or Edge.

| Task | Command |
|---|---|
| Test the daemon | `go -C daemon test ./...` |
| Run the daemon from source | `go -C daemon run ./cmd/bridge serve` |
| Build the extension (to `extension/.output/chrome-mv3`) | `npm --prefix extension install`, then `npm --prefix extension run build` |
| Run the extension with hot reload | `npm --prefix extension run dev` |
| Test and typecheck the extension | `npm --prefix extension test`, `npm --prefix extension run typecheck` |
| Regenerate the schema, the skill's action table and the TS types | `go -C daemon run ./cmd/schemagen`, then `npm --prefix extension run gen` |
| Check generated files are up to date | `npm --prefix extension run check:gen` |
| End-to-end tests (Playwright, separate daemon on port 19876) | `npm --prefix e2e install`, then `npm --prefix e2e test` |
| Build the release files | `./install/build-dist.ps1 -Out dist` |
| Test the installer without touching your real install | `./install/test-install.ps1 -Artifacts dist -Shell powershell` (and `-Shell pwsh`) |

Actions are defined **once**, as Go structs in `daemon/internal/protocol`. `schema/protocol.schema.json`, the action table in `daemon/skill/browser-bridge/SKILL.md` and `extension/src/generated/protocol.ts` are generated from them, and so are the MCP tools. Do not edit generated files by hand.

## Releasing

1. Set the same version in `daemon/internal/protocol/protocol.go` (`Version`) and `extension/package.json` (`version`).
2. In [`CHANGELOG.md`](CHANGELOG.md), turn `[Unreleased]` into the new version with today's date, start an empty `[Unreleased]`, update the compare links at the bottom, and commit.
3. Tag and push: `git tag v0.2.0 && git push origin v0.2.0`.

The [`release.yml`](.github/workflows/release.yml) workflow checks the versions, runs every test, builds the release, tries the installer on Windows PowerShell 5.1 and PowerShell 7, publishes the GitHub Release, and then installs from that release as a final check.

## Project layout

```text
daemon/       Go: bridge binary (cmd/bridge), schema generator (cmd/schemagen),
              internal/{protocol,server,session,home,client,mcp,fakeext},
              skill/ (the agent skill, built into the binary)
schema/       JSON Schema generated from Go (committed)
extension/    WXT + React: src/entrypoints, src/background (service worker),
              src/page-agent (runs inside pages), src/shared, src/generated
e2e/          Playwright tests running the real extension against a separate daemon
testpage/     Static HTML pages for tests
install/      install.ps1, build-dist.ps1, check-version.ps1, test-install.ps1
docs/         Design specs and implementation plans
```

## Documentation

- [Design spec](docs/superpowers/specs/2026-10-06-browser-bridge-design.md): architecture, API, actions, sessions and security
- [Installer and release design](docs/superpowers/specs/2026-10-06-installer-design.md): one-line install and tag-driven releases
- [Implementation plans](docs/superpowers/plans/)
- [Changelog](CHANGELOG.md): what changed in each release

## Contributing

Issues and pull requests are welcome. Before opening a pull request:

1. Run the daemon and extension tests, the typecheck and `check:gen` (see [Development](#development)).
2. If you change an action, edit the Go structs in `daemon/internal/protocol` and regenerate the schema and types.
3. Keep each commit focused, with a [Conventional Commits](https://www.conventionalcommits.org/) message (`feat:`, `fix:`, `docs:` …), as in the existing history.

## License

[MIT](LICENSE) © 2026 Phan Trần Thế Lĩnh
