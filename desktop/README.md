# Burnrate Desktop

Tauri v2 macOS app that wraps the burnrate daemon into a distributable `.app` bundle.

## How it works

1. On launch, the Tauri shell spawns the bundled Go `burnrate` binary as a sidecar process (`burnrate serve`).
2. A splash screen displays while the sidecar starts up.
3. Once the Go server's `/health` endpoint responds, the webview navigates to the burnrate web UI at `http://127.0.0.1:9112`.
4. The system tray icon lets you show/hide the window or quit (which kills the sidecar).
5. Closing the window hides to tray — the daemon keeps running.

If an external burnrate instance is already running on the port (e.g. via `launchd`), the app attaches to it instead of spawning a new one.

## Prerequisites

- Go 1.22+ (to build the Go binary)
- Rust stable (to build the Tauri shell)
- `cargo-tauri` CLI: `cargo install tauri-cli --version "^2"`

## Development

```bash
# Build the Go binary, then run Tauri in dev mode
make dev
```

In dev mode, the Tauri shell finds the Go binary at the repo root (`../burnrate`).

## Release build

```bash
# Build Go binary + copy as sidecar + build .app/.dmg
make build
```

The output lands in `src-tauri/target/release/bundle/`:
- `macos/Burnrate.app`
- `dmg/Burnrate_0.1.0_aarch64.dmg`

## Environment

The app inherits the user's environment, so `claude`, `gh`, keychain access, and all `BURNRATE_*` overrides work as expected. Set `BURNRATE_PORT` to change the daemon port (default 9112).
