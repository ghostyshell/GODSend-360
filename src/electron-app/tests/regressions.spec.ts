import { test, expect, chromium } from "@playwright/test";
import { spawn, type ChildProcess } from "child_process";
import path from "path";

const PORT = 1900 + (process.pid % 500);
const BASE = `http://localhost:${PORT}/?mock=1`;
let server: ChildProcess | null = null;

test.beforeAll(async () => {
  const appDir = path.resolve(__dirname, "..");
  server = spawn(path.join(appDir, "node_modules", ".bin", "vite"), ["--port", String(PORT), "--strictPort"], {
    cwd: appDir,
    stdio: "ignore",
  });
  const start = Date.now();
  while (Date.now() - start < 60_000) {
    try {
      if ((await fetch(`http://localhost:${PORT}/`)).ok) return;
    } catch {
      /* not up yet */
    }
    await new Promise((r) => setTimeout(r, 250));
  }
  throw new Error("dev server did not start");
});

test.afterAll(async () => {
  server?.kill("SIGTERM");
});

test("unslop-anything a11y pass", async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1440, height: 900 } });
    await page.goto(BASE, { waitUntil: "networkidle" });
    await page.waitForTimeout(2000);
    // Library: the title filter input has an accessible name.
    const libraryNav = page.locator('button[title="Xbox Library"]').first();
    if (await libraryNav.isVisible().catch(() => false)) await libraryNav.click();
    await page.waitForTimeout(800);
    // Every text input visible on the screen has an accessible name.
    const unnamed = await page.evaluate(() =>
      Array.from(document.querySelectorAll("input:not([type=hidden]):not([type=checkbox]):not([type=file])"))
        .filter((el) => {
          const i = el as HTMLInputElement;
          return !i.getAttribute("aria-label") && !i.labels?.length && !i.getAttribute("aria-labelledby");
        })
        .map((el) => (el as HTMLInputElement).placeholder),
    );
    expect(unnamed).toEqual([]);
  } finally {
    await browser.close();
  }
});
