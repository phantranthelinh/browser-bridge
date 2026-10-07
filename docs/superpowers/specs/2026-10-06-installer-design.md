# Installer and Release — Design Spec

- **Date:** 2026-10-06
- **Status:** Draft, awaiting review
- **Base spec:** `2026-10-06-browser-bridge-design.md` (referred to as the "base spec"). This spec only adds release and installation; the API, CLI and home directory are still defined in the base spec.

## 1. Goals

Install browser-bridge on someone else's Windows machine with a single command, the way Kimi WebBridge does:

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex
```

This command installs the daemon and the extension; the user only has to Load unpacked the extension **once**.

### 1.1 How Kimi does it (for comparison)

Kimi's `install.ps1` (the real one was read) only downloads `kimi-webbridge.exe` to `%USERPROFILE%\.kimi-webbridge\bin\`, runs `start`, then `install-skill -y`. Users install Kimi's extension themselves from the Chrome Web Store. No script can install an extension on a personal machine: Chrome only allows installing without a user click through enterprise policy, on centrally managed machines.

### 1.2 Decisions made

- **Extension is loaded unpacked**, not published to the Store (keeping base spec §1.3). The script downloads the extension to a fixed folder and guides the user through Load unpacked.
- **Build now** on what already exists (8 tab actions, called over HTTP). `install-skill` and MCP configuration will be added to the script when the CLI plan is complete. (Done: the script runs `bridge install-skill`, §3.2 step 5.)
- **No autostart at login**, same as Kimi. The script starts the daemon at install time; after the machine restarts, the user (later the agent, following `SKILL.md` or `bridge mcp`) runs `bridge start`.
- **Built with GitHub Actions, distributed through GitHub Releases.** The repo is public, so downloads need no login.
- Windows amd64 only (Windows ARM machines run the amd64 build through emulation). The script runs on Windows PowerShell 5.1 and PowerShell 7.

### 1.3 Completion criteria

On a Windows machine with Chrome, without Go or Node:
1. Run the one-line install command.
2. Load unpacked the folder the script points to.
3. The script reports that the extension is connected, and `curl` to `POST /command` with `navigate` opens a page.

Re-running the install command while the daemon is running updates it without error. Running `-Uninstall` removes everything cleanly.

### 1.4 Out of scope

- Installing the extension automatically, publishing to the Store.
- Code-signing the `.exe` file.
- Starting the daemon automatically at login.
- macOS/Linux (`install.sh`).

## 2. Release files

Each GitHub Release `vX.Y.Z` has exactly 3 files with fixed names so that the URL `releases/latest/download/<name>` is always valid:

| File | Contents |
|---|---|
| `install.ps1` | The install script (§3) |
| `bridge-windows-amd64.exe` | Daemon and CLI, `go build -trimpath -ldflags "-s -w"` |
| `browser-bridge-extension.zip` | Production build of the extension (`wxt zip`), the zip root is the folder containing `manifest.json` |

## 3. `install.ps1`

### 3.1 Invocation

```powershell
irm https://github.com/phantranthelinh/browser-bridge/releases/latest/download/install.ps1 | iex   # latest version
$env:BRIDGE_VERSION = '0.1.0'; irm <url>/install.ps1 | iex                                        # pin a version
iex "& { $(irm <url>/install.ps1) } -Uninstall"                                                   # uninstall
```

`<url>` is `https://github.com/phantranthelinh/browser-bridge/releases/latest/download`.

| Flag | Meaning |
|---|---|
| `-Help` | Print usage |
| `-NoStart` | Install but do not start the daemon (and therefore do not wait for the extension) |
| `-NoPath` | Do not modify `PATH` |
| `-NoWait` | Do not wait for the extension to connect (for scripts and CI) |
| `-NoSkill` | Do not add the browser-bridge skill to Claude Code and Codex |
| `-Uninstall` | Uninstall (§3.4) |

| Environment variable | Meaning |
|---|---|
| `BRIDGE_VERSION` | Pin a version, for example `0.1.0`. Default: the latest version |
| `BRIDGE_HOME` | Install directory, default `%USERPROFILE%\.browser-bridge`. The same variable the daemon uses (base spec §9.1) |
| `BRIDGE_INSTALL_BASE` | Download source instead of GitHub Releases: a URL, or a local folder (in which case files are copied instead of downloaded). Used for testing |

No admin rights are needed. The script enables TLS 1.2 for Windows PowerShell 5.1.

### 3.2 Install steps

Download source: `BRIDGE_INSTALL_BASE` if set; otherwise `https://github.com/phantranthelinh/browser-bridge/releases/download/v<BRIDGE_VERSION>` when a version is pinned, and `…/releases/latest/download` in all other cases.

1. If `bin\bridge.exe` already exists: run `bridge stop`. Windows locks a running `.exe` file, so without stopping first a reinstall would fail at step 2.
2. Download `bridge-windows-amd64.exe` to a temp file (retry 3 times, 5 seconds apart), then move it to `bin\bridge.exe`.
3. Download `browser-bridge-extension.zip`, extract it to a temp folder, check that `manifest.json` exists, delete the old `extension\`, then move the new folder in. The `extension\` path always stays the same, so an extension that was Loaded unpacked once keeps working across updates.
4. Unless `-NoPath` is set: add `bin\` to the user's `PATH` (HKCU), and to `$env:Path` of the current window.
5. Unless `-NoSkill` is set: run `bridge install-skill`, which adds the skill to Claude Code and Codex when they are installed and prints the MCP configuration (base spec §9.2).
6. Unless `-NoStart` is set: run `bridge start`.
7. Unless `-NoStart` is set: read `bridge status`. With `-NoWait` the script reports what it sees and does not wait.
   - Extension connected: report done. If the connected extension's version differs from the daemon's, Chrome still runs the old build: ask for a Reload in `chrome://extensions`.
   - Not connected, first install (`extension\manifest.json` did not exist before step 3): print the 3 steps (open `chrome://extensions`, turn on Developer mode, Load unpacked → `<BRIDGE_HOME>\extension`) and copy that path to the clipboard.
   - Not connected, update: ask for a Reload, with Load unpacked as a fallback in one line. The old build is most likely loaded and reconnects within 30 seconds; nothing is connected yet only because the daemon just restarted.
   - `/status` shows a protocol version mismatch: report that the extension needs a Reload.
   - Without `-NoWait`: wait up to 3 minutes, checking every 2 seconds, and report when the extension connects (or that it needs a Reload, once). Ctrl+C skips the wait; everything has already been installed.
8. Print a summary: the installed version, the daemon address, the `bridge status`, `bridge call` and `bridge mcp` commands, and a one-line security warning (§6).

### 3.3 Errors

- Each failed step prints one red line naming the step and how to fix it; for a failed download it also includes the URL. If steps 1–4 fail, the script stops with exit code 1.
- A failed `bridge start` is only a warning, with the path to `logs\daemon.log`, because the binary and extension are already installed.
- If the 3-minute wait ends without the extension connecting, repeat the 3 steps. This is not treated as an error.

### 3.4 Uninstall (`-Uninstall`)

1. `bridge stop` (if `bin\bridge.exe` exists).
2. `bridge install-skill --remove`, which deletes the skill from Claude Code and Codex.
3. Remove `bin\` from the user's `PATH`.
4. Delete exactly what bridge created in `BRIDGE_HOME`: `bin\`, `extension\`, `logs\`, `artifacts\`, `config.json`, `daemon.pid`, `daemon.addr`. Then delete `BRIDGE_HOME` only if it is empty. Do not recursively delete the whole `BRIDGE_HOME`, because a `BRIDGE_HOME` pointed at the wrong place (for example the user folder) would lose all its data.
5. Remind the user to remove the extension in `chrome://extensions`; Chrome will show an error for an extension whose folder has been deleted.

## 4. CLI commands needed by the installer

The script uses `start`, `stop`, `status`. These three commands, together with `restart`, are implemented per base spec §9.2 (updated). Summary:

- **Address:** read `daemon.addr` if present; otherwise `--addr` > `config.json` > `127.0.0.1:9876`.
- **`start`:** if `/status` already responds, print "already running" and exit 0. If not, run this same exe with `serve` as a detached process (`DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`, no window, stdout/stderr discarded) and wait up to 5 seconds for `/status`. On failure, print the last lines of `logs\daemon.log` and exit 1.
- **`stop`:** `POST /shutdown`, wait up to 5 seconds. If the daemon hangs, kill it by the `pid` from `/status`, never by the `daemon.pid` file, and only if the exe of that `pid` has the same name as `bridge` itself; otherwise `stop` refuses and exits 1. If nobody responds, delete any leftover `daemon.pid`/`daemon.addr`. Exit 0.
- **`restart`:** `stop` then `start`.
- **`status`:** print the JSON from `/status` (which now includes `pid`), or `{"running": false, "addr": …}`. Exit 0 when running, 1 when not.

On the daemon side, add `POST /shutdown` (base spec §4) and the `pid` field in `/status` (base spec §9.3).

## 5. Release

### 5.1 Version

The version lives in two places: `protocol.Version` (`daemon/internal/protocol/protocol.go`) and `version` in `extension/package.json`. To cut a new release:
1. Edit both, commit.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`.

The workflow refuses to build when the tag, Go and `package.json` are not all the same version.

### 5.2 Workflow `.github/workflows/release.yml`

Runs on a push of a tag `v*`, on `windows-latest`, with `contents: write` permission.

1. **Check the version** (§5.1).
2. **Test:**
   - daemon: `go vet ./...`, `go test ./...`;
   - extension: `npm ci`, `check:gen`, `typecheck`, `test`;
   - E2E: `npm ci`, `npx playwright install chromium`, `npm test`.
3. **Build into `dist/`** exactly the 3 files in §2.
4. **Test the install script** on `dist/` (§5.3), running it with both `powershell.exe` (5.1) and `pwsh` (7).
5. **Publish:** `gh release create vX.Y.Z dist/* --title vX.Y.Z --generate-notes`.
6. **Post-publish smoke test:** run the exact install command from GitHub (`irm …/releases/download/vX.Y.Z/install.ps1 | iex` with `BRIDGE_VERSION`), into a temp `BRIDGE_HOME`, with `-NoPath -NoWait`. Check that `bridge version` matches the tag, then `-Uninstall`.

### 5.3 Install script tests

`install/test-install.ps1 -Artifacts <dist folder>` runs on both a dev machine and CI. It uses `BRIDGE_INSTALL_BASE=<dist>`, a temp `BRIDGE_HOME`, `-NoPath -NoWait`, and a port other than 9876 (written to the `config.json` of the temp `BRIDGE_HOME`), so it does not touch the real installation on the machine. The checks:

1. **Fresh install:** `bin\bridge.exe` exists, `bridge version` is correct, `extension\manifest.json` has the `key` field, `bridge status` exits 0.
2. **Reinstall while the daemon is running:** no file-lock error, and the daemon is still running at the correct version.
3. **`-Uninstall`:** `/status` no longer responds, and `BRIDGE_HOME` is gone.
4. **`-Uninstall` does not delete foreign files:** a file not created by bridge, placed in `BRIDGE_HOME` beforehand, is still there after uninstall, and `BRIDGE_HOME` remains as well.

## 6. Security

| Risk | Handling |
|---|---|
| `irm \| iex` runs code from GitHub with the user's privileges | Download only from the repo's GitHub Releases over HTTPS. The README states clearly what the command does. Accepted risk, same as Kimi |
| The `.exe` is not code-signed, so Defender or SmartScreen may raise a false warning | Accepted risk. The README documents how to recognize it |
| On an installed machine, every process of the user can control the browser through bridge (base spec §7) | The script prints a one-line warning when installation finishes; the README repeats it |
| `/shutdown` lets a local process shut down the daemon | Same level as the rest of the API (base spec §7): whoever can call `/command` can already control the browser. It still goes through the Origin, Host and Content-Type checks, so web pages cannot call it |

## 7. README

Rewrite `README.md` for the recipient:
1. The one-line install command, with the 3 Load unpacked steps.
2. Verification (`bridge status`, side panel) and trying out one command.
3. Updating (re-run the install command, click Reload on the extension) and uninstalling (`-Uninstall`).
4. The security warning (§6).
5. A "Releasing" section for maintainers (§5.1).

## 8. Implementation order

1. Daemon: `pid` in `/status`, `POST /shutdown`.
2. CLI: `start`, `stop`, `restart`, `status`.
3. `install/install.ps1` and `install/test-install.ps1`, tested on a dev machine with a hand-built `dist/`.
4. `.github/workflows/release.yml`.
5. README.
6. Cut a real `v0.1.0` release and try the install command on a machine (or Windows user) that has never installed it.
