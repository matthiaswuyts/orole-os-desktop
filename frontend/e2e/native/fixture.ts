import { readFile } from "node:fs/promises";
import { isAbsolute, resolve } from "node:path";
import { chromium, expect, test as base, type Page } from "@playwright/test";

interface LaunchManifest { endpoint: string; home: string; token: string; appPid: number; daemonPid: number }

/** Calls the installed application's existing bridge; never installs or replaces a global. */
export async function nativeCall<T>(page: Page, method: string, ...args: unknown[]): Promise<T> {
  return page.evaluate(async ({ method, args }) => {
    if (!("mygo" in globalThis)) throw new Error("Installed MyGo bridge is missing");
    const runtime = globalThis.mygo;
    if (!runtime || typeof runtime !== "object" || !("call" in runtime) || typeof runtime.call !== "function") throw new Error("Installed MyGo bridge is missing");
    return runtime.call(method, ...args);
  }, { method, args }) as Promise<T>;
}

export const test = base.extend<{ nativePage: Page }>({
  nativePage: async ({}, provide, testInfo) => {
    if (process.platform !== "win32") throw new Error("Installed WebView2 tests require Windows");
    if (process.env.OROLE_E2E_ALLOW_ISOLATED_HOME !== "1") throw new Error("Explicit isolated-home opt-in is required; use scripts/test-installed-e2e.ps1");
    const rawEndpoint = process.env.OROLE_E2E_CDP_ENDPOINT;
    const home = process.env.OROLE_HOME;
    const manifestPath = process.env.OROLE_E2E_MANIFEST;
    if (!rawEndpoint || !home || !manifestPath || !isAbsolute(home)) throw new Error("An explicit CDP endpoint, absolute isolated OROLE_HOME and launch manifest are required");
    const endpoint = new URL(rawEndpoint);
    if (endpoint.protocol !== "http:" || !["127.0.0.1", "[::1]"].includes(endpoint.hostname) || !endpoint.port || endpoint.username || endpoint.password || endpoint.pathname !== "/" || endpoint.search || endpoint.hash) {
      throw new Error("CDP must use an explicit numeric loopback HTTP endpoint with a port and no credentials/path/query");
    }
    const manifest = JSON.parse((await readFile(manifestPath, "utf8")).replace(/^\uFEFF/, "")) as LaunchManifest;
    if (manifest.endpoint !== rawEndpoint || resolve(manifest.home) !== resolve(home) || !/^[a-f0-9]{32}$/.test(manifest.token) || !Number.isSafeInteger(manifest.appPid) || !Number.isSafeInteger(manifest.daemonPid)) {
      throw new Error("The launch manifest does not match this isolated run");
    }
    const browser = await chromium.connectOverCDP(rawEndpoint);
    // Do not create contexts/tabs or close the attached browser, context or page. The
    // launcher owns process lifetime. Worker exit drops only the CDP connection.
    const candidates: Page[] = [];
    for (const context of browser.contexts()) {
      for (const page of context.pages()) {
        if (await page.title() === "Orole OS") candidates.push(page);
      }
    }
    if (candidates.length !== 1) throw new Error(`Expected one installed Orole OS page, found ${candidates.length}`);
    const page = candidates[0]!;
    await expect.poll(async () => (await nativeCall<Record<string, unknown>>(page, "Settings.All"))["acceptance.launchToken"]).toBe(manifest.token);
    const health = await nativeCall<{ pid: number }>(page, "Daemon.Health");
    expect(health.pid, "Bridge must reach the launcher-owned isolated daemon").toBe(manifest.daemonPid);
    await expect(page.getByRole("navigation", { name: "Applications" })).toBeVisible();
    await expect(page.getByRole("complementary", { name: "Browser preview status" })).toHaveCount(0);
    try {
      await provide(page);
    } finally {
      if (testInfo.status !== testInfo.expectedStatus && !page.isClosed()) {
        await testInfo.attach("installed-webview2", { body: await page.screenshot(), contentType: "image/png" });
      }
    }
  },
});

export { expect };
