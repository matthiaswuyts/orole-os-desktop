import { defineConfig } from "@playwright/test";

/** No webServer, browser launch, bridge shim, or parallel access to the installed app. */
export default defineConfig({
  testDir: "e2e/native",
  timeout: 90_000,
  expect: { timeout: 15_000 },
  workers: 1,
  fullyParallel: false,
  retries: 0,
  forbidOnly: !!process.env.CI,
  outputDir: "test-results/native",
  reporter: [["list"], ["html", { open: "never", outputFolder: "playwright-report/native" }]],
  projects: [{ name: "installed-webview2" }],
});
