import { cloudflareTest } from "@cloudflare/vitest-pool-workers";
import { defineConfig } from "vitest/config";

export default defineConfig({
  plugins: [
    cloudflareTest({
      wrangler: { configPath: "./wrangler.toml" },
      miniflare: { bindings: { RELAY_TOKEN: "rt", LLM_SECRET: "ls", WEBHOOK_SECRET: "ws", IDLE_MS: "300", GRACE_MS: "200", MAX_MS: "5000" } },
    }),
  ],
});
