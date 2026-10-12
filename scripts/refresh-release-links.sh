#!/bin/bash
# Weekly GODsend-360 release refresh.
#
# Rebuilds the version currently in package.json, re-uploads all 12 release
# artifacts to GoFile (primary) and file.kiwi (backup), repoints the download
# links in readme.md + docs/headless-setup.md, and pushes the result.
#
# Why this exists: GoFile expires free-tier files, so the primary download
# links rot within weeks even when the code has not changed.
#
# Why dist/ is cleared first: a failed build must not be able to fall through
# to uploading last week's binaries. Empty dist/ means the upload stage has
# nothing to send, and the build step below aborts before it gets there.
#
# Run by ~/Library/LaunchAgents/com.akshat.godsend-weekly-refresh.plist
# (Sunday 03:47, under `caffeinate -i`). Safe to run by hand: re-running the
# same day resumes that day's uploads instead of starting over.
set -euo pipefail

REPO=/Users/akshat/Code/godsend-360
cd "$REPO"

V=$(node -p "require('./package.json').version")
EV=$(node -p "require('./src/electron-app/package.json').version")

log() { printf '[%s] %s\n' "$(date '+%F %T')" "$*"; }
notify() { osascript -e "display notification \"$1\" with title \"GODsend release refresh\"" >/dev/null 2>&1 || true; }
die() { log "FAILED: $*"; notify "GODsend link refresh FAILED - $*"; exit 1; }

# Manifest bookkeeping: one row per artifact, `name | gofile | kiwi`. Reads are
# keyed on the exact name so `godsend.exe` cannot match `godsend-Setup-X.exe`.
field() { grep -F "$1 |" "$LINKS" 2>/dev/null | tail -1 | cut -d'|' -f"$2" | tr -d ' ' || true; }
row() {
  grep -v -F "$1 |" "$LINKS" > "$LINKS.tmp" 2>/dev/null || true
  mv "$LINKS.tmp" "$LINKS"
  printf '%s | %s | %s\n' "$1" "$2" "$3" >> "$LINKS"
}

if [ "${1:-}" = "--selftest" ]; then
  LINKS=$(mktemp)
  touch "$LINKS"
  row godsend.exe https://gofile.io/d/A https://file.kiwi/a
  row godsend-Setup-9.9.9.exe EMPTY https://file.kiwi/b
  row godsend.exe https://gofile.io/d/A2 https://file.kiwi/a2
  [ "$(field godsend.exe 2)" = "https://gofile.io/d/A2" ] || die "selftest: gofile field wrong"
  [ "$(field godsend.exe 3)" = "https://file.kiwi/a2" ] || die "selftest: kiwi field wrong"
  [ "$(field godsend-Setup-9.9.9.exe 2)" = "EMPTY" ] || die "selftest: EMPTY row not read back"
  [ "$(wc -l < "$LINKS" | tr -d ' ')" = "2" ] || die "selftest: row() appended a duplicate instead of replacing"
  rm -f "$LINKS"
  echo "selftest OK"
  exit 0
fi

[ "$V" = "$EV" ] || die "version drift: package.json=$V but src/electron-app/package.json=$EV"

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

# --- 3. Upload -------------------------------------------------------------------
TOKEN=$(cat .gofile-io-token)
touch "$LINKS"

# GoFile needs the long timeout: a 130-160MB artifact runs ~20min, and the old
# --max-time 600 truncated those into silent EMPTY rows. Non-JSON bodies also
# happen on large files, hence the retries.
put_gofile() {
  local f=$1 url=""
  for attempt in 1 2 3; do
    url=$(curl -sS --max-time 3600 -X POST "https://upload.gofile.io/contents/uploadfile" \
      -H "Authorization: Bearer $TOKEN" -F "file=@dist/$f" 2>/dev/null \
      | jq -r '.data.downloadPage // empty' 2>/dev/null) || url=""
    if [ -n "$url" ]; then
      printf '%s\n' "$url"
      return 0
    fi
    log "  gofile attempt $attempt/3 failed for $f; retrying in 30s"
    sleep 30
  done
  return 1
}

put_kiwi() {
  FORCE_COLOR=0 npx -y @file-kiwi/node "dist/$1" --title "$1" 2>&1 \
    | grep -Eo 'https://file\.kiwi/[A-Za-z0-9][A-Za-z0-9#_+/=-]*' | tail -1 || true
}

incomplete=0
for f in "${FILES[@]}"; do
  gf=$(field "$f" 2)
  kw=$(field "$f" 3)
  if [ -n "$gf" ] && [ "$gf" != "EMPTY" ] && [ -n "$kw" ] && [ "$kw" != "EMPTY" ]; then
    log "[skip] $f already uploaded today"
    continue
  fi
  log "[start] $f ($(du -h "dist/$f" | cut -f1))"
  if [ -z "$gf" ] || [ "$gf" = "EMPTY" ]; then
    gf=$(put_gofile "$f") || gf="EMPTY"
  fi
  if [ -z "$kw" ] || [ "$kw" = "EMPTY" ]; then
    kw=$(put_kiwi "$f")
  fi
  [ -n "$gf" ] && [ "$gf" != "EMPTY" ] || incomplete=1
  log "[done] $f | ${gf:-EMPTY} | ${kw:-EMPTY}"
  row "$f" "${gf:-EMPTY}" "${kw:-EMPTY}"
done

# --- 4. Repoint the download links -----------------------------------------------
python3 -I scripts/patch-release-links.py "$LINKS" readme.md docs/headless-setup.md

# --- 5. Commit + push ------------------------------------------------------------
if git diff --quiet -- readme.md docs/headless-setup.md; then
  log "download links already current - nothing to commit"
else
  git commit -q -m "docs: refresh ${V} download links (GoFile + file.kiwi)

Automated weekly refresh: all artifacts rebuilt and re-uploaded to GoFile
(primary) and file.kiwi (backup). GoFile expires free-tier files, so the
previous links had gone stale.

Co-Authored-By: Claude Code <noreply@anthropic.com>" -- readme.md docs/headless-setup.md
  git push -q github main || die "git push failed - links are committed locally but not published"
  log "pushed refreshed ${V} links"
fi

if [ "$incomplete" -ne 0 ]; then
  log "WARNING: at least one artifact got no fresh GoFile link - dist/ left in place, re-run to fill"
  notify "GODsend ${V} refresh finished with gaps - re-run to fill"
else
  notify "GODsend ${V} download links refreshed"
fi
log "done"
