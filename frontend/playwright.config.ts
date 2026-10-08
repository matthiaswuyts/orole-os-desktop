import { defineConfig } from "@playwright/test";

/** Plain-browser tests only; native WebView2 attachment has a separate opt-in config. */
export default defineConfig({
  testDir: "e2e",
  testIgnore: ["**/native/**"],
  timeout: 45_000,
  expect: {
    timeout: 10_000,
    toHaveScreenshot: { animations: "disabled", caret: "hide", maxDiffPixelRatio: 0.002 },
  },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report/browser" }]],
  outputDir: "test-results/browser",
  use: {
    baseURL: "http://127.0.0.1:5173",
    viewport: { width: 1360, height: 860 },
    locale: "en-US",
    timezoneId: "UTC",
    reducedMotion: "reduce",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  webServer: {
    command: "bun run dev --host 127.0.0.1 --port 5173 --strictPort",
    url: "http://127.0.0.1:5173",
    reuseExistingServer: !process.env.CI,
  },
});
