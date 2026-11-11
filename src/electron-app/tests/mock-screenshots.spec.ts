import { test, expect, chromium, Page } from "@playwright/test";
import { spawn, type ChildProcess } from "child_process";
import path from "path";
import fs from "fs";

/**
 * Captures the marketing screenshots from the renderer fixture (`?mock=1`).
 *
 * The fixture bridge (renderer/mock.ts) answers every channel the app needs
 * with a realistic Aurora library, so the shots show the interface exactly as
 * it looks with a console connected, without needing one. Pixels are the real
 * renderer; nothing is drawn over them.
 *
 * Output: docs/site/screens/*.png
 *
 *   npx playwright test tests/mock-screenshots.spec.ts
 */

const OUT = path.resolve(__dirname, "../../../docs/site/screens");
const PORT = 1400 + (process.pid % 500);
const BASE = `http://localhost:${PORT}/?mock=1`;
const VIEWPORT = { width: 1440, height: 900 } as const;

fs.mkdirSync(OUT, { recursive: true });

let server: ChildProcess | null = null;

async function waitForServer(url: string, timeoutMs = 60_000) {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    try {
      const res = await fetch(url);
      if (res.ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error(`dev server did not start at ${url}`);
}

async function shoot(page: Page, name: string) {
  await page.waitForTimeout(800);
  await page.screenshot({ path: path.join(OUT, `${name}.png`), fullPage: false });
  console.log(`captured: ${name}.png`);
}

async function clickTitle(page: Page, title: string): Promise<boolean> {
  const loc = page.locator(`button[title="${title}"]`).first();
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
  await clickTitle(page, "Console");
}

test.beforeAll(async () => {
  const appDir = path.resolve(__dirname, "..");
  server = spawn(
    path.join(appDir, "node_modules", ".bin", "vite"),
    ["--port", String(PORT), "--strictPort"],
    { cwd: appDir, stdio: "ignore", detached: false }
  );
  // A spawn failure must fail the test, not leave waitForServer polling a
  // stale server that happens to answer on the port.
  server.on("error", (err) => {
    throw err;
  });
  await waitForServer(`http://localhost:${PORT}/`);
});

test.afterAll(async () => {
  if (!server || server.exitCode !== null) return;
  const exited = new Promise<void>((resolve) => server!.once("exit", () => resolve()));
  server.kill("SIGTERM");
  await Promise.race([exited, new Promise((r) => setTimeout(r, 5000))]);
});

test("capture site screenshots from the fixture", async ({}, testInfo) => {
  testInfo.setTimeout(180_000);

  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: VIEWPORT });
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForTimeout(2200); // cover events land in a small stagger

    // The fixture must actually be driving the page, and covers must decode.
    expect(await page.evaluate(() => (window as any).__GODSEND_MOCK__?.games?.length)).toBe(24);
    const coverCount = await page.locator("[style*='background-image']").count();
    expect(coverCount).toBeGreaterThan(0);

    // ── 1. Xbox Library (hero) ──────────────────────────────────────────────
    await shoot(page, "08-xbox-library");

    // ── 2. Home / live backend terminal ─────────────────────────────────────
    if (await clickTitle(page, "Console")) await shoot(page, "01-home-terminal");

    // ── 3. Browse & Download (store) ────────────────────────────────────────
    if (await clickTitle(page, "Browse & Download")) await shoot(page, "03-browse-store");
    await backHome(page);

    // ── 4. ISO to GOD (Toolbox) ─────────────────────────────────────────────
    if (await clickInToolbox(page, /ISO to GOD/i)) await shoot(page, "04-iso-to-god");
    await backHome(page);

    // ── 5. ISO to XEX (Toolbox) ─────────────────────────────────────────────
    if (await clickInToolbox(page, /ISO to XEX/i)) await shoot(page, "05-iso-to-xex");
    await backHome(page);

    // ── 6. BadAvatar USB (Toolbox) ──────────────────────────────────────────
    if (await clickInToolbox(page, /BadAvatar USB/i)) await shoot(page, "06-badavatar-usb");
    await backHome(page);

    // ── 7. FTP Manager (Toolbox) ────────────────────────────────────────────
    if (await clickInToolbox(page, /FTP Manager/i)) await shoot(page, "07-ftp-manager");
  } finally {
    await browser.close();
  }
});
