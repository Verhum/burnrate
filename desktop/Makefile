REPO_ROOT := $(shell cd .. && pwd)
GO_BINARY  := $(REPO_ROOT)/burnrate
TARGET     := $(shell rustc -Vv | grep host | awk '{print $$2}')
SIDECAR    := src-tauri/binaries/burnrate-$(TARGET)

# The Tauri CLI shells out to `cargo metadata`, so cargo/rustc must be on PATH.
# rustup is often installed under ~/.cargo with PATH set up only in interactive
# shells (via ~/.cargo/env), which make's non-login /bin/sh does NOT source. We
# source it here when present so targets work regardless of how the shell is set
# up. Override with CARGO_ENV= (empty) if cargo is already on PATH.
CARGO_ENV ?= [ -f "$$HOME/.cargo/env" ] && . "$$HOME/.cargo/env";

SRC_TAURI := src-tauri
BUNDLE_DIR := $(SRC_TAURI)/target/release/bundle

.DEFAULT_GOAL := help

.PHONY: help
help: ## List targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: web-build
web-build: ## Build frontend for Tauri embedding
	cd $(REPO_ROOT)/web && npm run build

.PHONY: go-build
go-build: ## Build the Go binary in the repo root
	cd $(REPO_ROOT) && go build -o burnrate ./cmd/burnrate

.PHONY: sidecar
sidecar: go-build ## Copy Go binary as a Tauri sidecar (target-triple named)
	mkdir -p src-tauri/binaries
	cp $(GO_BINARY) $(SIDECAR)
	chmod +x $(SIDECAR)

.PHONY: icons
icons: ## Generate app icons from a source PNG (requires `tauri icon`)
	@if [ -f icon.png ]; then \
		cd src-tauri && cargo tauri icon ../icon.png; \
	else \
		echo "Place an icon.png in desktop/ first"; \
	fi

# ---------------------------------------------------------------------------
# dev-signed: builds + signs the debug binary so keychain + network work.
#
# Unlike `make dev`, this does NOT use `tauri dev` (which auto-launches the
# unsigned binary before we can sign it). Instead we:
#   1. Build the Go sidecar.
#   2. Compile a DEBUG binary via `tauri build --debug --no-bundle`.
#   3. Codesign it with the hardened runtime + entitlements.
#   4. Launch it directly.
#
# Trade-off: no hot-reload (Tauri's file watcher is part of `tauri dev`).
# Use `make dev` for fast iteration; use `make dev-signed` when you need
# keychain or network entitlements to actually work.
# ---------------------------------------------------------------------------
dev-signed: dev-build ## Run the app in dev, code-signed (keychain + network work). Needs APPLE_SIGNING_IDENTITY.
	@if [ -z "$(APPLE_SIGNING_IDENTITY)" ]; then \
		echo "" >&2; \
		echo "ERROR: APPLE_SIGNING_IDENTITY is not set." >&2; \
		echo "       Export it or add it to .env so the debug binary can be signed." >&2; \
		echo "       Example: export APPLE_SIGNING_IDENTITY='Developer ID Application: ...'" >&2; \
		echo "" >&2; \
		exit 1; \
	fi
	codesign --force --options runtime --timestamp=none \
		--entitlements "$(SRC_TAURI)/Entitlements.plist" \
		--sign "$(APPLE_SIGNING_IDENTITY)" "$(SRC_TAURI)/target/debug/burnrate-desktop"
	@echo ">> Debug binary signed. Launching..."
	cd $(SRC_TAURI) && $(CARGO_ENV) ./target/debug/burnrate-desktop

# `cargo check` on its own fails in a fresh checkout with
# "glob pattern binaries/burnrate-* path not found" — tauri.conf.json declares the
# sidecar as a bundle resource, so the build script needs it to exist even for a
# type check. Hence the sidecar prerequisite.
.PHONY: check
check: sidecar ## Type-check the Rust crate
	cd $(SRC_TAURI) && $(CARGO_ENV) cargo check

# NOT `cargo tauri dev`: its built-in dev server hosts the UI at
# http://localhost:1430, an origin the daemon's origin guard rejects (and that
# WKWebView CORS-blocks — only the tauri:// scheme is CORS-exempt), so every
# page sits on a loading spinner while the daemon is perfectly healthy. The
# app must serve its UI from tauri://localhost, i.e. run the built shell.
# NOTE: unlike the repo-root `make dev`, this runs against the LIVE data dir
# (~/.burnrate, port 9112) and an armed scheduler.
.PHONY: dev
dev: ## Build + launch the app from source (live data dir — see note above)
	@echo ">> desktop dev runs against the LIVE ~/.burnrate on port 9112; for the isolated stack use \`make dev\` at the repo root." >&2
	$(MAKE) dev-app

.PHONY: dev-build
dev-build: sidecar ## Compile the debug binary (no bundle, no signing, no launch)
	cd $(SRC_TAURI) && $(CARGO_ENV) cargo tauri build --debug --no-bundle

# Launcher for the repo-root `make dev` stack. Signs when it can, but ALWAYS
# launches: half a stack is worse than a limited one, and signing legitimately
# fails in environments without the Developer ID (fresh clone, CI, a shell whose
# HOME does not reach the login keychain). Unsigned means no keychain, so the
# daemon polls usage with no OAuth token and the UI sits empty — stated loudly
# here because otherwise it reads as a bug in whatever you were testing.
.PHONY: dev-app
dev-app: dev-build ## Build + launch the desktop app for the root dev stack (signs when possible)
	@if [ -z "$(APPLE_SIGNING_IDENTITY)" ]; then \
		echo ">> APPLE_SIGNING_IDENTITY not set — launching UNSIGNED: no keychain, so usage stays empty." >&2; \
	elif codesign --force --options runtime --timestamp=none \
			--entitlements "$(SRC_TAURI)/Entitlements.plist" \
			--sign "$(APPLE_SIGNING_IDENTITY)" \
			"$(SRC_TAURI)/target/debug/burnrate-desktop"; then \
		echo ">> Debug binary signed — keychain + network work."; \
	else \
		echo ">> Signing FAILED — launching UNSIGNED: no keychain, so usage stays empty." >&2; \
	fi
	cd $(SRC_TAURI) && $(CARGO_ENV) ./target/debug/burnrate-desktop

# `--bundles app` deliberately skips the DMG. Tauri's DMG bundler mounts the
# image and drives Finder via AppleScript to write the window layout (custom
# background + icon positions) into .DS_Store — and create-dmg's
# template.applescript does `open / close / open` and never closes the last one,
# so every DMG build pops two Finder windows onto your desktop. `make sign`
# `rm -f`s this DMG and rebuilds it from the SIGNED .app anyway (see below), so
# building one here was pure waste plus two stray windows on every build.
.PHONY: build
build: web-build sidecar ## Build the release .app bundle (includes sidecar + web; no DMG — `make sign` builds that)
	cd $(SRC_TAURI) && $(CARGO_ENV) cargo tauri build --bundles app
	@echo "App bundle under $(BUNDLE_DIR)/macos (DMG is produced by 'make sign')"

# ---------------------------------------------------------------------------
# Code signing + notarization (Developer ID, hardened runtime, NOT App Store).
#
# Requires (supplied via env; NEVER commit these):
#   APPLE_SIGNING_IDENTITY  e.g. "Developer ID Application: Verifiably Human, Inc. (853X8ZH5DQ)"
#   APPLE_ID                Apple ID email used for notarytool
#   APPLE_TEAM_ID           10-char Apple Developer Team ID (853X8ZH5DQ)
#   APPLE_APP_PASSWORD      app-specific password (account.apple.com), NOT your login pw
#
# `make build` (above) does NOT sign because signingIdentity is null in
# tauri.conf.json. The flow below signs the produced .app after the fact:
#   1. sign the bundled sidecar (a separate binary inside Resources) with the
#      hardened runtime,
#   2. re-sign the .app itself (hardened runtime) so its seal covers the sidecar,
#   3. notarize the DMG (which covers the .app inside) and staple the ticket.
# ---------------------------------------------------------------------------

# NOTE: VERSION and DMG are recursively expanded (`=`, not `:=`) on purpose.
# `deploy-full` runs `_bump-version` — which rewrites tauri.conf.json — and then
# `build sign notarize` in the SAME make process. A `:=` variable is expanded
# once at parse time, i.e. BEFORE the bump, so every later target would still
# see the OLD version: `sign` would build Burnrate_<old>.dmg from the freshly
# bumped .app, `notarize` would notarize that misnamed file, and `upload` would
# publish something whose name disagrees with the version inside it.
# With `=` the grep re-runs at each use, so they see the bumped version.
VERSION = $(shell grep -m1 '"version"' $(SRC_TAURI)/tauri.conf.json | sed 's/.*"\([0-9]*\.[0-9]*\.[0-9]*\)".*/\1/')
DMG = $(BUNDLE_DIR)/dmg/Burnrate_$(VERSION)_aarch64.dmg
APP := $(BUNDLE_DIR)/macos/Burnrate.app
SIDECAR_IN_APP := $(APP)/Contents/Resources/binaries/burnrate-$(TARGET)

# Load env vars from .env if it exists (for signing/notarization credentials)
-include .env

.PHONY: sign
sign: ## Codesign the bundled sidecar + the .app with the hardened runtime (needs APPLE_SIGNING_IDENTITY).
	@if [ ! -d "$(APP)" ]; then \
		echo "ERROR: $(APP) not found — run 'make build' first." >&2; exit 1; \
	fi
	@echo ">> Signing bundled sidecar (hardened runtime)"
	codesign --force --options runtime --timestamp \
		--sign "$(APPLE_SIGNING_IDENTITY)" "$(SIDECAR_IN_APP)"
	@echo ">> Signing app bundle (hardened runtime, entitlements)"
	codesign --force --options runtime --timestamp \
		--entitlements "$(SRC_TAURI)/Entitlements.plist" \
		--sign "$(APPLE_SIGNING_IDENTITY)" "$(APP)"
	@echo ">> Verifying signature"
	codesign --verify --deep --strict --verbose=2 "$(APP)"
	@echo ">> Rebuilding DMG with the signed .app + Applications symlink + background"
	# The AppleScript below is passed one line per -e flag, NOT as a single
	# multi-line string. A `-e "line \n\` continuation looks like it works and does
	# not: inside double quotes the shell eats the backslash-newline as a line
	# continuation and leaves `\n` as two literal characters, so osascript receives
	# one line full of stray backslashes and dies with
	#   29:30: syntax error: Expected end of line but found unknown token
	# where 29 is the character offset of the first backslash. Single quotes also
	# keep the AppleScript's own double quotes readable.
	#
	# The disk is addressed by $$VOLUME_NAME, never the literal "Burnrate". A run
	# that dies before detaching leaves its read-write image mounted at
	# /Volumes/Burnrate, so the next run mounts at "/Volumes/Burnrate 1" — and a
	# hardcoded `tell disk "Burnrate"` then decorates the leftover volume and exits
	# 0, shipping a DMG with no background. Hence also the || cleanup below: never
	# leave a volume mounted on failure.
	rm -f "$(DMG)"
	@DMG_STAGING=$$(mktemp -d) && \
		DMG_RW=$$(mktemp -u).dmg && \
		cp -R "$(APP)" "$$DMG_STAGING/" && \
		ln -s /Applications "$$DMG_STAGING/Applications" && \
		mkdir -p "$$DMG_STAGING/.background" && \
		cp "$(SRC_TAURI)/dmg/background.png" "$$DMG_STAGING/.background/background.png" && \
		hdiutil create -volname "Burnrate" -srcfolder "$$DMG_STAGING" \
			-ov -format UDRW "$$DMG_RW" && \
		rm -rf "$$DMG_STAGING" && \
		MOUNT_DIR=$$(hdiutil attach -readwrite -noverify -noautoopen "$$DMG_RW" \
			| tail -1 | awk -F'\t' '{print $$NF}') && \
		VOLUME_NAME=$$(basename "$$MOUNT_DIR") && \
		osascript \
			-e 'tell application "Finder"' \
			-e "tell disk \"$$VOLUME_NAME\"" \
			-e 'open' \
			-e 'set current view of container window to icon view' \
			-e 'set toolbar visible of container window to false' \
			-e 'set statusbar visible of container window to false' \
			-e 'set the bounds of container window to {200, 200, 860, 622}' \
			-e 'set viewOptions to the icon view options of container window' \
			-e 'set arrangement of viewOptions to not arranged' \
			-e 'set icon size of viewOptions to 72' \
			-e 'set background picture of viewOptions to file ".background:background.png"' \
			-e 'set position of item "Burnrate.app" of container window to {180, 170}' \
			-e 'set position of item "Applications" of container window to {480, 170}' \
			-e 'close' \
			-e 'open' \
			-e 'update without registering applications' \
			-e 'delay 2' \
			-e 'close' \
			-e 'end tell' \
			-e 'end tell' && \
		{ i=0; while [ $$i -lt 15 ] && [ ! -f "$$MOUNT_DIR/.DS_Store" ]; do \
			i=$$((i+1)); sleep 1; \
		done; [ -f "$$MOUNT_DIR/.DS_Store" ]; } && \
		sync && \
		hdiutil detach "$$MOUNT_DIR" -quiet && \
		hdiutil convert "$$DMG_RW" -format UDZO -imagekey zlib-level=9 -o "$(DMG)" && \
		rm -f "$$DMG_RW" \
		|| { \
			echo "ERROR: DMG rebuild failed. If Finder never wrote .DS_Store the image" >&2; \
			echo "       would ship with no background and no icon layout." >&2; \
			[ -n "$$MOUNT_DIR" ] && hdiutil detach "$$MOUNT_DIR" -force -quiet 2>/dev/null; \
			rm -rf "$$DMG_STAGING"; rm -f "$$DMG_RW"; \
			exit 1; \
		}

.PHONY: verify
verify: ## Verify code signature and notarization of the current DMG.
	@if [ ! -f "$(DMG)" ]; then \
		echo "ERROR: no DMG at $(DMG)" >&2; exit 1; \
	fi
	@echo ">> Checking .app code signature"
	codesign --verify --deep --strict --verbose=2 "$(APP)"
	@echo ""
	@echo ">> Checking DMG contents for Applications symlink + background"
	@MOUNT=$$(hdiutil attach -nobrowse -readonly "$(DMG)" 2>/dev/null \
		| tail -1 | awk -F'\t' '{print $$NF}') && \
		if [ -L "$$MOUNT/Applications" ]; then \
			echo "       ✓ Applications symlink present"; \
		else \
			echo "       ✗ Applications symlink MISSING" >&2; \
		fi && \
		if [ -f "$$MOUNT/.background/background.png" ]; then \
			echo "       ✓ Background image present"; \
		else \
			echo "       ✗ Background image MISSING" >&2; \
		fi && \
		hdiutil detach "$$MOUNT" -quiet 2>/dev/null || true
	@echo ""
	@echo ">> Checking Gatekeeper assessment"
	spctl --assess --type open --context context:primary-signature -v "$(DMG)" 2>&1 || true
	@echo ""
	@echo ">> Checking stapled notarization ticket"
	xcrun stapler validate "$(DMG)" 2>&1 || true

.PHONY: notarize
notarize: ## Notarize + staple the DMG (needs APPLE_ID / APPLE_TEAM_ID / APPLE_APP_PASSWORD).
	@if [ ! -f "$(DMG)" ]; then \
		echo "ERROR: no DMG for v$(VERSION) at $(DMG) — run 'make sign' first." >&2; exit 1; \
	fi
	@echo ">> Submitting DMG to Apple notary service (waits for result)"
	xcrun notarytool submit "$(DMG)" \
		--apple-id "$(APPLE_ID)" \
		--team-id "$(APPLE_TEAM_ID)" \
		--password "$(APPLE_APP_PASSWORD)" \
		--wait
	@echo ">> Stapling the notarization ticket to the DMG"
	xcrun stapler staple "$(DMG)"

BUMP ?= patch

.PHONY: _bump-version
_bump-version:
	@OLD_VERSION=$(VERSION); \
	MAJOR=$$(echo $$OLD_VERSION | cut -d. -f1); \
	MINOR=$$(echo $$OLD_VERSION | cut -d. -f2); \
	PATCH=$$(echo $$OLD_VERSION | cut -d. -f3); \
	case "$(BUMP)" in \
		major) MAJOR=$$((MAJOR + 1)); MINOR=0; PATCH=0 ;; \
		minor) MINOR=$$((MINOR + 1)); PATCH=0 ;; \
		patch) PATCH=$$((PATCH + 1)) ;; \
		*) echo "ERROR: BUMP must be patch, minor, or major"; exit 1 ;; \
	esac; \
	NEW_VERSION="$$MAJOR.$$MINOR.$$PATCH"; \
	NOTES="$(RELEASE_NOTES)"; \
	RELEASE_FILE="$(REPO_ROOT)/releases/v$${NEW_VERSION}.md"; \
	echo ""; \
	echo "========================================"; \
	echo "  Burnrate release: v$$OLD_VERSION → v$$NEW_VERSION ($(BUMP))"; \
	echo "========================================"; \
	echo ""; \
	echo "[1/3] Bumping version..."; \
	sed -i '' 's/"version": ".*"/"version": "'$$NEW_VERSION'"/' $(SRC_TAURI)/tauri.conf.json; \
	sed -i '' 's/^version = ".*"/version = "'$$NEW_VERSION'"/' $(SRC_TAURI)/Cargo.toml; \
	echo "       tauri.conf.json → $$NEW_VERSION"; \
	echo "       Cargo.toml      → $$NEW_VERSION"; \
	echo ""; \
	echo "[2/3] Writing release notes..."; \
	echo "# v$$NEW_VERSION" > "$$RELEASE_FILE"; \
	echo "" >> "$$RELEASE_FILE"; \
	echo "**Date:** $$(date +%Y-%m-%d)" >> "$$RELEASE_FILE"; \
	echo "**Bump:** $(BUMP) (from v$$OLD_VERSION)" >> "$$RELEASE_FILE"; \
	echo "" >> "$$RELEASE_FILE"; \
	if [ -n "$$NOTES" ]; then \
		echo "## Notes" >> "$$RELEASE_FILE"; \
		echo "" >> "$$RELEASE_FILE"; \
		echo "$$NOTES" >> "$$RELEASE_FILE"; \
		echo "" >> "$$RELEASE_FILE"; \
	fi; \
	echo "## Build" >> "$$RELEASE_FILE"; \
	echo "" >> "$$RELEASE_FILE"; \
	echo "- Target: darwin-aarch64" >> "$$RELEASE_FILE"; \
	echo "- Status: built" >> "$$RELEASE_FILE"; \
	echo "       $$RELEASE_FILE"; \
	echo ""; \
	echo "[3/3] Committing + tagging v$$NEW_VERSION..."; \
	cd $(REPO_ROOT) && git add desktop/src-tauri/tauri.conf.json desktop/src-tauri/Cargo.toml "releases/v$${NEW_VERSION}.md" && \
		git commit -m "release: v$$NEW_VERSION" && \
		git tag "v$$NEW_VERSION"

.PHONY: deploy-full
deploy-full: _bump-version build sign notarize ## Bump, build, sign, notarize. Then run `make upload`. BUMP=patch|minor|major
	@echo ""
	@echo "========================================"
	@echo "  Build complete! DMG ready at:"
	@echo "  $(BUNDLE_DIR)/dmg/Burnrate_$(VERSION)_aarch64.dmg"
	@echo ""
	@echo "  To publish the release:"
	@echo "    make upload R='what changed'"
	@echo "========================================"

.PHONY: upload
#
# NOTE: the recipe is one long backslash-continued shell command, so make only
# ever sees the exit status of the LAST statement in it. Without `set -e` a
# failed upload in [1/4] does not stop [2/4]–[4/4]: make would go on to delete
# the previous blob, stamp "Published" into the release notes, and print
# "published!" while the site still served the old version. That is exactly how
# the download link got stuck on v0.1.0 pointing at a .dmg that had been
# deleted. `set -e` makes step 1 fatal.
upload: ## Upload current version's DMG to Vercel Blob + register release metadata.
	@set -e; \
	VER=$(VERSION); \
	NOTES="$(RELEASE_NOTES)"; \
	OLD_DMG_VERSION="$(OLD_VERSION)"; \
	RELEASE_FILE="$(REPO_ROOT)/releases/v$${VER}.md"; \
	echo ""; \
	echo "========================================"; \
	echo "  Publishing v$$VER"; \
	echo "========================================"; \
	echo ""; \
	echo "[1/4] Uploading DMG to Vercel Blob..."; \
	cd $(REPO_ROOT)/landing && ./scripts/upload-release.sh "$$VER" "$$NOTES"; \
	echo ""; \
	echo "[2/4] Cleaning up old blob..."; \
	if [ -n "$$OLD_DMG_VERSION" ] && [ "$$OLD_DMG_VERSION" != "$$VER" ]; then \
		[ -f $(REPO_ROOT)/.env ] && . $(REPO_ROOT)/.env || true; \
		curl -sS -X POST "https://blob.vercel-storage.com/delete" \
			-H "Authorization: Bearer $$BLOB_READ_WRITE_TOKEN" \
			-H "x-api-version: 7" \
			-H "Content-Type: application/json" \
			-d "{\"urls\":[\"releases/Burnrate_$${OLD_DMG_VERSION}_darwin-aarch64.dmg\"]}" >/dev/null 2>&1 || true; \
		echo "       deleted old blob: Burnrate_$${OLD_DMG_VERSION}_darwin-aarch64.dmg"; \
	else \
		echo "       no old version to clean up (pass OLD_VERSION=x.y.z to delete a specific one)"; \
	fi; \
	echo ""; \
	echo "[3/4] Updating release log..."; \
	if [ -f "$$RELEASE_FILE" ]; then \
		echo "" >> "$$RELEASE_FILE"; \
		echo "## Published" >> "$$RELEASE_FILE"; \
		echo "" >> "$$RELEASE_FILE"; \
		echo "- Published: $$(date '+%Y-%m-%d %H:%M')" >> "$$RELEASE_FILE"; \
		echo "- URL: https://www.burnthemtokens.com/api/releases/latest" >> "$$RELEASE_FILE"; \
		if [ -n "$$OLD_DMG_VERSION" ] && [ "$$OLD_DMG_VERSION" != "$$VER" ]; then \
			echo "- Replaced: v$$OLD_DMG_VERSION" >> "$$RELEASE_FILE"; \
		fi; \
		echo "       updated $$RELEASE_FILE"; \
	else \
		echo "       no release file found at $$RELEASE_FILE (skipping)"; \
	fi; \
	echo ""; \
	echo "[4/4] Committing release log..."; \
	cd $(REPO_ROOT) && git add "releases/v$${VER}.md" 2>/dev/null && \
		git commit -m "release: v$$VER published" || echo "       nothing to commit"; \
	echo ""; \
	echo "========================================"; \
	echo "  v$$VER published!"; \
	echo "  https://www.burnthemtokens.com/api/releases/latest"; \
	echo "========================================"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf src-tauri/target src-tauri/binaries
