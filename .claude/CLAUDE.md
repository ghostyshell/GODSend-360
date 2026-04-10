# GODsend-360 — Claude Code guidance

The primary reference for this project is [`AGENTS.md`](AGENTS.md). Read it before making any changes. It covers:

- Project overview and component boundaries (Go backend, Electron app, Aurora Lua scripts)
- Repository layout and architectural patterns
- Build, run, and test commands
- Code style and design guidelines
- Rules for keeping `AGENTS.md` itself up to date

Pure-Go ISO→GOD and archive logic lives in **`src/server/utils/`** (`package utils`). Optional third-party binaries such as 7-Zip still belong in gitignored **`tools/`** at the repo root when needed; see `AGENTS.md`.

## Additional references

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — version-bump checklist, changelog rules, PR conventions, and per-component change guidelines. Follow this when making any non-trivial change.
- [`CHANGELOG.md`](CHANGELOG.md) — record of changes per release. Add an entry under `[Unreleased]` for every notable change.
- [`docs/reference/aurora.md`](docs/reference/aurora.md) — Aurora Lua host API reference. Required reading before editing `aurora-scripts/`.
- [`docs/reference/multi-disc-compatibility.md`](docs/reference/multi-disc-compatibility.md) — multi-disc game compat table used by the disc-picker feature.
- [`README.md`](README.md) — user-facing documentation; keep in sync with any API or UX changes.
