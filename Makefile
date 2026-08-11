BINARY := burnrate
PKG    := ./cmd/burnrate
CMD    := $(BINARY)

# The dev stack deliberately does NOT share the installed app's port or data
# dir: on 9112 + ~/.burnrate it would race the installed app for
# {dataDir}/daemon.lock, and — since the desktop app now kills whatever holds
# its port or lock on launch — quietly take over the live queue with a debug
# build. Point DEV_DATA_DIR at ~/.burnrate to work against real data on
# purpose, and quit the installed app first.
DEV_PORT ?= 9113
DEV_DATA_DIR ?= $(HOME)/.burnrate-dev
DEV_ENV := BURNRATE_PORT=$(DEV_PORT) BURNRATE_DATA_DIR=$(DEV_DATA_DIR) $(if $(DRY),BURNRATE_DRYRUN=1,)

.DEFAULT_GOAL := help

.PHONY: help
help: ## List available targets
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "} {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

## --- Setup ---

.PHONY: bootstrap
bootstrap: ## Make a fresh clone/worktree workable, then verify it compiles
	@./scripts/bootstrap.sh check

## --- Frontend ---

.PHONY: web-deps
web-deps: ## Install frontend dependencies if they don't match package-lock.json
	@./scripts/bootstrap.sh deps

.PHONY: web-build
web-build: web-deps ## Build frontend for embedding
	cd web && npm run build

.PHONY: web-dev
web-dev: web-deps ## Run Next.js dev server only
	cd web && npm run dev

## --- Build / run ---

.PHONY: go-build
go-build: web-build ## Build web + Go binary to ./burnrate (web assets are embedded)
	go build -o $(BINARY) $(PKG)

.PHONY: build
build: go-build ## Build everything: web + Go + Tauri desktop app
	cd desktop && make build

.PHONY: dev
dev: go-build ## Start dev stack via PM2 (daemon + web). DRY=1 stubs claude
	pm2 delete ecosystem.config.cjs 2>/dev/null || true
	$(if $(DRY),DRY=1 )pm2 start ecosystem.config.cjs
	@printf '>> PM2 dev stack\n     daemon    127.0.0.1:%s\n     web       http://localhost:3113\n     data dir  %s\n     claude    %s\n\n' \
		'$(DEV_PORT)' '$(DEV_DATA_DIR)' '$(if $(DRY),stubbed — BURNRATE_DRYRUN=1,live)'
	@echo 'Run "make dev-logs" to tail logs, "make dev-stop" to stop.'

.PHONY: kill
kill: ## Kill ALL burnrate processes (daemon, PM2, installed app) and free ports
	pm2 delete ecosystem.config.cjs 2>/dev/null || true
	@pkill -f 'burnrate.*serve' 2>/dev/null && echo "killed burnrate daemon(s)" || true
	@for port in 9112 9113 3113; do \
		pid=$$(lsof -ti :$$port 2>/dev/null); \
		if [ -n "$$pid" ]; then kill $$pid 2>/dev/null && echo "killed pid $$pid on port $$port"; fi; \
	done
	@echo "all clear"

.PHONY: dev-stop
dev-stop: ## Stop the PM2 dev stack
	pm2 delete ecosystem.config.cjs 2>/dev/null || true

.PHONY: dev-logs
dev-logs: ## Tail PM2 logs for the dev stack
	pm2 logs --lines 50

.PHONY: dev-restart
dev-restart: go-build ## Rebuild Go binary and restart daemon
	pm2 restart burnrate-daemon

.PHONY: dev-status
dev-status: ## Show PM2 process status
	pm2 status

.PHONY: dev-web
dev-web: web-deps ## Daemon (go run) + Next dev server, no desktop app — fast Go iteration
	@trap 'kill 0' EXIT; \
	$(DEV_ENV) go run $(PKG) serve & \
	BURNRATE_API_ORIGIN=http://127.0.0.1:$(DEV_PORT) $(MAKE) web-dev & \
	wait

.PHONY: go-dev
go-dev: ## Run Go daemon only, foreground, on DEV_PORT against the LIVE data dir
	BURNRATE_PORT=$(DEV_PORT) go run $(PKG) serve

.PHONY: go-dev-dry
go-dev-dry: ## Like go-dev, but stub out claude invocations (BURNRATE_DRYRUN=1)
	BURNRATE_DRYRUN=1 BURNRATE_PORT=$(DEV_PORT) go run $(PKG) serve

.PHONY: run
run: go-build ## Build then run the foreground daemon from the built binary
	./$(BINARY) serve

.PHONY: status
status: ## Print a live usage snapshot + queue summary
	go run $(PKG) status

## --- Quality ---

.PHONY: test
test: ## Run all tests
	go test ./...

.PHONY: web-test
web-test: ## Run frontend unit tests (node --test; needs no npm install)
	cd web && npm test

.PHONY: test-race
test-race: ## Run all tests with the race detector
	go test -race ./...

.PHONY: fmt
fmt: ## Format all Go sources in place
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any Go source is not gofmt-clean
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: fmt-check vet web-deps ## Run all linters
	cd web && npm run lint

.PHONY: secrets
secrets: ## Scan the working tree for committed credentials
	./scripts/secret-scan.sh tree

.PHONY: secrets-history
secrets-history: ## Scan every commit reachable from any ref
	./scripts/secret-scan.sh history

.PHONY: snapshot
snapshot: ## Build the history-free repo to publish (stops before pushing)
	./scripts/publish-snapshot.sh

.PHONY: check
check: lint test web-test secrets ## Full quality gate
	$(MAKE) web-build

.PHONY: recover
recover: go-build ## Sweep unpushed branches -> draft PRs, prune stale worktrees
	./$(BINARY) recover

## --- Desktop app (Tauri) ---

.PHONY: desktop-dev
desktop-dev: go-build ## Desktop app alone, on the dev port + data dir (no Next dev server)
	$(DEV_ENV) $(MAKE) -C desktop dev-app

.PHONY: desktop-build
desktop-build: go-build ## Build the .app bundle (Go binary + Tauri bundle; the DMG comes from `make deploy`)
	cd desktop && make build

RELEASE_NOTES ?= $(R)

.PHONY: deploy
deploy: ## Full release: bump, build, sign, notarize. BUMP=patch|minor|major R="notes"
	cd desktop && make deploy-full BUMP=$(or $(BUMP),patch) RELEASE_NOTES="$(RELEASE_NOTES)"

.PHONY: upload
upload: ## Upload built DMG to Vercel Blob + register release. OLD_VERSION=x.y.z to delete prior.
	cd desktop && make upload RELEASE_NOTES="$(RELEASE_NOTES)" OLD_VERSION="$(OLD_VERSION)"

## --- Housekeeping ---

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY)
	rm -rf dist
	go clean -testcache
