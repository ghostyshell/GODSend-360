# Skill: Aurora Scripts — Lua Conventions

## Description
Governs conventions for the Aurora Lua scripts (`aurora-scripts/`). All Lua-related changes MUST follow this skill.

## Scope
- `aurora-scripts/main.lua` — entry point, orchestrator
- `aurora-scripts/state.lua` — connection settings, mutable state
- `aurora-scripts/http_client.lua` — HTTP helpers, error catalogue
- `aurora-scripts/services.lua` — backend-facing operations (trigger, install, wait)
- `aurora-scripts/menu.lua` — in-Aurora UI

## Key Conventions
- Treat each file as a module with globals shared intentionally
- Cross-module state lives in `state.lua`
- I/O-heavy functions live in `http_client.lua` and `services.lua`
- Menu logic lives in `menu.lua`, delegates to services (no duplicated HTTP)
- Defensive parsing for HTTP responses (`jsonField`, `validateResponse`)
- Use `pcall` around operations that can throw from the host

## See Also
- `docs-source-of-truth.md` — version bump workflow (includes `scriptVersion`)
- `docs/reference/aurora.md` — supported Lua APIs, path rules, known limits
