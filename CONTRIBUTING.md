# Contributing to GODsend-360

## Before you start

Read [`AGENTS.md`](AGENTS.md) — it covers the architecture, module boundaries, build commands, and code-style rules for all three components (Go backend, Electron app, Aurora Lua scripts). Changes that violate those constraints will be asked to be revised.

---

## Development setup

**Requirements**: Go 1.21+, Node.js 18+. No external tool binaries needed.

```
git clone <repo>
cd godsend-360
npm install          # installs root + Electron devDependencies
```

Build and run:

```
npm run build        # compiles Go backend + Windows NSIS installer → dist/
npm start            # launches Electron app in dev mode (uses dist/godsend.exe)
```

Backend only (faster iteration):

```
go build -C src/server -o ../../dist/godsend.exe .
```

---

## Making changes

### All changes

1. Work on a feature branch off `main`.
2. Keep commits focused — one logical change per commit, written in the imperative (`fix:`, `feat:`, `chore:`, `docs:`).
3. **Update `CHANGELOG.md`** under the `[Unreleased]` section (create it if it doesn't exist at the top of the file) with a short bullet describing what changed and why. Use the existing categories: `Added`, `Fixed`, `Changed`, `Removed`.
4. If your change affects architecture, build commands, or code conventions, **update `AGENTS.md`** in the same PR.

### Go backend (`src/server/`)

- Run `gofmt` before committing.
- Keep handlers thin — parse input, call a service, return JSON. No business logic in handlers.
- New HTTP endpoints must be documented in the `README.md` API table and in `AGENTS.md` if they affect the Lua↔backend contract.

### Electron app (`src/electron-app/`)

- New IPC channels must be added to `preload.js` and documented in `AGENTS.md`.
- Settings that persist to `config.json` must go through `settingsService.js` getters/setters — do not read/write `config.json` directly from `bootstrap.js`.
- UI changes go in `index.html` + `renderer.js`. Keep `renderer.js` DOM-only (no Node/Electron imports).

### Aurora Lua scripts (`aurora-scripts/`)

- Test manually on-console before submitting — there is no automated test harness.
- Read `docs/aurora-reference.md` for supported APIs, path rules, and known crash patterns.
- The scripting host is Lua 5.1: no `goto`, no bitwise operators, no `table.move`, limited string library.
- Wrap all host API calls (`Http.*`, `FileSystem.*`, `ZipFile.*`, `Script.*`) in `pcall`.

---

## Version bumping

Versions follow [Semantic Versioning](https://semver.org/). When your changes are ready to release, update **all** of the following together:

| File | Field |
|---|---|
| `package.json` | `"version"` |
| `src/electron-app/package.json` | `"version"` |
| `src/server/main.go` | version string in the startup banner (`fmt.Println`) |
| `aurora-scripts/main.lua` | `scriptVersion` and the `Menu.SetTitle(...)` string |

Rules:
- **Patch** (`x.y.Z`) — bug fixes only, no new features or API changes.
- **Minor** (`x.Y.0`) — new features, backwards-compatible. Most PRs land here.
- **Major** (`X.0.0`) — breaking changes to the Aurora↔backend HTTP contract or the Electron IPC surface.

Move the `[Unreleased]` section in `CHANGELOG.md` to the new version number and date when cutting a release.

---

## Pull requests

- Target `main`.
- Title format: `<type>: <short description>` — e.g. `feat: configurable FTP destination path`.
- Include a short description of what changed and how to test it.
- Ensure `npm run build` succeeds without errors before marking ready for review.
