import { defineConfig } from "@playwright/test";

/** E2E runs against the browser preview (vite dev). Specs that need the MyGo bridge install a shim inside the spec itself. */
export default defineConfig({
  testDir: "e2e",
  use: { baseURL: "http://localhost:5173", viewport: { width: 1360, height: 860 } },
  webServer: { command: "bun run dev", url: "http://localhost:5173", reuseExistingServer: true },
});
