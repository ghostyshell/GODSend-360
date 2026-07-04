#!/bin/sh
# Point git at the repo's version-controlled .githooks/ dir.
# Run once after cloning: sh scripts/install-hooks.sh
cd "$(dirname "$0")/.." || exit 1
git config core.hooksPath .githooks
chmod +x .githooks/* 2>/dev/null
echo "hooks installed: core.hooksPath = .githooks"
