> **SKILL SHIM** — This file is a pointer only. The canonical source of truth lives at:
> `docs/agents/skills/docs-source-of-truth.md`

# Skill: Docs — Source of Truth (Shim)

## Quick Reference
This skill governs all documentation conventions for GODsend-360:
- `AGENTS.md`, `README.md`, `CHANGELOG.md`, `docs/**/*.md`
- Version bump workflow (4 files + build + GoFile + commit)
- Skill addition pattern (add to `docs/agents/skills/`, update index)

## How to Use
When asked to update docs, add skills, or cut a release, load the canonical skill:
```
docs/agents/skills/docs-source-of-truth.md
```

## Local Commands
| Task | Command |
|---|---|
| Build all | `npm run build` |
| Upload to GoFile | `curl -s -X POST "https://upload.gofile.io/uploadfile" -F "file=@dist/FILENAME"` |
| Push release | `git push github HEAD` |

## See Also
- `docs/agents/skills/playwright-testing.md` — UI recording conventions
- `docs/agents/skills/shim-electron.md` — Electron app patterns
- `docs/agents/skills/shim-go-backend.md` — Go backend patterns
- `docs/agents/skills/shim-aurora-scripts.md` — Aurora Lua patterns

---
*This shim exists so that agent-specific directories (`.claude`, `.opencode`, `.cursor`) stay in sync. The canonical file is under `docs/agents/skills/`.*
