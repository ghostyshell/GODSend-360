import { test, _electron as electron, Page } from "@playwright/test";
import path from "path";
import fs from "fs";

/**
 * Captures real PNG screenshots of the desktop app for the marketing site.
 * Output: `docs/site/screens/*.png` (published at
 * https://ghostyshell.github.io/GODSend-360/site/screens/<name>.png).
 *
 * Without a connected Xbox the Library / Queue buttons don't appear, so
 * those shots are skipped automatically — the rest still produces real,
 * non-mocked UI captures.
 *
 * Run with:
 *   npx playwright test tests/screenshots-for-site.spec.ts
 */

const OUT = path.resolve(__dirname, "../../../docs/site/screens");
fs.mkdirSync(OUT, { recursive: true });

const VIEWPORT = { width: 1440, height: 900 } as const;

async function shoot(page: Page, name: string) {
  await page.waitForTimeout(800);
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage: false });
  console.log(`captured: ${name}.png`);
}

async function clickTitle(page: Page, title: string): Promise<boolean> {
  const loc = page.locator(`button[title="${title}"], [title="${title}"]`).first();
  if (await loc.isVisible().catch(() => false)) {
    await loc.click();
    await page.waitForTimeout(700);
    return true;
  }
  return false;
}

async function clickInToolbox(page: Page, menuText: RegExp): Promise<boolean> {
  if (!(await clickTitle(page, "Toolbox"))) return false;
  await page.waitForTimeout(300);
  const item = page.locator("button").filter({ hasText: menuText }).first();
  if (await item.isVisible().catch(() => false)) {
    await item.click();
    await page.waitForTimeout(900);
    return true;
  }
  return false;
}

async function backHome(page: Page) {
  // Try the Console (back-to-home) button; fall back to Escape
  if (!(await clickTitle(page, "Console"))) {
    await page.keyboard.press("Escape");
    await page.waitForTimeout(500);
  }
}

test("capture site screenshots", async ({}, testInfo) => {
  testInfo.setTimeout(180_000);

  const electronApp = await electron.launch({
    args: [path.resolve(__dirname, "../main.js")],
    env: { ...process.env, NODE_ENV: "production" },
  });

  await electronApp.waitForEvent("window");
  const page = await electronApp.firstWindow();
  await page.setViewportSize(VIEWPORT);
  await page.waitForLoadState("domcontentloaded").catch(() => {});
  await page.waitForTimeout(4000); // backend spawn + first paint

  // ── 1. Home / live backend terminal ─────────────────────────────────────
  await shoot(page, "01-home-terminal");

  // ── 2. Settings ─────────────────────────────────────────────────────────
  if (await clickTitle(page, "Settings")) {
    await page.waitForTimeout(1500);
    await shoot(page, "02-settings");
    await backHome(page);
  }

  // ── 3. Browse & Download (store) ────────────────────────────────────────
  if (await clickTitle(page, "Browse & Download")) {
    await page.waitForTimeout(2000);
    await shoot(page, "03-browse-store");
    await backHome(page);
  }

  // ── 4. ISO to GOD (Toolbox) ─────────────────────────────────────────────
  if (await clickInToolbox(page, /ISO to GOD/i)) {
    await shoot(page, "04-iso-to-god");
    await backHome(page);
  }

  // ── 5. ISO to XEX (Toolbox) ─────────────────────────────────────────────
  if (await clickInToolbox(page, /ISO to XEX/i)) {
    await shoot(page, "05-iso-to-xex");
    await backHome(page);
  }

  // ── 6. BadAvatar USB (Toolbox) ──────────────────────────────────────────
  if (await clickInToolbox(page, /BadAvatar USB/i)) {
    await shoot(page, "06-badavatar-usb");
    await backHome(page);
  }

  // ── 7. FTP Manager (Toolbox) ────────────────────────────────────────────
  if (await clickInToolbox(page, /FTP Manager/i)) {
    await shoot(page, "07-ftp-manager");
    await backHome(page);
  }

  // ── 8. Xbox Library (only if Xbox connected) ───────────────────────────
  if (await clickTitle(page, "Xbox Library")) {
    await page.waitForTimeout(3000);
    await shoot(page, "08-xbox-library");
  } else {
    console.log("Xbox not connected — skipping Library shot.");
  }

  await electronApp.close();
});
