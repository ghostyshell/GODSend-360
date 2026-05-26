# Skill: Docs — Source of Truth

## Description
This skill is the **source of truth** for all documentation-related changes in the GODsend-360 project. It defines conventions, patterns, and requirements for maintaining `AGENTS.md`, `README.md`, `CHANGELOG.md`, and any files under `docs/`.

## Purpose
When any agent modifies, adds, or removes documentation, it MUST consult this skill first. All other domain-specific skills (Electron, Go, Aurora) reference this skill for doc-related decisions.

## Scope
- `AGENTS.md` — living architecture & workflow docs for agents
- `README.md` — user-facing install, setup, and usage docs
- `CHANGELOG.md` — versioned release history
- `docs/**/*.md` — extended guides (headless-setup, API reference, etc.)
- `CONTRIBUTING.md` — contribution rules

## Conventions

### Version Bump Workflow
1. Update version in **all four places**:
   - `package.json` (root)
   - `src/electron-app/package.json`
   - `src/server/main.go` (banner line)
   - `aurora-scripts/main.lua` (`scriptVersion`)
2. Update `CHANGELOG.md`: move `[Unreleased]` to new version heading
3. Update `README.md`: replace all inline version references + GoFile links
4. Build all targets: `npm run build`
5. Upload each `dist/` artifact individually to GoFile (no `folderId`)
6. Replace every GoFile URL in README with fresh per-file links
7. Commit and push to **github** remote (`git push github HEAD`)

### Skill Reference Pattern
When adding new skills:
1. Create the skill file under `docs/agents/skills/<name>.md`
2. Include YAML frontmatter with `description`, `scope`, and `source_of_truth` fields
3. If this skill governs a subdomain, it should contain a `seealso` block linking to the docs skill
4. Update this file's `related_skills` list

## Related Skills
- `docs/agents/skills/doc-sync.md` — **auto-sync trigger checklist** for keeping README/AGENTS/CHANGELOG/features.md/api-reference.md in lockstep with code changes
- `docs/agents/skills/playwright-testing.md` — video recording & UI test conventions (results saved to ignored `test-results/` dir)
- `docs/agents/skills/shim-electron.md` — Electron agent conventions
- `docs/agents/skills/shim-go-backend.md` — Go backend agent conventions
- `docs/agents/skills/shim-aurora-scripts.md` — Aurora Lua agent conventions

## Quick Commands
| Task | Command |
|---|---|
| Build all | `npm run build` |
| Electron dev | `npm start --prefix src/electron-app` |
| Run Playwright | `npx playwright test --prefix src/electron-app` |
| Go backend only | `go build -C src/server -o ../../dist/godsend-mac .` |
