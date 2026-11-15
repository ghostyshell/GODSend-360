#!/bin/sh
# Rebuild the site's static Tailwind stylesheet after editing docs/index.html.
# Pages serves docs/site/tailwind.css directly, so commit the output with the
# markup change.
set -e
cd "$(dirname "$0")/.."

# Pin the compiler. A sibling checkout's tailwindcss is used when present
# because it is already installed; otherwise fetch the same pinned version.
VERSION=3.4.17
TW=${TW:-"$HOME/Code/stream-frontend/node_modules/.bin/tailwindcss"}

run() {
  "$@" -c docs/site/tailwind.config.cjs -i docs/site/tailwind.src.css -o docs/site/tailwind.css --minify
}

if [ -x "$TW" ]; then
  run "$TW"
else
  run npx --yes "tailwindcss@$VERSION"
fi
