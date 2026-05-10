# Skill: Electron App — Architecture & Patterns

## Description
Governs conventions for the Electron desktop app (`src/electron-app/`). All Electron-related changes MUST follow this skill.

## Scope
- `src/electron-app/main.ts`, `app/`, `services/`, `ipc/`, `infrastructure/`
- `src/electron-app/renderer/` — React + Vite renderer
- `src/electron-app/preload.ts` — IPC surface

## Architecture
- `app/` — lifecycle, IPC registration, top-level composition
- `services/` — high-level behaviour, no direct Electron window creation
- `infrastructure/` — filesystem and OS-specific helpers, no business logic
- `preload.ts` — IPC surface only; no business logic
- `ipc/` — IPC handler modules (one file per domain)

## Key Conventions
- TypeScript compiled in-place (`tsconfig.json`: `module: commonjs`, no `outDir`)
- `build.clean:compiled-js` removes `.js` artefacts after packaging
- `renderer-dist/` is the production bundle (built by Vite)
- All FTP operations now flow through Go backend (`backendPost("/ftp/batch")`)

## See Also
- `docs-source-of-truth.md` — version bump & release workflow
- `playwright-testing.md` — UI recording & test conventions
- Go backend skill for HTTP endpoint contracts
