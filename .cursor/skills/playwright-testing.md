> **SKILL SHIM** — This file is a pointer only. The canonical source of truth lives at:
> `docs/agents/skills/playwright-testing.md`

# Skill: Playwright Testing (Shim)

## Quick Reference
Defines E2E test and video recording conventions for the Electron app.

## How to Use
When asked to record a UI demo or write a Playwright test, load the canonical skill:
```
docs/agents/skills/playwright-testing.md
```

## Quick Commands
```bash
cd src/electron-app
npx playwright test                  # run tests
```

**Never commit video files.** `test-results/` is already in `.gitignore`.

---
*This shim exists so that agent-specific directories (`.claude`, `.opencode`, `.cursor`) stay in sync. The canonical file is under `docs/agents/skills/`.*
