#!/bin/bash
# Weekly GODsend-360 release refresh.
#
# Rebuilds the version currently in package.json, uploads all 12 release
# artifacts to Cloudflare R2, repoints the download links in readme.md +
# docs/headless-setup.md + docs/index.html (the site's platform cards), pushes,
# and deletes the older R2 versions and the local dist/.
#
# R2 keys are versioned and immutable: a link stays valid until the next
# version bump, so nothing needs a timed refresh any more. The launchd job
# remains as a cheap version check.
#
# Why dist/ is cleared first: a failed build must not be able to fall through
# to uploading last week's binaries. Empty dist/ means the upload stage has
# nothing to send, and the build step below aborts before it gets there.
#
# Run by ~/Library/LaunchAgents/com.ghost.godsend-weekly-refresh.plist
# (Sunday 03:47, under `caffeinate -i`). Safe to run by hand: re-running the
# same day resumes that day's uploads instead of starting over.
set -euo pipefail

REPO="$HOME/Code/godsend-360"
cd "$REPO"

V=$(node -p "require('./package.json').version")
EV=$(node -p "require('./src/electron-app/package.json').version")

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*"; }
notify() { osascript -e "display notification \"$1\" with title \"GODsend release refresh\"" >/dev/null 2>&1 || true; }
die() { log "FAILED: $*"; notify "GODsend link refresh FAILED - $*"; exit 1; }

# Fail fast on a missing tool rather than deep inside the build. launchd gets a much
# thinner PATH than an interactive shell: `go` lives in /usr/local/go/bin, which no
# shell profile on this machine exports, so the plist has to list it.
for tool in node npm go python3 jq curl git; do
  command -v "$tool" >/dev/null 2>&1 || die "$tool not found on PATH ($PATH)"
done

# Manifest bookkeeping: one row per artifact, `name | url`. Reads are keyed on
# the exact name so `godsend.exe` cannot match `godsend-Setup-X.exe`.
field() { grep -F "$1 |" "$LINKS" 2>/dev/null | tail -1 | cut -d'|' -f"$2" | tr -d ' ' || true; }
row() {
  grep -v -F "$1 |" "$LINKS" > "$LINKS.tmp" 2>/dev/null || true
  mv "$LINKS.tmp" "$LINKS"
  printf '%s | %s\n' "$1" "$2" >> "$LINKS"
}

if [ "${1:-}" = "--selftest" ]; then
  LINKS=$(mktemp)
  touch "$LINKS"
  row godsend.exe https://covers.debrid.in/godsend-360/releases/9.9.9/godsend.exe
  row godsend-Setup-9.9.9.exe https://covers.debrid.in/godsend-360/releases/9.9.9/godsend-Setup-9.9.9.exe
  row godsend.exe https://covers.debrid.in/godsend-360/releases/9.9.9/godsend.exe
  [ "$(field godsend.exe 2)" = "https://covers.debrid.in/godsend-360/releases/9.9.9/godsend.exe" ] || die "selftest: url field wrong"
  [ "$(field godsend-Setup-9.9.9.exe 2)" = "https://covers.debrid.in/godsend-360/releases/9.9.9/godsend-Setup-9.9.9.exe" ] || die "selftest: second row wrong"
  [ "$(wc -l < "$LINKS" | tr -d ' ')" = "2" ] || die "selftest: row() appended a duplicate instead of replacing"
  rm -f "$LINKS"
  echo "selftest OK"
  exit 0
fi

[ "$V" = "$EV" ] || die "version drift: package.json=$V but src/electron-app/package.json=$EV"

# R2 links never expire, so there is nothing to renew on a timer. Publish only
# when the repo version is ahead of the live feed; FORCE=1 overrides.
LIVE=$(curl -fsS "https://ghostyshell.github.io/GODSend-360/downloads.json?v=$(date +%s)" 2>/dev/null \
  | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("version",""))
except Exception: print("")' 2>/dev/null || true)
if [ "${FORCE:-0}" != 1 ] && [ "$LIVE" = "$V" ]; then
  log "$V is already live; nothing to publish"
  exit 0
fi

RUN=".release-${V}-$(date +%Y%m%d)"
LINKS="$RUN/links.txt"
mkdir -p "$RUN"

# One run at a time: a slept-through slot can hand us two starts on the same day.
LOCK=.release-refresh.lock
if ! mkdir "$LOCK" 2>/dev/null; then
  pid=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
    log "another refresh is already running (pid $pid) - exiting"
    exit 0
  fi
  log "clearing stale lock (pid ${pid:-unknown} is not running)"
  rm -rf "$LOCK"
  mkdir "$LOCK"
fi
printf '%s\n' "$$" > "$LOCK/pid"
trap 'rm -rf "$LOCK"' EXIT

log "refreshing GODsend-360 $V into $RUN"

# --- 1. Clear old local artifacts -------------------------------------------------
log "clearing dist/ so a failed build cannot upload stale binaries"
rm -rf dist
# Per-release scratch dirs are gitignored text, not disk pressure: keep a month.
find . -maxdepth 1 -name '.release-*' -type d -mtime +30 -exec rm -rf {} + 2>/dev/null || true

# --- 2. Build --------------------------------------------------------------------
log "building all platforms (npm run build)"
npm run build >"$RUN/build.log" 2>&1 || die "npm run build failed - see $RUN/build.log"

FILES=(
  godsend.exe
  godsend-darwin-arm64
  godsend-darwin-amd64
  godsend-mac
  godsend-linux-x64
  godsend-linux-arm64
  "godsend-Setup-${V}.exe"
  "godsend-Portable-${V}.exe"
  "godsend-${V}-arm64.dmg"
  "godsend-${V}-x64.dmg"
  "godsend-${V}-arm64.AppImage"
  "godsend-${V}-x86_64.AppImage"
)
for f in "${FILES[@]}"; do
  [ -f "dist/$f" ] || die "build did not produce dist/$f"
done
log "all 12 artifacts built"

# --- 3. Upload to Cloudflare R2 --------------------------------------------------
R2_ENV=${R2_ENV:-$HOME/.r2.env}
R2_BUCKET=${R2_BUCKET:-torrent-search-covers-eu}
R2_PREFIX=${R2_PREFIX:-godsend-360/releases}
R2_PUBLIC=${R2_PUBLIC:-https://covers.debrid.in}
[ -f "$R2_ENV" ] || die "missing $R2_ENV (R2 S3 keys)"
set -a; . "$R2_ENV"; set +a
for v in R2_S3_ENDPOINT R2_S3_ACCESS_KEY_ID R2_S3_SECRET_ACCESS_KEY; do
  eval "val=\${$v:-}"
  [ -n "$val" ] || die "$v missing from $R2_ENV"
done
command -v aws >/dev/null 2>&1 || die "aws CLI not found on PATH ($PATH)"
# R2 rejects the extra CRC headers modern aws-cli injects on some builds.
export AWS_REQUEST_CHECKSUM_CALCULATION=when_required AWS_RESPONSE_CHECKSUM_VALIDATION=when_required

r2_aws() {
  AWS_ACCESS_KEY_ID="$R2_S3_ACCESS_KEY_ID" AWS_SECRET_ACCESS_KEY="$R2_S3_SECRET_ACCESS_KEY" \
  AWS_DEFAULT_REGION=auto AWS_EC2_METADATA_DISABLED=true \
    aws "$@" --endpoint-url "$R2_S3_ENDPOINT"
}

r2_put() {  # $1=local file -> prints the public URL
  local key="$R2_PREFIX/$V/$(basename "$1")"
  # stderr: the URL is captured with $(...), a log line on stdout would be
  # captured with it and corrupt the manifest row.
  log "  R2 -> s3://$R2_BUCKET/$key" >&2
  r2_aws s3 cp "$1" "s3://$R2_BUCKET/$key" \
    --content-type application/octet-stream \
    --cache-control 'public, max-age=31536000, immutable' \
    --only-show-errors || return 1
  printf '%s\n' "$R2_PUBLIC/$key"
}

r2_prune() {  # keep the newest $1 versions under the prefix (default 2)
  local keep=${1:-2} versions total
  versions=$(r2_aws s3api list-objects-v2 --bucket "$R2_BUCKET" \
      --prefix "$R2_PREFIX/" --delimiter / \
      --query 'CommonPrefixes[].Prefix' --output text 2>/dev/null \
    | tr '\t' '\n' | sed 's#/$##' | awk -F/ '{print $NF}' \
    | grep -E '^[0-9]+\.[0-9]+\.[0-9]+$' | sort -t. -k1,1n -k2,2n -k3,3n) || true
  total=$(printf '%s\n' "$versions" | grep -c . || true)
  [ "${total:-0}" -gt "$keep" ] || return 0
  printf '%s\n' "$versions" | sed -n "1,$((total-keep))p" | while IFS= read -r old; do
    log "R2: pruning $R2_PREFIX/$old/"
    r2_aws s3 rm "s3://$R2_BUCKET/$R2_PREFIX/$old/" --recursive --only-show-errors \
      || log "WARNING: R2 prune failed for $old (old objects left in place)"
  done
}

: > "$LINKS"
for f in "${FILES[@]}"; do
  log "[upload] $f ($(du -h "dist/$f" | cut -f1))"
  url=$(r2_put "dist/$f") || die "R2 upload failed for $f"
  row "$f" "$url"
done

# --- 4. Repoint the download links -----------------------------------------------
python3 -I scripts/patch-release-links.py "$LINKS" readme.md docs/headless-setup.md docs/index.html

# --- 5. Commit + push ------------------------------------------------------------
if git diff --quiet -- readme.md docs/headless-setup.md docs/index.html; then
  log "download links already current - nothing to commit"
else
  git commit -q -m "docs: publish ${V} downloads to Cloudflare R2

One host, versioned keys: the links stay valid until the next version, so no
weekly mirror refresh is needed any more.

Co-Authored-By: Claude Code <noreply@anthropic.com>" -- readme.md docs/headless-setup.md docs/index.html
  git push -q github main || die "git push failed - links are committed locally but not published"
  log "pushed refreshed ${V} links"
fi

log "pruning old R2 versions (keeping the current and previous)"
r2_prune 2

log "removing local build artifacts"
rm -rf dist
notify "GODsend ${V} downloads published"
log "done"
