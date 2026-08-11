.PHONY: dev build deploy test typecheck

# Releases are published to Vercel Blob by `make upload` at the repo root and
# handed out through /api/releases/latest — the site itself ships no .dmg. There
# used to be a `stage-release` target here that copied a hardcoded
# Burnrate_0.1.0_aarch64.dmg into public/releases/; nothing ever served that
# file, and the pinned version made it look like the download was still on
# 0.1.0. Deploying the site is now just a build and a push.

dev:
	npm run dev

build:
	npm run build

test:
	npm test

typecheck:
	npm run typecheck

# Deploy to Vercel production
deploy: build
	npx vercel deploy --prod
