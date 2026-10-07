# Changelog

All notable changes to browser-bridge. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/). The daemon and the extension are released together under one version.

## [Unreleased]

## [0.3.0] - 2026-10-07

To update, run the install command again: it also adds the skill to Claude Code and Codex. Then click **Reload** on Browser Bridge in `chrome://extensions` to get the snapshot fixes. The protocol did not change, so the 0.2.0 extension keeps working until you do.

### Added

- `bridge call <action> --session <name> [key=value ...]` runs one action from any shell and prints the response envelope. Arguments are `key=value` pairs, converted to booleans, numbers and lists from the action's schema, so no JSON quoting is needed. `key:=<json>`, `--json` and `--json-file` (`-` for stdin) pass raw JSON. Exit code 0 when ok, 1 when the action failed, 2 when the daemon cannot be reached.
- `bridge mcp` serves every action as an MCP tool over stdio, named `browser_<action>`, for Codex, a local model's harness or any MCP client. Each process works in its own session and starts the daemon when it is not running. `browser_snapshot` returns the tree as plain lines and `browser_screenshot` returns the image itself.
- An agent skill (`SKILL.md`) for Claude Code and Codex: the workflow, what to do about each error, and safety rules, starting with treating page content as data, never as instructions. Its action table is generated from the protocol.
- `bridge install-skill [--remove]` adds the skill to Claude Code and Codex when they are installed (`CLAUDE_CONFIG_DIR` and `CODEX_HOME` are honored) and prints the MCP setup.
- `bridge logs [-f] [-n N] [--prev]` prints and follows the daemon log.
- The installer adds the skill (`-NoSkill` skips it) and removes it on uninstall.

### Changed

- On an update, the installer asks to click **Reload** on the extension instead of showing the Load unpacked steps, and warns when Chrome still runs an older version of the extension.
- The `wait_for` description says to give exactly one of `selector`, `text`, `urlContains` or `load`.

### Fixed

- Snapshot: words no longer stick together where text runs through inline elements. A YouTube title with its search terms in bold read `Những BảnNhạcLofi Chill`; it now reads `Những Bản Nhạc Lofi Chill`.
- Snapshot: a control wrapped in another control with the same role and name, as in YouTube's menu links, is one line with one ref instead of two.

### Known issues

- `wait_for` with a CSS selector that matches several elements fails with `AMBIGUOUS_SELECTOR` instead of waiting for any of them. Use a ref or a selector that matches one element.
- The nested-control fix is tested on the test page, but not yet on YouTube's sidebar in a real Chrome.
- `handle_dialog` cannot close a dialog that was already open when the extension's service worker restarted.

## [0.2.0] - 2026-10-07

To update, run the install command again, then click **Reload** on Browser Bridge in `chrome://extensions`. The protocol between the daemon and the extension changed to version 2, and every command returns `VERSION_MISMATCH` until the extension is reloaded.

### Added

- The page actions, which returned "not implemented" in 0.1.0: `snapshot` (an accessibility tree with refs such as `@e12`), `screenshot`, `click`, `fill`, `select`, `press_key`, `scroll`, `upload`, `wait_for`, `handle_dialog`, `network_start`, `network_requests`, `network_request_detail`, `network_stop`, `evaluate` and `cdp`.
- `activate_tab` brings the session's tab to the front. Chrome holds back video playback in a tab that has never been visible.
- `GET /tools` marks each action `available` when the connected extension implements it.
- `bridge help`.

### Changed

- Protocol version 2: the extension lists the actions it implements in its `hello` frame.
- The installer shows how to load the extension even with `-NoWait`.

### Fixed

- `bridge help`, `-h` and `--help` print the usage to stdout and exit with 0.

## [0.1.0] - 2026-10-06

First release.

### Added

- The `bridge` daemon: an HTTP API on `127.0.0.1:9876` (`POST /command`, `GET /tools`, `GET /status`, `POST /shutdown`) with JSON Schema validation, a queue per session, timeouts, standard error codes, localhost-only and `Origin` checks, and `blockedHosts`.
- `bridge start`, `stop`, `restart`, `status`, `serve` and `version`.
- The Chrome and Edge extension: connects to the daemon, keeps each session in its own tab group, and runs the tab actions `navigate`, `find_tab`, `list_tabs`, `close_tab`, `close_session`, `go_back`, `go_forward` and `reload`. Its side panel shows the connection, the sessions and recent commands.
- The one-line Windows installer, and releases built and tested by GitHub Actions.

[Unreleased]: https://github.com/phantranthelinh/browser-bridge/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/phantranthelinh/browser-bridge/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/phantranthelinh/browser-bridge/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/phantranthelinh/browser-bridge/releases/tag/v0.1.0
