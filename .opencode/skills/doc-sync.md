> **SKILL SHIM** — This file is a pointer only. The canonical source of truth lives at:
> `docs/agents/skills/doc-sync.md`

# Skill: Doc Sync (Shim)

## Quick Reference
Invoke whenever a change adds/removes/renames an HTTP route, IPC channel, service, infrastructure adapter, runtime folder, env var, npm script, or user-facing feature. Updates `README.md`, `AGENTS.md`, `CHANGELOG.md`, `docs/features.md`, and `docs/api-reference.md` in the **same commit** as the code change.

## Trigger checklist (short form)
- New route in `src/server/interfaces/http/router.go` → `docs/api-reference.md` + `readme.md` endpoints
- New IPC in `src/electron-app/preload.ts` → `AGENTS.md` IPC list
- New service / infrastructure file → `AGENTS.md` services list
- New user-facing feature → `docs/features.md` section + `readme.md` blurb
- Any non-trivial change → `CHANGELOG.md` `[Unreleased]` bullet

## How to Use
Load the canonical skill for the full table, templates, and anti-patterns:
```
docs/agents/skills/doc-sync.md
```

---
*This shim exists so that agent-specific directories (`.claude`, `.opencode`, `.cursor`) stay in sync. The canonical file is under `docs/agents/skills/`.*
