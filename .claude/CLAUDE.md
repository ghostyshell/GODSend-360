# GODsend-360 — Claude Code guidance

The primary reference for this project is [`AGENTS.md`](AGENTS.md). Read it before making any changes. It covers:

- Project overview and component boundaries (Go backend, Electron app, Aurora Lua scripts)
- Repository layout and architectural patterns
- Run and test commands (builds are handled by `godsend-release-keeper`)
- Code style and design guidelines
- Rules for keeping `AGENTS.md` itself up to date

Pure-Go ISO→GOD and archive logic lives in **`src/server/utils/`** (`package utils`). Optional third-party binaries such as 7-Zip still belong in gitignored **`tools/`** at the repo root when needed; see `AGENTS.md`.

## Agent rules

- **Always version bump** — every non-trivial change must update the version string in all four places (`package.json`, `src/electron-app/package.json`, `src/server/main.go`, `aurora-scripts/main.lua`) and add a `CHANGELOG.md` entry under `[Unreleased]`. Do not commit without this.
- **Always run the doc-sync checklist** — for any change to HTTP routes, IPC channels, services, infrastructure files, runtime folders, env vars, or user-facing features, follow [`docs/agents/skills/doc-sync.md`](../docs/agents/skills/doc-sync.md). README/AGENTS/features.md/api-reference.md must be updated in the **same commit** as the code change, not a follow-up.

## Additional references

- [`CONTRIBUTING.md`](CONTRIBUTING.md) — version-bump checklist, changelog rules, PR conventions, and per-component change guidelines. Follow this when making any non-trivial change.
- [`CHANGELOG.md`](CHANGELOG.md) — record of changes per release. Add an entry under `[Unreleased]` for every notable change.
- [`docs/reference/aurora.md`](docs/reference/aurora.md) — Aurora Lua host API reference. Required reading before editing `aurora-scripts/`.
- [`docs/reference/multi-disc-compatibility.md`](docs/reference/multi-disc-compatibility.md) — multi-disc game compat table used by the disc-picker feature.
- [`README.md`](README.md) — user-facing documentation; keep in sync with any API or UX changes.
