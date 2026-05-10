import { test, expect, _electron as electron } from "@playwright/test";
import path from "path";
import fs from "fs";
import os from "os";

/**
 * E2E test: verify the Export Aurora DBs feature works with a live Xbox.
 *
 * Requires Xbox to be on with Aurora FTP enabled (default password).
 * Requires Go binary built at dist/godsend-mac.
 *
 * This test mocks the folder picker dialog so it doesn't block on a native OS dialog.
 *
 * Run with:
 *   cd src/electron-app && npx playwright test tests/export-aurora-db.spec.ts --headed
 */

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

async function getOutputText(page: any): Promise<string> {
  const pre = page.locator("pre").first();
  const visible = await pre.isVisible().catch(() => false);
  if (!visible) return "";
  return await pre.innerText().catch(() => "");
}

test("Export Aurora DBs from console via FTP", async () => {
  test.setTimeout(120000);

  const repoRoot = path.resolve(__dirname, "../../..");
  const mainJs = path.resolve(__dirname, "../main.js");

  if (!fs.existsSync(mainJs)) {
    throw new Error("main.js not found. Run: cd src/electron-app && npm run tsc");
  }
  const goBinary = path.join(repoRoot, "dist", "godsend-mac");
  if (!fs.existsSync(goBinary)) {
    throw new Error("Go binary not found. Run: go build -C src/server -o ../../dist/godsend-mac .");
  }

  // Create a temp folder to receive the exported DBs
  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), "godsend-export-db-"));
  console.log("[test] Export destination:", tmpDir);

  console.log("[test] Launching app...");
  const electronApp = await electron.launch({
    executablePath: require("electron"),
    args: [mainJs],
    env: { ...process.env, NODE_ENV: "development" },
    timeout: 60000,
  });

  // Override dialog.showOpenDialog in the main process so it returns our tmpDir
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
  console.log("[test] Waiting for backend startup...");
  let backendReady = false;
  for (let i = 0; i < 60; i++) {
    const text = await getOutputText(page);
    if (text.includes("GODSend Backend Server")) {
      backendReady = true;
      console.log("[test] ✅ Backend started");
      break;
    }
    await sleep(1000);
  }
  expect(backendReady, "Backend should start").toBe(true);

  // ── Step 1: Settings → Save connection ───────────────────────────────────
  console.log("[test] Step 1: Settings → Save connection...");
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
    console.log("[test] ✅ Save connection clicked");
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
  expect(saveConfirmed, "Save connection should confirm").toBe(true);
  await sleep(2000);

  // ── Step 2: Click Export Aurora DBs ──────────────────────────────────────
  console.log("[test] Step 2: Clicking Export Aurora DBs...");
  const exportBtn = page.locator("button:has-text('Export Aurora DBs')").first();
  for (let i = 0; i < 15; i++) {
    if (await exportBtn.isVisible().catch(() => false)) {
      await exportBtn.click();
      console.log("[test] ✅ Export Aurora DBs clicked");
      break;
    }
    await sleep(1000);
  }

  // ── Step 3: Wait for export status ───────────────────────────────────────
  console.log("[test] Step 3: Waiting for export completion...");
  let exportDone = false;
  for (let i = 0; i < 60; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Aurora DBs exported to")) {
      exportDone = true;
      console.log("[test] ✅ Export completed");
      break;
    }
    if (bodyText.includes("Export failed")) {
      console.log("[test] ❌ Export failed early:", bodyText);
      break;
    }
    await sleep(1000);
  }
  expect(exportDone, "Export should complete successfully").toBe(true);

  // ── Step 4: Verify files on disk ─────────────────────────────────────────
  console.log("[test] Step 4: Verifying exported files...");
  const contentDbPath = path.join(tmpDir, "content.db");
  const settingsDbPath = path.join(tmpDir, "settings.db");

  // Give a little extra time for filesystem flush
  for (let i = 0; i < 10; i++) {
    if (fs.existsSync(contentDbPath) && fs.existsSync(settingsDbPath)) {
      break;
    }
    await sleep(500);
  }

  expect(fs.existsSync(contentDbPath), "content.db should exist").toBe(true);
  expect(fs.existsSync(settingsDbPath), "settings.db should exist").toBe(true);

  const contentSize = fs.statSync(contentDbPath).size;
  const settingsSize = fs.statSync(settingsDbPath).size;
  console.log(`[test] content.db: ${contentSize} bytes, settings.db: ${settingsSize} bytes`);

  expect(contentSize, "content.db should be non-empty").toBeGreaterThan(0);
  expect(settingsSize, "settings.db should be non-empty").toBeGreaterThan(0);

  console.log("[test] ✅ Export Aurora DBs test passed!");

  // Cleanup
  await electronApp.close();
  try {
    fs.unlinkSync(contentDbPath);
    fs.unlinkSync(settingsDbPath);
    fs.rmdirSync(tmpDir);
  } catch { /* ignore cleanup errors */ }
});
