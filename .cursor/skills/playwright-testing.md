> **SKILL SHIM** — This file is a pointer only. The canonical source of truth lives at:
> `docs/agents/skills/playwright-testing.md`

# Skill: Playwright Testing (Shim)

## Quick Reference
Defines video recording and E2E test conventions for the Electron app.

## How to Use
When asked to record a UI demo or write a Playwright test, load the canonical skill:
```
docs/agents/skills/playwright-testing.md
```

## Quick Commands
```bash
cd src/electron-app
npx playwright test                  # run tests
node tests/record-fix-demo-ffmpeg.js # record screen capture
```

---
*This shim exists so that agent-specific directories (`.claude`, `.opencode`, `.cursor`) stay in sync. The canonical file is under `docs/agents/skills/`.*
