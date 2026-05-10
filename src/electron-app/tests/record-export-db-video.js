#!/usr/bin/env node
/**
 * Video recorder / manual test for the Export Aurora DBs feature.
 *
 * Flow:
 *   1. Launch app with video recording
 *   2. Wait for backend startup
 *   3. Settings → Save connection
 *   4. Click Export Aurora DBs
 *   5. Mock dialog to auto-select temp folder
 *   6. Wait for export completion
 *   7. Verify files exist on disk
 *   8. Close app → video saved to test-results/
 *
 * Run: cd src/electron-app && node tests/record-export-db-video.js
 */

const { _electron: electron } = require("playwright");
const path = require("path");
const fs = require("fs");
const os = require("os");

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

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "godsend-export-db-"));
  console.log("[video] Export destination:", tmpDir);

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

  // Override dialog.showOpenDialog so it auto-returns our tmpDir
  await electronApp.evaluate(({ dialog }, dest) => {
    dialog.showOpenDialog = async () => ({
      canceled: false,
      filePaths: [dest],
      bookmarks: [],
    });
  }, tmpDir);

  const page = await electronApp.firstWindow();
  await page.setViewportSize({ width: 1440, height: 900 });

  // ── Wait for backend startup ─────────────────────────────────────────────
  console.log("[video] Waiting for backend startup...");
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
    console.error("[video] ❌ Backend did not start in time");
    await electronApp.close();
    process.exit(1);
  }

  // ── Step 1: Settings → Save connection ───────────────────────────────────
  console.log("[video] Step 1: Settings → Save connection...");
  const settingsBtn = page.locator("button[title='Settings']").first();
  for (let i = 0; i < 15; i++) {
    if (await settingsBtn.isVisible().catch(() => false)) {
      await settingsBtn.click();
      break;
    }
    await sleep(1000);
  }
  await sleep(2000);

  const saveConnBtn = page.locator("button:has-text('Save connection')").first();
  if (await saveConnBtn.isVisible().catch(() => false)) {
    await saveConnBtn.click();
    console.log("[video] ✅ Save connection clicked");
  }

  let saveConfirmed = false;
  for (let i = 0; i < 15; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Saved. Backend restarted")) {
      saveConfirmed = true;
      break;
    }
    await sleep(1000);
  }
  if (!saveConfirmed) {
    console.error("[video] ⚠️ Save connection confirmation not seen, continuing anyway...");
  }
  await sleep(4000);

  // ── Step 2: Scroll to Export Aurora DBs ──────────────────────────────────
  console.log("[video] Step 2: Looking for Export Aurora DBs button...");

  // Debug: check if the button text exists in the body
  const bodyBefore = await page.innerText("body").catch(() => "");
  const hasExportText = bodyBefore.includes("Export Aurora DBs");
  console.log("[video] Body contains 'Export Aurora DBs':", hasExportText);

  let exportBtn = page.locator("button:has-text('Export Aurora DBs')").first();
  let exportVisible = await exportBtn.isVisible().catch(() => false);

  // Try scrolling within the settings scroll area to bring the button into view
  if (!exportVisible) {
    console.log("[video] Button not visible, scrolling settings page...");
    await page.evaluate(() => {
      const scrollArea = document.querySelector('[class*="ScrollArea"] [class*="viewport"]');
      if (scrollArea) scrollArea.scrollTop = scrollArea.scrollHeight;
    });
    await sleep(1000);
    exportVisible = await exportBtn.isVisible().catch(() => false);
  }

  if (exportVisible) {
    await exportBtn.scrollIntoViewIfNeeded();
    await sleep(500);
    await exportBtn.click();
    console.log("[video] ✅ Export Aurora DBs clicked");
  } else {
    console.error("[video] ❌ Export Aurora DBs button not found");
    await electronApp.close();
    process.exit(1);
  }

  // ── Step 3: Wait for export completion ───────────────────────────────────
  console.log("[video] Step 3: Waiting for export completion (up to 120s)...");
  let exportDone = false;
  for (let i = 0; i < 120; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Exported to:")) {
      exportDone = true;
      console.log("[video] ✅ Export completed");
      break;
    }
    if (bodyText.includes("Export failed")) {
      console.error("[video] ❌ Export failed detected in UI");
      break;
    }
    await sleep(1000);
  }

  if (!exportDone) {
    console.error("[video] ❌ Export did not complete successfully (timeout or failure)");
  }

  // ── Step 4: Verify files on disk ─────────────────────────────────────────
  console.log("[video] Step 4: Verifying exported files...");
  const contentDbPath = path.join(tmpDir, "content.db");
  const settingsDbPath = path.join(tmpDir, "settings.db");

  for (let i = 0; i < 10; i++) {
    if (fs.existsSync(contentDbPath) && fs.existsSync(settingsDbPath)) {
      break;
    }
    await sleep(500);
  }

  if (!fs.existsSync(contentDbPath) || !fs.existsSync(settingsDbPath)) {
    console.error("[video] ❌ Exported files not found on disk");
    await electronApp.close();
    process.exit(1);
  }

  const contentSize = fs.statSync(contentDbPath).size;
  const settingsSize = fs.statSync(settingsDbPath).size;
  console.log(`[video] content.db: ${contentSize} bytes, settings.db: ${settingsSize} bytes`);

  // Pause briefly so the success state is visible in the video
  await sleep(3000);

  console.log("[video] ✅ All checks passed. Closing app...");
  await electronApp.close();

  // Find video file
  const videos = fs.readdirSync(OUTPUT_DIR).filter((f) => f.endsWith(".webm") || f.endsWith(".mp4"));
  if (videos.length > 0) {
    const latest = videos
      .map((f) => ({ name: f, time: fs.statSync(path.join(OUTPUT_DIR, f)).mtimeMs }))
      .sort((a, b) => b.time - a.time)[0];
    console.log("[video] 🎬 Video saved:", path.join(OUTPUT_DIR, latest.name));
  }

  // Cleanup temp files
  try {
    fs.unlinkSync(contentDbPath);
    fs.unlinkSync(settingsDbPath);
    fs.rmdirSync(tmpDir);
  } catch { /* ignore cleanup errors */ }

  console.log("[video] Done.");
  process.exit(0);
}

main().catch((err) => {
  console.error("[video] Fatal error:", err);
  process.exit(1);
});
