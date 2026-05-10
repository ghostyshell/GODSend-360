# Skill: Playwright Testing — UI Recording & E2E Convention

## Description
Defines how to write E2E tests and record video demos for the GODsend-360 Electron app using Playwright.

## Scope
- `src/electron-app/tests/**/*.spec.ts` — Playwright test specs
- `src/electron-app/tests/**/*.js` — standalone recording scripts
- `src/electron-app/playwright.config.ts` — Playwright configuration
- Video outputs under `test-results/` (never committed)

## Key Files
| Path | Purpose |
|---|---|
| `src/electron-app/playwright.config.ts` | Playwright config (video: on, trace: on-first-retry) |

## Recording Conventions
1. Launch Electron in dev mode with `NODE_ENV=development`
2. Set viewport to `{ width: 1440, height: 900 }` for consistent demo sizing
3. Use `electron.launch({ args: [path.resolve(__dirname, "../main.js")] })`
4. Wait for `window` event before accessing `firstWindow()`
5. Add generous `waitForTimeout` pauses (2–5 s) between steps for clarity
6. Use defensive visibility checks (`isVisible().catch(() => false)`) because Electron pages may load conditionally (e.g. Xbox FTP offline)

## Hard-Won Targeting Rules (GODsend Electron)

These rules are critical — they represent ~10 iterations of trial-and-error against the actual React renderer.

### ❌ NEVER use these locators
| Bad locator | Why it fails |
|---|---|
| `img[class*='cover']` | Game cards do **not** use `<img>` tags. Covers are rendered as `<div>` with inline `style="aspect-ratio:3/4"` (no space after colon) and `background-image` / lazily-loaded `<img>` inside `<button>`. |
| `page.locator("pre")` on Library page | The `<pre>` console output panel is part of **HomePage**, not **LibraryPage**. It does not exist on the Library page at all. |
| `page.evaluate(() => button.click())` for interactive elements | React synthetic events may not fire. Use Playwright `.click()` on proper locators. |
| `button:has-text('Save')` generically | There are multiple Save buttons (Save connection, Save port, Save drive). Be specific: `button:has-text('Save connection')`. |

### ✅ Correct locators
| Target | Correct locator |
|---|---|
| Backend started (Home page) | `page.locator("pre").first()` — text includes `"GODSend Backend Server"` |
| Backend restarted after Save | Poll `page.innerText("body")` and count occurrences of `"GODSend Backend Server"` (should be ≥2) |
| Game card in Library | `page.locator("button").nth(1)` or `page.evaluate(() => document.querySelector('div[class*="grid"] button').click())` |
| Game grid loaded | `page.innerText("body")` matches `/Xbox Library/` + `/\d+\s+games/` |
| Icon/Banner slots | `page.locator("text=Icon").first()` / `page.locator("text=Banner").first()` |
| "Save to Console" button | `page.locator("button:has-text('Save to Console')").first()` — only appears when `pending` state is non-empty |
| Search result item | `page.locator('div[class*="flex-wrap"] button').first()` |
| Close search panel | `page.locator("button[title='Close']").first()` |

### Dev Mode Gotchas
- **userData directory**: In dev mode, Electron uses `~/Library/Application Support/Electron/` (not `godsend-electron/`). Copy `config.json` there if the script needs saved settings.
- **Backend restart on Save**: Clicking "Save connection" restarts the Go backend. You must wait for the second banner before Reconnect works.
- **Library does not auto-open after Reconnect**: The app stays on Home page. You must manually click the Library button.
- **Aurora Assets are below the fold**: After opening a game, scroll down to `text=Aurora Assets`.
- **Search results auto-load**: The `AssetSearchPanel` auto-searches on open. Wait for `Results from` text or `Official`/`Xbox CDN` badges.

## Video Output
- Playwright built-in video saves to `test-results/` in webm format
- FFmpeg screen capture can save to `test-results/*.mp4`
- Always clean old output before recording
- **NEVER commit video files to git** — `test-results/` is already in `.gitignore`

## Running Tests
```bash
cd src/electron-app
npm install --save-dev playwright
npx playwright test                  # run all specs
node tests/my-demo-script.js         # standalone script (create as needed)
```

## Example: Correct Library Detection
```js
// ❌ WRONG — pre doesn't exist on Library page
const text = await page.locator("pre").innerText(); // throws!

// ✅ CORRECT — use body text
const bodyText = await page.innerText("body");
if (bodyText.includes("Xbox Library") && /\d+\s+games/.test(bodyText)) {
  console.log("Library loaded");
}
```

## See Also
- `docs-source-of-truth.md` — general doc conventions
- `shim-electron.md` — Electron app architecture
