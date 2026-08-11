#!/usr/bin/env bash
set -euo pipefail

# Upload a release .dmg to Vercel Blob, then register metadata via the API.
#
# Usage:
#   ./scripts/upload-release.sh VERSION [NOTES]
#
# Requires env vars (or source from ../../.env):
#   BLOB_READ_WRITE_TOKEN  — Vercel Blob write token
#   UPLOAD_SECRET           — API auth secret

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ -f "${SCRIPT_DIR}/../../.env" ]]; then
  set -a
  source "${SCRIPT_DIR}/../../.env"
  set +a
fi

VERSION="${1:?Usage: upload-release.sh VERSION [NOTES]}"
NOTES="${2:-}"
TARGET="darwin-aarch64"
DMG="../desktop/src-tauri/target/release/bundle/dmg/Burnrate_${VERSION}_aarch64.dmg"
SITE="${SITE_URL:-https://www.burnthemtokens.com}"
SECRET="${UPLOAD_SECRET:?Set UPLOAD_SECRET env var}"
BLOB_TOKEN="${BLOB_READ_WRITE_TOKEN:?Set BLOB_READ_WRITE_TOKEN env var}"

if [[ ! -f "$DMG" ]]; then
  echo "ERROR: $DMG not found — build the desktop app first"
  exit 1
fi

DMG_SIZE=$(wc -c < "$DMG" | tr -d ' ')
DMG_HUMAN=$(du -h "$DMG" | cut -f1 | tr -d ' ')
PATHNAME="releases/Burnrate_${VERSION}_${TARGET}.dmg"

echo "       file: ${DMG}"
echo "       size: ${DMG_HUMAN} (${DMG_SIZE} bytes)"
echo "       dest: ${PATHNAME}"
echo ""

echo "       uploading to blob.vercel-storage.com..."
BLOB_RESPONSE=$(curl -sS -X PUT "https://blob.vercel-storage.com/${PATHNAME}" \
  -H "Authorization: Bearer ${BLOB_TOKEN}" \
  -H "x-api-version: 7" \
  -H "x-content-type: application/octet-stream" \
  -H "x-add-random-suffix: 0" \
  -H "x-cache-control-max-age: 31536000" \
  --data-binary "@${DMG}")

BLOB_URL=$(echo "$BLOB_RESPONSE" | python3 -c "import sys,json; print(json.load(sys.stdin)['url'])" 2>/dev/null || true)

if [[ -z "$BLOB_URL" ]]; then
  echo "ERROR: Blob upload failed"
  echo "       response: $BLOB_RESPONSE"
  exit 1
fi

echo "       blob url: ${BLOB_URL}"
echo ""
echo "       registering metadata at ${SITE}/api/releases/upload..."

META_RESPONSE=$(curl -sS -w "\n%{http_code}" -X POST "${SITE}/api/releases/upload" \
  -H "Authorization: Bearer ${SECRET}" \
  -H "Content-Type: application/json" \
  -d "{\"version\":\"${VERSION}\",\"url\":\"${BLOB_URL}\",\"target\":\"${TARGET}\",\"notes\":\"${NOTES}\",\"size\":${DMG_SIZE}}")

HTTP_CODE=$(echo "$META_RESPONSE" | tail -1)
META_BODY=$(echo "$META_RESPONSE" | sed '$d')

if [[ "$HTTP_CODE" -lt 200 || "$HTTP_CODE" -ge 300 ]]; then
  echo "ERROR: Metadata registration failed (HTTP ${HTTP_CODE})"
  echo "       response: ${META_BODY}"
  exit 1
fi

echo "       metadata registered (HTTP ${HTTP_CODE})"
echo "$META_BODY" | python3 -m json.tool | sed 's/^/       /'

# Read the release back through the public endpoint the download button uses.
# Registering used to be assumed to have worked, and for several releases it
# silently hadn't — the site kept serving the first version ever published. The
# only trustworthy confirmation is asking the site what it will hand out now.
echo ""
echo "       verifying ${SITE}/api/releases/latest..."

LIVE=$(curl -sS -H "Accept: application/json" "${SITE}/api/releases/latest")
LIVE_VERSION=$(echo "$LIVE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('version',''))" 2>/dev/null || true)
LIVE_URL=$(echo "$LIVE" | python3 -c "import sys,json; print(json.load(sys.stdin).get('url',''))" 2>/dev/null || true)

if [[ "$LIVE_VERSION" != "$VERSION" ]]; then
  echo "ERROR: site still serves version '${LIVE_VERSION}', expected '${VERSION}'"
  echo "       response: $LIVE"
  exit 1
fi

DMG_CODE=$(curl -sS -o /dev/null -w "%{http_code}" -I "$LIVE_URL")
if [[ "$DMG_CODE" != "200" ]]; then
  echo "ERROR: published .dmg is not downloadable (HTTP ${DMG_CODE})"
  echo "       url: $LIVE_URL"
  exit 1
fi

echo "       verified: v${VERSION} is live and downloadable"
