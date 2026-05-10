import { test, expect, _electron as electron, Page } from "@playwright/test";
import path from "path";
import fs from "fs";

/**
 * E2E test: verify the icon/banner fix works with a live Xbox at 192.168.1.229.
 *
 * Requires Xbox to be on with Aurora FTP enabled.
 * Requires Go binary built at dist/godsend-mac.
 *
 * Run with:
 *   cd src/electron-app && npx playwright test tests/icon-banner-fix.spec.ts --headed
 */

test.setTimeout(180000);

function sleep(ms: number) {
  return new Promise((r) => setTimeout(r, ms));
}

async function getOutputText(page: any): Promise<string> {
  const pre = page.locator("pre").first();
  const visible = await pre.isVisible().catch(() => false);
  if (!visible) return "";
  return await pre.innerText().catch(() => "");
}

test("icon/banner fix demo with live Xbox", async () => {
  const repoRoot = path.resolve(__dirname, "../../..");
  const mainJs = path.resolve(__dirname, "../main.js");

  if (!fs.existsSync(mainJs)) {
    throw new Error(`main.js not found. Run: cd src/electron-app && npm run tsc`);
  }
  const goBinary = path.join(repoRoot, "dist", "godsend-mac");
  if (!fs.existsSync(goBinary)) {
    throw new Error(`Go binary not found. Run: go build -C src/server -o ../../dist/godsend-mac .`);
  }

  console.log("[test] Launching app...");
  const electronApp = await electron.launch({
    executablePath: require("electron"),
    args: [mainJs],
    env: { ...process.env, NODE_ENV: "development" },
    timeout: 60000,
  });

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

  // ── Step 2: Home → Reconnect ─────────────────────────────────────────────
  console.log("[test] Step 2: Home → Reconnect...");
  const homeBtn = page.locator("button[title='Home'], button[title='Console']").first();
  if (await homeBtn.isVisible().catch(() => false)) {
    await homeBtn.click();
    await sleep(500);
  }

  const reconnectBtn = page.locator("button[title='Retry FTP connection']").first();
  for (let i = 0; i < 10; i++) {
    if (await reconnectBtn.isVisible().catch(() => false)) {
      await reconnectBtn.click();
      console.log("[test] ✅ Reconnect clicked");
      break;
    }
    await sleep(500);
  }

  let ftpConnected = false;
  for (let i = 0; i < 60; i++) {
    const text = await getOutputText(page);
    if (text.includes("connected") || text.includes("disconnected")) {
      ftpConnected = true;
      break;
    }
    await sleep(1000);
  }
  expect(ftpConnected, "FTP should connect or report status").toBe(true);

  // ── Step 3: Library ────────────────────────────────────────────────────
  console.log("[test] Step 3: Opening Library...");
  const libraryBtn = page.locator("button[title='Xbox Library']").first();
  for (let i = 0; i < 30; i++) {
    if (await libraryBtn.isVisible().catch(() => false)) {
      await libraryBtn.click();
      console.log("[test] ✅ Library clicked");
      break;
    }
    await sleep(1000);
  }

  let onLibrary = false;
  for (let i = 0; i < 90; i++) {
    const bodyText = await page.innerText("body").catch(() => "");
    if (bodyText.includes("Xbox Library") && /\d+\s+games/.test(bodyText)) {
      onLibrary = true;
      console.log("[test] ✅ Library loaded");
      break;
    }
    await sleep(1000);
  }
  expect(onLibrary, "Library should load with games").toBe(true);

  // ── Step 4: Open first game ────────────────────────────────────────────
  console.log("[test] Step 4: Opening first game...");
  await page.evaluate(() => {
    const grid = document.querySelector('div[class*="grid"]');
    const firstCard = grid?.querySelector('button');
    if (firstCard) firstCard.click();
  });
  await sleep(3000);

  // ── Step 5: Scroll to Aurora Assets ────────────────────────────────────
  console.log("[test] Step 5: Scrolling to Aurora Assets...");
  const assetsHeader = page.locator("text=Aurora Assets").first();
  if (await assetsHeader.isVisible().catch(() => false)) {
    await assetsHeader.scrollIntoViewIfNeeded();
  }
  await sleep(2000);

  // ── Step 6: Verify Icon and Banner exist ───────────────────────────────
  const icon = page.locator("text=Icon").first();
  const banner = page.locator("text=Banner").first();
  const iconVisible = await icon.isVisible().catch(() => false);
  const bannerVisible = await banner.isVisible().catch(() => false);

  if (iconVisible) {
    await icon.scrollIntoViewIfNeeded();
    console.log("[test] ✅ Icon visible");
  }
  if (bannerVisible) {
    await banner.scrollIntoViewIfNeeded();
    console.log("[test] ✅ Banner visible");
  }

  expect(iconVisible, "Icon slot should be visible").toBe(true);
  expect(bannerVisible, "Banner slot should be visible").toBe(true);

  console.log("[test] ✅ Fix verified — both Icon and Banner slots present!");
  await electronApp.close();
});
