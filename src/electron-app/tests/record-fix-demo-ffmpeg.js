#!/usr/bin/env node
/**
 * Screen-recording demo of the icon/banner fix using ffmpeg.
 *
 * This script launches the Electron app, records the screen with ffmpeg,
 * navigates the UI to demonstrate the fix, and saves the final video.
 *
 * Prerequisites: ffmpeg installed (brew install ffmpeg)
 *
 * Usage:
 *   node tests/record-fix-demo-ffmpeg.js
 *
 * Output:
 *   test-results/fix-demo-video.mp4
 */

const { _electron: electron } = require("playwright");
const { spawn } = require("child_process");
const path = require("path");
const fs = require("fs");
const os = require("os");

const OUTPUT_DIR = path.resolve(__dirname, "../test-results");
const VIDEO_PATH = path.join(OUTPUT_DIR, "fix-demo-video.mp4");

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function startRecording(outputPath) {
  const platform = os.platform();
  let args;
  if (platform === "darwin") {
    // macOS AVFoundation screen capture (capture entire screen)
    args = [
      "-f", "avfoundation",
      "-i", "1:none", // screen 1, no audio
      "-pix_fmt", "yuv420p",
      "-r", "30",
      "-s", "2880x1800", // retina resolution; scale later
      "-vf", "scale=1440:900",
      "-y",
      outputPath,
    ];
  } else if (platform === "win32") {
    args = [
      "-f", "gdigrab",
      "-i", "desktop",
      "-pix_fmt", "yuv420p",
      "-r", "30",
      "-y",
      outputPath,
    ];
  } else {
    args = [
      "-f", "x11grab",
      "-i", ":0.0",
      "-pix_fmt", "yuv420p",
      "-r", "30",
      "-y",
      outputPath,
    ];
  }
  const proc = spawn("ffmpeg", args, { stdio: "ignore" });
  return proc;
}

async function main() {
  fs.mkdirSync(OUTPUT_DIR, { recursive: true });

  // Remove old video
  try { fs.unlinkSync(VIDEO_PATH); } catch {}

  console.log("[demo] Starting screen recording with ffmpeg...");
  const recorder = await startRecording(VIDEO_PATH);

  // Give ffmpeg a moment to start
  await sleep(1500);

  console.log("[demo] Launching Electron app...");
  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: {
      ...process.env,
      NODE_ENV: "development",
    },
  });

  await electronApp.waitForEvent("window");
  const page = await electronApp.firstWindow();
  await page.setViewportSize({ width: 1440, height: 900 });

  console.log("[demo] Waiting for app to initialise...");
  await sleep(5000);

  // ── Step 1: Navigate to Library ──────────────────────────────────────────
  console.log("[demo] Step 1: Navigate to Xbox Library");
  const libraryBtn = page.locator("button, a").filter({ hasText: /Library/i }).first();
  if (await libraryBtn.isVisible().catch(() => false)) {
    await libraryBtn.click();
    await sleep(3000);
  }

  // ── Step 2: Wait for grid ───────────────────────────────────────────────
  console.log("[demo] Step 2: Wait for game grid");
  await page
    .waitForSelector("[class*='cover'], [class*='game']", { timeout: 20000 })
    .catch(() => {
      console.log("[demo] Grid not loaded (FTP offline) — showing UI state");
    });

  // ── Step 3: Click first game card ───────────────────────────────────────
  console.log("[demo] Step 3: Open first game detail");
  const firstCard = page.locator("[class*='cover'], [class*='game']").first();
  if (await firstCard.isVisible().catch(() => false)) {
    await firstCard.click();
    await sleep(3000);
  }

  // ── Step 4: Scroll to Assets section ────────────────────────────────────
  console.log("[demo] Step 4: Scroll to Aurora Assets");
  const assetsHeading = page.locator("text=Assets, text=Asset").first();
  if (await assetsHeading.isVisible().catch(() => false)) {
    await assetsHeading.scrollIntoViewIfNeeded();
    await sleep(2000);
  }

  // ── Step 5: Show Icon slot ──────────────────────────────────────────────
  console.log("[demo] Step 5: Show Icon slot");
  const iconSlot = page.locator("text=Icon").first();
  if (await iconSlot.isVisible().catch(() => false)) {
    await iconSlot.scrollIntoViewIfNeeded();
    await sleep(2000);

    const searchBtn = page.locator("button").filter({ hasText: /Search/i }).first();
    if (await searchBtn.isVisible().catch(() => false)) {
      await searchBtn.click();
      await sleep(3000);

      // Close modal
      const closeBtn = page.locator("button").filter({ hasText: /Close|×|X/i }).first();
      if (await closeBtn.isVisible().catch(() => false)) {
        await closeBtn.click();
        await sleep(1000);
      }
    }
  }

  // ── Step 6: Show Banner slot ──────────────────────────────────────────────
  console.log("[demo] Step 6: Show Banner slot");
  const bannerSlot = page.locator("text=Banner").first();
  if (await bannerSlot.isVisible().catch(() => false)) {
    await bannerSlot.scrollIntoViewIfNeeded();
    await sleep(2500);
  }

  // ── Step 7: End card ────────────────────────────────────────────────────
  console.log("[demo] Step 7: End card");
  await sleep(3000);

  console.log("[demo] Closing app and stopping recording...");
  await electronApp.close();

  // Stop ffmpeg gracefully
  recorder.kill("SIGINT");
  await sleep(2000);

  // Ensure ffmpeg exits
  if (!recorder.killed) {
    recorder.kill("SIGTERM");
    await sleep(1000);
  }

  if (fs.existsSync(VIDEO_PATH)) {
    const stats = fs.statSync(VIDEO_PATH);
    console.log(`[demo] Video saved: ${VIDEO_PATH} (${(stats.size / 1024 / 1024).toFixed(1)} MB)`);
  } else {
    console.log("[demo] Warning: video file not found — ffmpeg may have failed");
  }

  console.log("[demo] Done.");
}

main().catch((err) => {
  console.error("[demo] Error:", err);
  process.exit(1);
});
