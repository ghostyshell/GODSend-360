#!/bin/sh
# Rebuild the site's static Tailwind stylesheet after editing docs/index.html.
# Pages serves docs/site/tailwind.css directly, so commit the output with the
# markup change.
set -e
cd "$(dirname "$0")/.."
TW=${TW:-"$HOME/Code/stream-frontend/node_modules/.bin/tailwindcss"}
exec "$TW" -c docs/site/tailwind.config.cjs -i docs/site/tailwind.src.css -o docs/site/tailwind.css --minify
