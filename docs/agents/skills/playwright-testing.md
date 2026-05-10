# Skill: Playwright Testing — UI Recording & E2E Convention

## Description
Defines how to record video demos and write E2E tests for the GODsend-360 Electron app using Playwright.

## Scope
- `src/electron-app/tests/**/*.spec.ts` — Playwright test specs
- `src/electron-app/tests/**/*.js` — standalone recording scripts
- `src/electron-app/playwright.config.ts` — Playwright configuration
- Video outputs under `test-results/`

## Key Files
| Path | Purpose |
|---|---|
| `src/electron-app/tests/record-fix-demo.js` | Standalone video recorder script |
| `src/electron-app/tests/record-fix-demo-ffmpeg.js` | Screen-capture via ffmpeg |
| `src/electron-app/playwright.config.ts` | Playwright config (video: on, trace: on-first-retry) |

## Recording Conventions
1. Launch Electron in dev mode with `NODE_ENV=development`
2. Set viewport to `{ width: 1440, height: 900 }` for consistent demo sizing
3. Use `electron.launch({ args: [path.resolve(__dirname, "../main.js")] })`
4. Wait for `window` event before accessing `firstWindow()`
5. Add generous `waitForTimeout` pauses (2–5 s) between steps for clarity
6. Use defensive visibility checks (`isVisible().catch(() => false)`) because Electron pages may load conditionally (e.g. Xbox FTP offline)

## Video Output
- Playwright built-in video saves to `test-results/` in webm format
- FFmpeg screen capture saves to `test-results/fix-demo-video.mp4`
- Always clean old output before recording

## Running Tests
```bash
cd src/electron-app
npm install --save-dev playwright
npx playwright test                  # run all specs
node tests/record-fix-demo.js       # standalone script
node tests/record-fix-demo-ffmpeg.js # ffmpeg screen capture
```

## See Also
- `docs-source-of-truth.md` — general doc conventions
- `shim-electron.md` — Electron app architecture
