import { test, expect, _electron as electron } from "@playwright/test";
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
    const output = page.locator("pre, [class*='output']").first();
    if (await output.isVisible().catch(() => false)) {
      const text = await output.innerText().catch(() => "");
      if (text.includes("GODSend Backend Server")) {
        backendReady = true;
        console.log("[test] ✅ Backend started");
        break;
      }
    }
    await page.waitForTimeout(1000);
  }
  expect(backendReady, "Backend should start").toBe(true);

  // ── Step 1: Reconnect to Xbox ────────────────────────────────────────────
  console.log("[test] Step 1: Clicking Reconnect to ping Xbox...");
  const reconnectBtn = page.locator("button[title='Retry FTP connection']").first();
  if (await reconnectBtn.isVisible().catch(() => false)) {
    await reconnectBtn.click();
    console.log("[test] Clicked Reconnect");
  }

  // Wait for reconnection
  await page.waitForTimeout(5000);
  await page.screenshot({ path: path.join(__dirname, "../test-results", "01-after-reconnect.png") });

  // ── Step 2: Wait for Library button to appear ────────────────────────────
  console.log("[test] Step 2: Waiting for Library button to appear...");
  let libraryBtnVisible = false;
  let libraryBtn = page.locator("button[title='Xbox Library']").first();

  for (let i = 0; i < 20; i++) {
    if (await libraryBtn.isVisible().catch(() => false)) {
      libraryBtnVisible = true;
      console.log("[test] ✅ Library button visible");
      break;
    }
    await page.waitForTimeout(1000);
  }

  // ── Step 3: Click Library button ─────────────────────────────────────────
  if (libraryBtnVisible) {
    console.log("[test] Step 3: Clicking Library button...");
    await libraryBtn.click();
    console.log("[test] Clicked Library");
  } else {
    console.log("[test] ❌ Library button never appeared — Xbox may be off");
  }

  // ── Step 4: Wait for game grid ───────────────────────────────────────────
  console.log("[test] Step 4: Waiting for game grid (up to 60s)...");
  let onLibrary = false;
  for (let i = 0; i < 60; i++) {
    const cover = page.locator("img[class*='cover']").first();
    if (await cover.isVisible().catch(() => false)) {
      onLibrary = true;
      console.log("[test] ✅ Game grid loaded at attempt", i);
      break;
    }
    await page.waitForTimeout(1000);
  }

  await page.screenshot({ path: path.join(__dirname, "../test-results", "02-library-grid.png") });
  expect(onLibrary, "Library should load with game covers").toBe(true);

  // ── Step 5: Click first game card ────────────────────────────────────────
  console.log("[test] Step 5: Opening first game...");
  const firstCard = page.locator("img[class*='cover']").first();
  await firstCard.click();
  await page.waitForTimeout(3000);
  await page.screenshot({ path: path.join(__dirname, "../test-results", "03-game-detail.png") });

  // ── Step 6: Scroll to Aurora Assets ──────────────────────────────────────
  console.log("[test] Step 6: Scrolling to Aurora Assets...");
  const assets = page.locator("text=Assets").first();
  if (await assets.isVisible().catch(() => false)) {
    await assets.scrollIntoViewIfNeeded();
  }
  await page.waitForTimeout(2000);

  // ── Step 7: Verify Icon and Banner ───────────────────────────────────────
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

  await page.screenshot({ path: path.join(__dirname, "../test-results", "04-assets-section.png") });

  expect(iconVisible, "Icon slot should be visible").toBe(true);
  expect(bannerVisible, "Banner slot should be visible").toBe(true);

  console.log("[test] ✅ Fix verified — both Icon and Banner slots present!");
  await electronApp.close();
});
