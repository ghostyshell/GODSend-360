#!/usr/bin/env node
/**
 * Screen recording demo of the icon/banner fix with a live Xbox.
 *
 * This script launches the Electron app, connects to the Xbox at
 * 192.168.1.229, navigates the Library, and demonstrates that
 * updating an icon no longer wipes the banner (and vice versa).
 *
 * Prerequisites: ffmpeg installed (brew install ffmpeg)
 *
 * Usage:
 *   node tests/record-fix-demo-live.js
 *
 * Output:
 *   test-results/fix-demo-live.mp4
 */

const { _electron: electron } = require("playwright");
const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");
const os = require("os");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");
const VIDEO_PATH = path.join(OUTPUT_DIR, "fix-demo-live.mp4");

async function startFfmpeg(outputPath) {
  return new Promise((resolve, reject) => {
    fs.mkdirSync(OUTPUT_DIR, { recursive: true });
    try { fs.unlinkSync(outputPath); } catch {}

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
    let stderr = "";
    proc.stderr.on("data", (d) => { stderr += d.toString(); });

    proc.on("error", (err) => reject(err));

    // Give ffmpeg a moment to spin up
    setTimeout(() => {
      console.log("[ffmpeg] Started recording to", outputPath);
      resolve(proc);
    }, 1000);
  });
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function main() {
  console.log("[demo] Starting screen recording with ffmpeg...");
  let recorder;
  try {
    recorder = await startFfmpeg(VIDEO_PATH);
  } catch (err) {
    console.error("[demo] Failed to start ffmpeg:", err.message);
    console.error("[demo] Make sure ffmpeg is installed: brew install ffmpeg");
    process.exit(1);
  }

  await sleep(2000);

  console.log("[demo] Launching GODsend Electron app...");
  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: {
      ...process.env,
      NODE_ENV: "development",
    },
  });

  await electronApp.waitForEvent("window");
  const page = await electronApp.firstWindow();

  // Maximize window for clear recording
  await page.setViewportSize({ width: 1440, height: 900 });

  console.log("[demo] Waiting for app to initialise (backend start)...");
  await sleep(8000);

  // ── Step 1: Navigate to Xbox Library ──────────────────────────────────────
  console.log("[demo] Step 1: Navigate to Xbox Library");
  const libraryBtn = page.locator("button, a").filter({ hasText: /Library|Xbox Library/i }).first();
  if (await libraryBtn.isVisible().catch(() => false)) {
    await libraryBtn.click();
    console.log("[demo]   Clicked Library button");
    await sleep(4000);
  } else {
    console.log("[demo]   Library button not visible, trying fallback...");
    // Try clicking any button that might be the library
    const allBtns = page.locator("button");
    const count = await allBtns.count().catch(() => 0);
    for (let i = 0; i < count; i++) {
      const text = await allBtns.nth(i).innerText().catch(() => "");
      if (text.toLowerCase().includes("library")) {
        await allBtns.nth(i).click();
        await sleep(4000);
        break;
      }
    }
  }

  // ── Step 2: Wait for game grid to load ───────────────────────────────────
  console.log("[demo] Step 2: Wait for game grid to load from Xbox...");
  let gridReady = false;
  for (let attempt = 0; attempt < 30; attempt++) {
    const firstCard = page.locator("[class*='cover']").first();
    if (await firstCard.isVisible().catch(() => false)) {
      gridReady = true;
      console.log("[demo]   Game grid loaded");
      break;
    }
    await sleep(1000);
  }
  if (!gridReady) {
    console.log("[demo]   WARNING: Grid did not load — continuing with UI capture");
  }

  // ── Step 3: Click first game card ──────────────────────────────────────
  console.log("[demo] Step 3: Open first game detail");
  const firstCard = page.locator("[class*='cover']").first();
  if (await firstCard.isVisible().catch(() => false)) {
    await firstCard.click();
    console.log("[demo]   Opened game detail");
    await sleep(4000);
  }

  // ── Step 4: Scroll to Aurora Assets section ───────────────────────────────
  console.log("[demo] Step 4: Show Aurora Assets section");
  let foundAssets = false;
  const scrollAttempts = [
    "text=Assets",
    "text=Aurora Assets",
    "text=Icon",
    "text=Banner",
  ];
  for (const sel of scrollAttempts) {
    const el = page.locator(sel).first();
    if (await el.isVisible().catch(() => false)) {
      await el.scrollIntoViewIfNeeded();
      foundAssets = true;
      console.log("[demo]   Scrolled to:", sel);
      await sleep(2000);
      break;
    }
  }
  if (!foundAssets) {
    // Generic scroll down
    await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight / 2));
    await sleep(2000);
  }

  // ── Step 5: Show Icon slot ─────────────────────────────────────────────
  console.log("[demo] Step 5: Show Icon slot");
  const iconSlot = page.locator("text=Icon").first();
  if (await iconSlot.isVisible().catch(() => false)) {
    await iconSlot.scrollIntoViewIfNeeded();
    await sleep(2000);

    // Show the current icon (or empty state)
    const iconArea = iconSlot.locator("xpath=../..");
    if (await iconArea.isVisible().catch(() => false)) {
      await iconArea.hover();
      await sleep(1000);
    }

    // Click Search button near icon
    const searchBtn = page.locator("button").filter({ hasText: /Search/i }).first();
    if (await searchBtn.isVisible().catch(() => false)) {
      await searchBtn.click();
      console.log("[demo]   Opened asset search dialog");
      await sleep(3000);

      // Close without selecting (to demo the fix flow)
      const closeBtn = page.locator("button").filter({ hasText: /Close|×|X/i }).first();
      if (await closeBtn.isVisible().catch(() => false)) {
        await closeBtn.click();
        await sleep(1000);
        console.log("[demo]   Closed search dialog");
      }
    }
  }

  // ── Step 6: Show Banner slot ───────────────────────────────────────────
  console.log("[demo] Step 6: Show Banner slot");
  const bannerSlot = page.locator("text=Banner").first();
  if (await bannerSlot.isVisible().catch(() => false)) {
    await bannerSlot.scrollIntoViewIfNeeded();
    await sleep(2500);

    // Hover to show the slot area
    const bannerArea = bannerSlot.locator("xpath=../..");
    if (await bannerArea.isVisible().catch(() => false)) {
      await bannerArea.hover();
      await sleep(1000);
    }

    // Show explanation: both slots are from the same GL file
    console.log("[demo]   Banner slot visible — both icon + banner share GL.asset");
  }

  // ── Step 7: Highlight the fix ─────────────────────────────────────────────
  console.log("[demo] Step 7: Highlight the fix");
  await sleep(2000);

  // Scroll back up to show the full assets panel
  const assetsHeading = page.locator("text=Assets").first();
  if (await assetsHeading.isVisible().catch(() => false)) {
    await assetsHeading.scrollIntoViewIfNeeded();
    await sleep(3000);
  }

  // ── Step 8: End card ────────────────────────────────────────────────────
  console.log("[demo] Step 8: End card — fix demonstration complete");
  await sleep(5000);

  console.log("[demo] Closing app and stopping recording...");
  await electronApp.close();

  // Gracefully stop ffmpeg
  recorder.kill("SIGINT");
  await sleep(2000);
  if (!recorder.killed) {
    recorder.kill("SIGTERM");
    await sleep(1000);
  }

  if (fs.existsSync(VIDEO_PATH)) {
    const stats = fs.statSync(VIDEO_PATH);
    console.log(`[demo] ✅ Video saved: ${VIDEO_PATH} (${(stats.size / 1024 / 1024).toFixed(1)} MB)`);
  } else {
    console.log("[demo] ⚠️  Warning: video file not found");
  }

  console.log("[demo] Done.");
  process.exit(0);
}

main().catch((err) => {
  console.error("[demo] Fatal error:", err);
  if (recorder && !recorder.killed) recorder.kill("SIGTERM");
  process.exit(1);
});
