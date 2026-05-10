import { defineConfig, devices } from "@playwright/test";
import path from "path";

/**
 * Playwright configuration for GODsend Electron app E2E testing.
 *
 * Usage:
 *   npx playwright test                    # run all tests
 *   npx playwright test --ui               # interactive UI mode
 *   npx playwright test --project=chromium --headed  # headed mode
 */
export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: "html",
  use: {
    trace: "on-first-retry",
    video: "on",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "electron",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 900 },
      },
    },
  ],
});
