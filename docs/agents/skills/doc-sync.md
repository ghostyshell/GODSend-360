# Skill: Doc Sync (auto-sync docs with code changes)

## Description
This skill is invoked **every time an agent adds, removes, or modifies user-visible behaviour** in the GODsend-360 codebase. It guarantees that `README.md`, `AGENTS.md`, `CHANGELOG.md`, `docs/features.md`, and `docs/api-reference.md` stay in lockstep with the code, instead of trailing several versions behind.

It supplements (does not replace) [`docs-source-of-truth`](docs-source-of-truth.md) — that skill defines the canonical conventions; this skill defines the **trigger checklist** that agents run while a change is still in flight.

## Trigger

Invoke this skill whenever a change touches **any** of:

| If the change adds / removes / renames… | Then update… |
|---|---|
| HTTP route in `src/server/interfaces/http/router.go` | `docs/api-reference.md` (endpoint table), `readme.md` "Key endpoints" blurb |
| IPC channel exposed in `src/electron-app/preload.ts` | `AGENTS.md` "IPC handlers" list |
| Service in `src/server/services/*` or `src/electron-app/services/*` | `AGENTS.md` services list |
| `infrastructure/*` adapter | `AGENTS.md` infrastructure list |
| New runtime folder / env var | `docs/api-reference.md` runtime folders + env table |
| New user-facing feature (UI page, Toolbox entry, settings option) | `docs/features.md` (add a section with **What / How / How it works**) and `readme.md` feature blurb |
| New top-level npm script or build artifact | `AGENTS.md` build tooling, `docs/building.md` if it exists |
| New Aurora script module or Lua API consumer | `AGENTS.md` Aurora scripts section, `docs/reference/aurora.md` if API surface changes |
| Bug fix, perf change, or breaking refactor | `CHANGELOG.md` under `[Unreleased]` with the right category |
| Version bump | All four version files + `readme.md` filenames + new `CHANGELOG.md` heading |

## Workflow (per change)

1. **Before committing**, run `git diff --stat` and check each modified path against the table above.
2. For every row that matches, open the corresponding doc and either:
   - Add the new entry (endpoint row, service bullet, feature section), or
   - Update the existing entry to reflect the new behaviour, or
   - Remove a stale entry if functionality was deleted.
3. Add a `CHANGELOG.md` bullet under `[Unreleased]` describing **why** (not what — the diff has the what). Categories: `Added`, `Changed`, `Fixed`, `Removed`.
4. If user-facing behaviour changed, audit `readme.md`:
   - "Features" paragraph (line ~120)
   - "Key endpoints" blurb (line ~245)
   - Any specific section that names the changed feature
5. Stage doc files **in the same commit** as the code change. Do not split docs into a follow-up commit unless the user explicitly asks for it.

## Patterns to follow

### `docs/features.md` — feature section template
```markdown
## <Feature name>

- **What:** <one-sentence pitch>
- **How:** <click-path the user takes>
- **How it works:** <implementation: services, endpoints, files involved>
```

### `docs/api-reference.md` — endpoint row
```
| METHOD | `/path` | One-line purpose; mention required params and side effects |
```

### `AGENTS.md` — service / handler bullet
```
  - `path/to/file.ts`: <one-line purpose> — <key entry points or exports>
```

### `CHANGELOG.md` — entry
```markdown
### Added | Changed | Fixed | Removed
- **<headline>** — <why this matters / what was wrong before>. <Optional implementation note: file, function, mechanism>.
```

## Anti-patterns

- ❌ "I'll update docs in a follow-up PR" — docs drift starts here.
- ❌ Marketing prose in `docs/api-reference.md` — keep it factual.
- ❌ Re-stating the diff in `CHANGELOG.md` — explain the motivation or the bug, not the lines changed.
- ❌ Duplicating the same content across `readme.md`, `features.md`, and `AGENTS.md` — `readme.md` summarises, `features.md` explains, `AGENTS.md` indexes code.
- ❌ Updating only one doc when several apply — every row in the table above is a separate edit.

## Quick self-check before commit

```bash
git diff --stat
# For each src/ path in the diff:
#   - Did I open at least one doc file under docs/ or README/AGENTS?
#   - Is there a CHANGELOG bullet under [Unreleased]?
# If both answers are "no" and the change is non-trivial: STOP. Update docs first.
```

## Related Skills
- [`docs-source-of-truth`](docs-source-of-truth.md) — conventions for the doc files themselves.
- [`shim-electron`](shim-electron.md), [`shim-go-backend`](shim-go-backend.md), [`shim-aurora-scripts`](shim-aurora-scripts.md) — per-component agent guidance; each triggers this skill when code changes land.
