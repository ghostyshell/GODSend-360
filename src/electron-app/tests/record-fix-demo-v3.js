#!/usr/bin/env node
/**
 * Playwright video recorder using Playwright's built-in video capture.
 * This records the actual Electron window pixels, not the screen.
 *
 * Usage:
 *   node tests/record-fix-demo-v3.js
 *
 * Output:
 *   test-results/v3-*.webm
 */

const { _electron: electron } = require("playwright");
const path = require("path");
const fs = require("fs");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function main() {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });

  console.log("[demo] Launching Electron app with Playwright video recording...");
  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: { ...process.env, NODE_ENV: "development" },
    recordVideo: {
      dir: OUTPUT_DIR,
      size: { width: 1280, height: 720 },
    },
  });

  await electronApp.waitForEvent("window");

  const context = electronApp.context();
  const pages = await context.pages();
  const page = pages.length > 0 ? pages[0] : await electronApp.firstWindow();

  console.log("[demo] App launched. Waiting for backend + FTP...");
  await sleep(15000);

  // Step 1: Wait for Library page to auto-load
  console.log("[demo] Step 1: Waiting for Library to auto-load...");
  let isLibrary = false;
  for (let i = 0; i < 20; i++) {
    const cards = page.locator("img[class*='cover'], [class*='game-card'], [class*='coverart']").first();
    if (await cards.isVisible().catch(() => false)) {
      isLibrary = true;
      console.log("[demo]   Library loaded with games");
      break;
    }
    const output = page.locator("[class*='output']").first();
    if (await output.isVisible().catch(() => false)) {
      console.log("[demo]   On home page - clicking Library button...");
      const btns = page.locator("nav button, [role='tab'] button");
      const n = await btns.count().catch(() => 0);
      for (let j = 0; j < n; j++) {
        const title = await btns.nth(j).getAttribute("title").catch(() => "");
        if (title.toLowerCase().includes("library")) {
          await btns.nth(j).click();
          await sleep(5000);
          isLibrary = true;
          break;
        }
      }
      break;
    }
    await sleep(1000);
  }

  if (!isLibrary) {
    console.log("[demo]   Could not confirm library page");
  }

  await sleep(3000);

  // Step 2: Click first game card
  console.log("[demo] Step 2: Click first game card");
  let detailOpened = false;
  for (let retry = 0; retry < 3; retry++) {
    const card = page.locator("img[class*='cover'], [class*='game-card']").first();
    if (await card.isVisible().catch(() => false)) {
      await card.click();
      console.log("[demo]   Clicked game card");
      detailOpened = true;
      break;
    }
    console.log("[demo]   No card visible, retrying...");
    await sleep(2000);
  }

  if (!detailOpened) {
    console.log("[demo]   Could not open game detail - continuing");
  }

  await sleep(5000);

  // Step 3: Scroll to Assets section
  console.log("[demo] Step 3: Scroll to Aurora Assets");
  const assetTexts = ["Assets", "Aurora Assets", "Icon", "Banner"];
  for (const text of assetTexts) {
    const el = page.locator(`text=${text}`).first();
    if (await el.isVisible().catch(() => false)) {
      await el.scrollIntoViewIfNeeded();
      console.log(`[demo]   Scrolled to "${text}"`);
      await sleep(2000);
      break;
    }
  }

  // Step 4: Show Icon slot
  console.log("[demo] Step 4: Show Icon slot");
  const iconEl = page.locator("text=Icon").first();
  if (await iconEl.isVisible().catch(() => false)) {
    await iconEl.scrollIntoViewIfNeeded();
    await sleep(2000);
  }

  // Step 5: Show Banner slot
  console.log("[demo] Step 5: Show Banner slot");
  const bannerEl = page.locator("text=Banner").first();
  if (await bannerEl.isVisible().catch(() => false)) {
    await bannerEl.scrollIntoViewIfNeeded();
    await sleep(2000);
  }

  // Step 6: End card
  console.log("[demo] Step 6: End card");
  await sleep(3000);

  console.log("[demo] Closing app...");
  await electronApp.close();

  const files = fs.readdirSync(OUTPUT_DIR).filter((f) => f.includes("v3") || f.endsWith(".webm"));
  if (files.length > 0) {
    const latest = files.sort((a, b) => {
      const sa = fs.statSync(path.join(OUTPUT_DIR, a));
      const sb = fs.statSync(path.join(OUTPUT_DIR, b));
      return sb.mtimeMs - sa.mtimeMs;
    })[0];
    const stats = fs.statSync(path.join(OUTPUT_DIR, latest));
    console.log(`[demo] Video saved: ${latest} (${(stats.size / 1024 / 1024).toFixed(1)} MB)`);
  } else {
    console.log("[demo] No video file found in", OUTPUT_DIR);
  }
}

main().catch((err) => {
  console.error("[demo] Fatal:", err);
  process.exit(1);
});
