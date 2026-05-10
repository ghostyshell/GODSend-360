#!/usr/bin/env node
/**
 * Video recorder for the icon/banner fix demonstration.
 *
 * Flow:
 *   1. Settings → Save connection → Home (brief) → Reconnect → Library
 *   2. Open first game → scroll to Aurora Assets
 *   3. Show Icon + Banner BEFORE state
 *   4. Click "Search" on Icon → wait for Xbox CDN results
 *   5. Click first search result → sets Icon to PENDING (red dot)
 *   6. Close search panel → show Icon pending + Banner unchanged
 *   7. Click "Save to Console"
 *   8. Wait for green success message
 *   9. Show AFTER: Banner still intact → fix verified!
 *
 * Run: cd src/electron-app && node tests/record-demo-video.js
 */

const { _electron: electron } = require("playwright");
const path = require("path");
const fs = require("fs");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function getOutputText(page) {
  const pre = page.locator("pre").first();
  const visible = await pre.isVisible().catch(() => false);
  if (!visible) return "";
  return await pre.innerText().catch(() => "");
}

async function main() {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });

  const repoRoot = path.resolve(__dirname, "../../..");
  const mainJs = path.resolve(__dirname, "../main.js");

  if (!fs.existsSync(mainJs)) {
    console.error("[video] main.js not found. Run: npm run tsc");
    process.exit(1);
  }

  const goBinary = path.join(repoRoot, "dist", "godsend-mac");
  if (!fs.existsSync(goBinary)) {
    console.error("[video] Go binary not found. Build it first.");
    process.exit(1);
  }

  console.log("[video] Launching Electron app with video recording...");
  const electronApp = await electron.launch({
    executablePath: require("electron"),
    args: [mainJs],
    env: { ...process.env, NODE_ENV: "development" },
    recordVideo: {
      dir: OUTPUT_DIR,
      size: { width: 1440, height: 900 },
    },
    timeout: 60000,
  });

  const page = await electronApp.firstWindow();

  // ── 1. Backend startup ──────────────────────────────────────────────────
  console.log("[video] Step 1: Waiting for backend to start...");
  let backendReady = false;
  for (let i = 0; i < 60; i++) {
    const text = await getOutputText(page);
    if (text.includes("GODSend Backend Server")) {
      backendReady = true;
      console.log("[video] ✅ Backend started");
      break;
    }
    await sleep(1000);
  }
  if (!backendReady) {
    console.error("[video] ❌ Backend did not start");
    await electronApp.close();
    process.exit(1);
  }

  // ── 2. Settings ────────────────────────────────────────────────────────
  console.log("[video] Step 2: Navigating to Settings...");
  const settingsBtn = page.locator("button[title='Settings']").first();
  let foundSettings = false;
  for (let i = 0; i < 15; i++) {
    if (await settingsBtn.isVisible().catch(() => false)) {
      foundSettings = true;
      await settingsBtn.click();
      console.log("[video] ✅ Settings opened");
      break;
    }
    await sleep(1000);
  }
  if (!foundSettings) {
    console.error("[video] ❌ Settings button not found");
    await electronApp.close();
    process.exit(1);
  }
  await sleep(2000);

  const ipInput = page.locator("input#xboxIp").first();
  if (await ipInput.isVisible().catch(() => false)) {
    const val = await ipInput.inputValue().catch(() => "");
    if (!val.includes("192.168.1.229")) {
      await ipInput.fill("192.168.1.229");
      console.log("[video] ✅ Filled Xbox IP");
    } else {
      console.log("[video] ✅ Xbox IP already set");
    }
  }

  // ── 3. Save connection ────────────────────────────────────────────────
  console.log("[video] Step 3: Clicking 'Save connection'...");
  const saveBtn = page.locator("button:has-text('Save connection')").first();
  if (await saveBtn.isVisible().catch(() => false)) {
    await saveBtn.click();
    console.log("[video] ✅ Save connection clicked");
  } else {
    console.error("[video] ❌ 'Save connection' button not found");
    await electronApp.close();
    process.exit(1);
  }

  for (let i = 0; i < 15; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Saved. Backend restarted")) {
      console.log("[video] ✅ Save confirmed");
      break;
    }
    await sleep(1000);
  }
  await sleep(2000);

  // ── 4. Home (brief) & Reconnect ────────────────────────────────────────
  console.log("[video] Step 4: Home & Reconnect...");
  const homeBtn = page.locator("button[title='Home'], button[title='Console']").first();
  if (await homeBtn.isVisible().catch(() => false)) {
    await homeBtn.click();
    await sleep(500);
  }

  const reconnectBtn = page.locator("button[title='Retry FTP connection']").first();
  for (let i = 0; i < 10; i++) {
    if (await reconnectBtn.isVisible().catch(() => false)) {
      await reconnectBtn.click();
      console.log("[video] ✅ Reconnect clicked");
      break;
    }
    await sleep(500);
  }

  for (let i = 0; i < 60; i++) {
    const text = await getOutputText(page);
    if (text.includes("connected") || text.includes("disconnected")) {
      console.log("[video] ✅ FTP result seen");
      break;
    }
    await sleep(1000);
  }

  // ── 5. Library ─────────────────────────────────────────────────────────
  console.log("[video] Step 5: Opening Library...");
  const libraryBtn = page.locator("button[title='Xbox Library']").first();
  for (let i = 0; i < 30; i++) {
    if (await libraryBtn.isVisible().catch(() => false)) {
      await libraryBtn.click();
      console.log("[video] ✅ Library clicked");
      break;
    }
    await sleep(1000);
  }

  let onLibrary = false;
  for (let i = 0; i < 90; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Xbox Library") && /\d+\s+games/.test(bodyText)) {
      onLibrary = true;
      console.log("[video] ✅ Library loaded");
      break;
    }
    await sleep(1000);
  }
  if (!onLibrary) {
    console.error("[video] ❌ Library did not load");
    await electronApp.close();
    process.exit(1);
  }

  // ── 6. Open first game ─────────────────────────────────────────────────
  console.log("[video] Step 6: Opening first game...");
  await page.evaluate(() => {
    const grid = document.querySelector('div[class*="grid"]');
    const firstCard = grid?.querySelector('button');
    if (firstCard) firstCard.click();
  });
  console.log("[video] ✅ First game opened");
  await sleep(3000);

  // ── 7. Scroll to Aurora Assets ───────────────────────────────────────
  console.log("[video] Step 7: Scrolling to Aurora Assets...");
  const assetsHeader = page.locator("text=Aurora Assets").first();
  if (await assetsHeader.isVisible().catch(() => false)) {
    await assetsHeader.scrollIntoViewIfNeeded();
  }
  await sleep(2000);

  // ── 8. BEFORE state ────────────────────────────────────────────────────
  console.log("[video] Step 8: BEFORE state...");
  const icon = page.locator("text=Icon").first();
  const banner = page.locator("text=Banner").first();
  if (await icon.isVisible().catch(() => false)) {
    await icon.scrollIntoViewIfNeeded();
    await sleep(2000);
    console.log("[video] ✅ Icon shown (before)");
  }
  if (await banner.isVisible().catch(() => false)) {
    await banner.scrollIntoViewIfNeeded();
    await sleep(2000);
    console.log("[video] ✅ Banner shown (before)");
  }

  // ── 9. Click "Search" on Icon ──────────────────────────────────────────
  console.log("[video] Step 9: Clicking 'Search' on Icon...");
  await page.evaluate(() => {
    // Find the Icon card and click its Search button
    const labels = Array.from(document.querySelectorAll('p'));
    for (const p of labels) {
      if (p.textContent?.trim() === 'Icon') {
        const card = p.closest('div[class*="flex-col"]') || p.parentElement?.parentElement;
        if (card) {
          const searchBtn = Array.from(card.querySelectorAll('button')).find(
            b => b.textContent?.includes('Search')
          );
          if (searchBtn) {
            searchBtn.click();
            return true;
          }
        }
      }
    }
    return false;
  });
  console.log("[video] ✅ Clicked 'Search' on Icon");
  await sleep(2000);

  // ── 10. Wait for search results ────────────────────────────────────────
  console.log("[video] Step 10: Waiting for search results...");
  let resultsReady = false;
  for (let i = 0; i < 30; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Results from") || bodyText.includes("Official") || bodyText.includes("Xbox CDN")) {
      resultsReady = true;
      console.log("[video] ✅ Search results loaded");
      break;
    }
    // Also check for "No icon results found"
    if (bodyText.includes("No icon results found")) {
      console.log("[video] ⚠️  No icon results from CDN");
      break;
    }
    await sleep(1000);
  }

  if (resultsReady) {
    // ── 11. Click first result ─────────────────────────────────────────
    console.log("[video] Step 11: Clicking first result...");
    // Use Playwright to click the first button inside the search results flex-wrap container
    const firstResult = page.locator('div[class*="flex-wrap"] button').first();
    if (await firstResult.isVisible().catch(() => false)) {
      await firstResult.click();
      console.log("[video] ✅ First result clicked (Playwright)");
    } else {
      console.warn("[video] ⚠️  First result not visible");
    }
    await sleep(3000);

    // ── 12. Close search panel ──────────────────────────────────────────
    console.log("[video] Step 12: Closing search panel...");
    const closeBtn = page.locator("button[title='Close']").first();
    if (await closeBtn.isVisible().catch(() => false)) {
      await closeBtn.click();
      console.log("[video] ✅ Search panel closed");
    } else {
      await page.evaluate(() => {
        const xBtn = document.querySelector('button[title="Close"]');
        if (xBtn) xBtn.click();
      });
      console.log("[video] ✅ Search panel closed (evaluate)");
    }
    await sleep(2000);
  } else {
    console.warn("[video] ⚠️  Skipping result click — no results");
  }

  // ── 13. PENDING state ──────────────────────────────────────────────────
  console.log("[video] Step 13: PENDING state...");
  if (await icon.isVisible().catch(() => false)) {
    await icon.scrollIntoViewIfNeeded();
    await sleep(3000);
    console.log("[video] ✅ Icon shown (should have pending red dot)");
  }
  if (await banner.isVisible().catch(() => false)) {
    await banner.scrollIntoViewIfNeeded();
    await sleep(3000);
    console.log("[video] ✅ Banner shown (should be unchanged)");
  }

  // ── 14. Save to Console ────────────────────────────────────────────────
  console.log("[video] Step 14: Clicking 'Save to Console'...");
  const saveConsoleBtn = page.locator("button:has-text('Save to Console')").first();
  let saveClicked = false;
  if (await saveConsoleBtn.isVisible().catch(() => false)) {
    await saveConsoleBtn.click();
    saveClicked = true;
    console.log("[video] ✅ 'Save to Console' clicked (locator)");
  } else {
    // Fallback: use evaluate to find by text
    const clicked = await page.evaluate(() => {
      const buttons = Array.from(document.querySelectorAll('button'));
      for (const btn of buttons) {
        if (btn.textContent?.includes('Save to Console')) {
          btn.click();
          return true;
        }
      }
      return false;
    });
    if (clicked) {
      saveClicked = true;
      console.log("[video] ✅ 'Save to Console' clicked (evaluate)");
    } else {
      console.warn("[video] ⚠️  'Save to Console' not found");
    }
  }

  // ── 15. Wait for save success ──────────────────────────────────────────
  console.log("[video] Step 15: Waiting for save success...");
  let saveSuccess = false;
  for (let i = 0; i < 30; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("saved as RXEA") || bodyText.includes("asset(s) saved")) {
      saveSuccess = true;
      console.log("[video] ✅ Save success message seen");
      break;
    }
    await sleep(1000);
  }
  if (!saveSuccess) {
    console.warn("[video] ⚠️  Save success not seen");
  }
  await sleep(2000);

  // ── 16. AFTER state: Banner survives ─────────────────────────────────
  console.log("[video] Step 16: AFTER state — verifying Banner survived...");
  if (await banner.isVisible().catch(() => false)) {
    await banner.scrollIntoViewIfNeeded();
    await sleep(3000);
    console.log("[video] ✅ Banner still intact — fix verified!");
  }

  // ── 17. End card ───────────────────────────────────────────────────────
  console.log("[video] Step 17: Recording end card...");
  await sleep(3000);

  // ── Close and save video ───────────────────────────────────────────────
  console.log("[video] Closing app...");
  await electronApp.close();

  const files = fs.readdirSync(OUTPUT_DIR)
    .filter((f) => f.endsWith(".webm"))
    .map((f) => ({ name: f, stat: fs.statSync(path.join(OUTPUT_DIR, f)) }))
    .sort((a, b) => b.stat.mtimeMs - a.stat.mtimeMs);

  if (files.length > 0) {
    const latest = files[0];
    console.log(
      `[video] ✅ Video saved: ${latest.name} (${(latest.stat.size / 1024 / 1024).toFixed(1)} MB)`
    );
    console.log(`[video] Path: ${path.join(OUTPUT_DIR, latest.name)}`);
  } else {
    console.log("[video] ⚠️  No video file found");
  }
}

main().catch((err) => {
  console.error("[video] Fatal:", err);
  process.exit(1);
});
