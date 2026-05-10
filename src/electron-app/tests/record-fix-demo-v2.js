#!/usr/bin/env node
/**
 * Live screen-recording demo of the icon/banner fix with a real Xbox.
 *
 * This script connects to the Xbox at 192.168.1.229, loads the Xbox Library,
 * opens a game detail, and demonstrates the Aurora Asset Editor showing
 * that icon and banner are preserved independently.
 *
 * Usage:
 *   node tests/record-fix-demo-v2.js
 *
 * Output:
 *   test-results/fix-demo-v2.mp4
 */

const { _electron: electron } = require("playwright");
const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");
const VIDEO_PATH = path.join(OUTPUT_DIR, "fix-demo-v2.mp4");

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function startFfmpeg(outputPath) {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });
  try { fs.unlinkSync(outputPath); } catch {}

  return new Promise((resolve, reject) => {
    const args = [
      "-f", "avfoundation",
      "-framerate", "30",
      "-i", "0:none",
      "-pixel_format", "yuv420p",
      "-s", "2560x1600",
      "-vf", "scale=1280:800",
      "-preset", "ultrafast",
      "-tune", "zerolatency",
      "-y",
      outputPath,
    ];

    const proc = spawn("ffmpeg", args, { stdio: "pipe" });
    proc.stderr.on("data", () => {}); // swallow ffmpeg noise
    proc.on("error", reject);

    setTimeout(() => {
      console.log("[ffmpeg] Recording →", outputPath);
      resolve(proc);
    }, 1500);
  });
}

async function main() {
  console.log("[demo] Starting screen recording...");
  let recorder;
  try {
    recorder = await startFfmpeg(VIDEO_PATH);
  } catch (err) {
    console.error("[demo] ffmpeg failed:", err.message);
    process.exit(1);
  }

  await sleep(2000);

  console.log("[demo] Launching Electron app...");
  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: { ...process.env, NODE_ENV: "development" },
  });

  await electronApp.waitForEvent("window");
  const page = await electronApp.firstWindow();
  await page.setViewportSize({ width: 1440, height: 900 });

  console.log("[demo] Waiting for backend startup + FTP ping (may take 10–20s)...");
  await sleep(15000);

  // ── The app auto-navigates to Library on successful FTP ping ───
  console.log("[demo] Detecting current page state...");

  // Step 1: Wait for game cards OR the output panel (home page)
  let onLibrary = false;
  let onHome = false;

  for (let attempt = 0; attempt < 30; attempt++) {
    const card = page.locator("[class*='cover']").first();
    if (await card.isVisible().catch(() => false)) {
      onLibrary = true;
      console.log("[demo]   ✅ We are on the Library page");
      break;
    }
    // Check if still on home page (output panel visible)
    const outputPanel = page.locator("[class*='output'], [class*='console']").first();
    if (await outputPanel.isVisible().catch(() => false)) {
      onHome = true;
    }
    await sleep(1000);
  }

  // If still on home, click the Library button
  if (onHome && !onLibrary) {
    console.log("[demo]   Clicking Library button manually...");
    // Look for any element with "Library" text
    const allEls = page.locator("*");
    const count = await allEls.count();
    for (let i = 0; i < count; i++) {
      const text = await allEls.nth(i).innerText().catch(() => "");
      if (text.toLowerCase().includes("library") && text.length < 20) {
        await allEls.nth(i).click();
        console.log("[demo]   Clicked:", text);
        await sleep(5000);
        onLibrary = true;
        break;
      }
    }
  }

  if (!onLibrary) {
    console.log("[demo]   Could not detect library page — falling back to screenshot capture");
  }

  // Step 2: Wait for game grid to fully load
  if (onLibrary) {
    console.log("[demo] Step 2: Waiting for game grid...");
    let loaded = false;
    for (let attempt = 0; attempt < 40; attempt++) {
      const card = page.locator("[class*='cover']").first();
      if (await card.isVisible().catch(() => false)) {
        loaded = true;
        console.log("[demo]   ✅ Game grid loaded");
        break;
      }
      await sleep(1000);
    }
    if (!loaded) {
      console.log("[demo]   ⚠️ Grid not loaded — Xbox FTP may be slow");
    }

    await sleep(3000);
  }

  // Step 3: Click first game card
  console.log("[demo] Step 3: Open first game detail");
  const firstCard = page.locator("[class*='cover']").first();
  if (await firstCard.isVisible().catch(() => false)) {
    await firstCard.click();
    console.log("[demo]   ✅ Opened game detail");
    await sleep(5000);
  } else {
    console.log("[demo]   ❌ No game card found");
  }

  // Step 4: Scroll to Aurora Assets section
  console.log("[demo] Step 4: Scroll to Aurora Assets");
  const assetsHeading = page.locator("text=Assets").first();
  if (await assetsHeading.isVisible().catch(() => false)) {
    await assetsHeading.scrollIntoViewIfNeeded();
    console.log("[demo]   ✅ Scrolled to Assets heading");
    await sleep(2000);
  } else {
    // Try generic scroll
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight / 2));
    await sleep(2000);
  }

  // Step 5: Show Icon slot
  console.log("[demo] Step 5: Show Icon slot");
  const iconSlot = page.locator("text=Icon").first();
  if (await iconSlot.isVisible().catch(() => false)) {
    await iconSlot.scrollIntoViewIfNeeded();
    console.log("[demo]   ✅ Icon slot visible");
    await sleep(2000);

    const searchBtn = page.locator("button").filter({ hasText: /Search/i }).first();
    if (await searchBtn.isVisible().catch(() => false)) {
      await searchBtn.click();
      console.log("[demo]   ✅ Opened search dialog");
      await sleep(3000);

      const closeBtn = page.locator("button").filter({ hasText: /Close|×|X/i }).first();
      if (await closeBtn.isVisible().catch(() => false)) {
        await closeBtn.click();
        await sleep(1000);
        console.log("[demo]   ✅ Closed search dialog");
      }
    }
  }

  // Step 6: Show Banner slot
  console.log("[demo] Step 6: Show Banner slot");
  const bannerSlot = page.locator("text=Banner").first();
  if (await bannerSlot.isVisible().catch(() => false)) {
    await bannerSlot.scrollIntoViewIfNeeded();
    console.log("[demo]   ✅ Banner slot visible");
    await sleep(3000);
  }

  // Step 7: Highlight fix
  console.log("[demo] Step 7: Highlight fix — Icon + Banner share GL asset");
  if (await assetsHeading.isVisible().catch(() => false)) {
    await assetsHeading.scrollIntoViewIfNeeded();
    await sleep(2000);
  }

  // End card
  console.log("[demo] Step 8: End card");
  await sleep(5000);

  console.log("[demo] Closing app...");
  await electronApp.close();

  recorder.kill("SIGINT");
  await sleep(2000);
  if (!recorder.killed) recorder.kill("SIGTERM");

  if (fs.existsSync(VIDEO_PATH)) {
    const stats = fs.statSync(VIDEO_PATH);
    console.log(`[demo] ✅ Video saved: ${VIDEO_PATH} (${(stats.size / 1024 / 1024).toFixed(1)} MB)`);
  } else {
    console.log("[demo] ⚠️  Video not found");
  }
}

main().catch((err) => {
  console.error("[demo] Fatal:", err);
  process.exit(1);
});
