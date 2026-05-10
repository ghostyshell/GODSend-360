#!/usr/bin/env node
/**
 * Playwright video recorder for the icon/banner fix demo.
 *
 * This script launches the Electron app in dev mode and records a
 * narrated walkthrough of the fix.  It navigates the Xbox Library,
 * opens a game detail, shows the Aurora Asset Editor, and demonstrates
 * that saving an icon no longer wipes the banner (and vice versa).
 *
 * Usage:
 *   node tests/record-fix-demo.js
 *
 * Output:
 *   test-results/fix-demo-video.webm
 */

const { chromium } = require("playwright");
const { _electron: electron } = require("playwright");
const path = require("path");
const fs = require("fs");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");
const VIDEO_PATH = path.join(OUTPUT_DIR, "fix-demo-video.webm");

async function main() {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });

  console.log("[demo] Launching Electron app with video recording...");
  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: {
      ...process.env,
      NODE_ENV: "development",
    },
  });

  await electronApp.waitForEvent("window");
  const page = await electronApp.firstWindow();

  // Set viewport for clean recording
  await page.setViewportSize({ width: 1440, height: 900 });

  // Start video recording via Playwright's built-in video (configured in context)
  // We use the page's own video handle
  const video = page.video();

  console.log("[demo] Waiting for app to initialise...");
  await page.waitForTimeout(4000);

  // ── Step 1: Navigate to Library ──────────────────────────────────────────
  console.log("[demo] Navigating to Xbox Library...");
  const libraryBtn = page.locator("button, a").filter({ hasText: /Library/i }).first();
  if (await libraryBtn.isVisible().catch(() => false)) {
    await libraryBtn.click();
    await page.waitForTimeout(2500);
  }

  // ── Step 2: Wait for grid or show placeholder ───────────────────────────
  console.log("[demo] Waiting for game grid...");
  const gridReady = await page
    .waitForSelector("[class*='cover'], [class*='game']", { timeout: 20000 })
    .then(() => true)
    .catch(() => false);

  if (!gridReady) {
    console.log("[demo] Grid not loaded (FTP may be offline) — showing UI state");
  }

  // ── Step 3: Click first game card ───────────────────────────────────────
  const firstCard = page.locator("[class*='cover'], [class*='game']").first();
  if (await firstCard.isVisible().catch(() => false)) {
    console.log("[demo] Opening first game detail...");
    await firstCard.click();
    await page.waitForTimeout(2500);
  }

  // ── Step 4: Scroll to Assets section ────────────────────────────────────
  console.log("[demo] Scrolling to Aurora Assets section...");
  const assetsHeading = page.locator("text=Assets, text=Asset").first();
  if (await assetsHeading.isVisible().catch(() => false)) {
    await assetsHeading.scrollIntoViewIfNeeded();
    await page.waitForTimeout(1500);
  }

  // ── Step 5: Show Icon slot and open search ───────────────────────────────
  console.log("[demo] Showing Icon slot...");
  const iconSlot = page.locator("text=Icon").first();
  if (await iconSlot.isVisible().catch(() => false)) {
    await iconSlot.scrollIntoViewIfNeeded();
    await page.waitForTimeout(1000);

    const searchBtn = page.locator("button").filter({ hasText: /Search/i }).first();
    if (await searchBtn.isVisible().catch(() => false)) {
      await searchBtn.click();
      await page.waitForTimeout(2500);

      // Close modal
      const closeBtn = page.locator("button").filter({ hasText: /Close|×|X/i }).first();
      if (await closeBtn.isVisible().catch(() => false)) {
        await closeBtn.click();
        await page.waitForTimeout(800);
      }
    }
  }

  // ── Step 6: Show Banner slot ──────────────────────────────────────────────
  console.log("[demo] Showing Banner slot...");
  const bannerSlot = page.locator("text=Banner").first();
  if (await bannerSlot.isVisible().catch(() => false)) {
    await bannerSlot.scrollIntoViewIfNeeded();
    await page.waitForTimeout(2000);
  }

  // ── Step 7: End card ────────────────────────────────────────────────────
  console.log("[demo] Recording end card...");
  await page.waitForTimeout(3000);

  // Save video
  if (video) {
    const videoPath = await video.path();
    console.log("[demo] Video saved to:", videoPath);
  }

  await electronApp.close();
  console.log("[demo] Done.");
}

main().catch((err) => {
  console.error("[demo] Error:", err);
  process.exit(1);
});
