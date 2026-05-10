# Skill: Go Backend — Architecture & Patterns

## Description
Governs conventions for the Go backend (`src/server/`). All Go-related changes MUST follow this skill.

## Scope
- `src/server/main.go` — entry point (wiring only, ~180 lines)
- `src/server/models/` — pure domain types (no dependencies)
- `src/server/app/` — central `App` struct and configuration
- `src/server/infrastructure/` — side-effect adapters (filesystem, network, external processes)
- `src/server/services/` — application-layer logic
- `src/server/interfaces/http/` — HTTP delivery layer
- `src/server/utils/` — pure Go utilities (RXEA codec, ISO→GOD)

## Dependency Flow (no import cycles)
```
models → (nothing)
app → models
infrastructure/* → app, models
services/* → app, models, infrastructure/*
interfaces/http → app, models, services/*, infrastructure/*
main → everything (wiring only)
```

## Key Conventions
- Keep `main.go` thin: wiring only
- Handlers thin: parse input, delegate to services, translate to JSON
- No global mutable state — use `*app.App` injected via constructors
- All HTTP endpoints and IPC channel names are stable contracts

## RXEA Codec
- `src/server/utils/rxea.go` — encode/decode Aurora `.asset` files
- `POST /rxea/encode?slot=N` — single slot encode
- `POST /rxea/encode-multi` — multi-slot encode (preserves sibling slots)
- `POST /rxea/decode` — decode all slots from `.asset`

## See Also
- `docs-source-of-truth.md` — version bump & release workflow
- `shim-electron.md` — Electron-side HTTP client patterns
