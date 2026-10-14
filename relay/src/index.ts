import type { HubEnv } from "./hub";
export { Hub } from "./hub";

export interface Env extends HubEnv {
  HUB: DurableObjectNamespace;
  RELAY_TOKEN: string;
  LLM_SECRET: string;
  WEBHOOK_SECRET: string;
}

function safeEqual(a: string, b: string): boolean {
  if (!a || !b) return false;
  const x = new TextEncoder().encode(a);
  const y = new TextEncoder().encode(b);
  if (x.length !== y.length) return false;
  let d = 0;
  for (let i = 0; i < x.length; i++) d |= x[i] ^ y[i];
  return d === 0;
}

async function hmacHex(secret: string, data: string): Promise<string> {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(data));
  return [...new Uint8Array(sig)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

async function validSignature(header: string | null, body: string, secret: string): Promise<boolean> {
  if (!header) return false;
  const parts: Record<string, string> = Object.fromEntries(header.split(",").map((p) => p.split("=", 2) as [string, string]));
  const ts = Number(parts.t);
  if (!parts.v0 || !Number.isFinite(ts) || Math.abs(Date.now() / 1000 - ts) > 30 * 60) return false;
  return safeEqual(await hmacHex(secret, `${parts.t}.${body}`), parts.v0);
}

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    const url = new URL(req.url);
    const hub = env.HUB.get(env.HUB.idFromName("hub"));
    const notConfigured = () => new Response("relay not configured", { status: 503 });
    switch (url.pathname) {
      case "/health":
        return hub.fetch(req);
      case "/daemon":
        if (!env.RELAY_TOKEN) return notConfigured();
        if (!safeEqual(req.headers.get("Authorization") ?? "", `Bearer ${env.RELAY_TOKEN}`)) return new Response("unauthorized", { status: 401 });
        return hub.fetch(req);
      case "/llm/chat/completions":
      case "/inbound":
        if (!env.LLM_SECRET) return notConfigured();
        if (req.method !== "POST" || !safeEqual(req.headers.get("x-orole-relay") ?? "", env.LLM_SECRET)) return new Response("unauthorized", { status: 401 });
        return hub.fetch(req);
      case "/status": {
        if (!env.WEBHOOK_SECRET) return notConfigured();
        if (req.method !== "POST") return new Response("unauthorized", { status: 401 });
        const body = await req.text();
        if (!(await validSignature(req.headers.get("ElevenLabs-Signature"), body, env.WEBHOOK_SECRET))) return new Response("unauthorized", { status: 401 });
        return hub.fetch(new Request(req.url, { method: "POST", body }));
      }
    }
    return new Response("not found", { status: 404 });
  },
};
