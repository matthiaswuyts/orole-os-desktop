import { SELF, env } from "cloudflare:test";
import worker, { type Env } from "../src/index";
import { describe, expect, it } from "vitest";

async function daemon() {
  const res = await SELF.fetch("https://relay/daemon", { headers: { Upgrade: "websocket", Authorization: "Bearer rt" } });
  expect(res.status).toBe(101);
  const ws = res.webSocket!;
  ws.accept();
  return ws;
}

async function sign(body: string, ts = Math.floor(Date.now() / 1000), secret = "ws") {
  const key = await crypto.subtle.importKey("raw", new TextEncoder().encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign"]);
  const sig = await crypto.subtle.sign("HMAC", key, new TextEncoder().encode(`${ts}.${body}`));
  return `t=${ts},v0=${[...new Uint8Array(sig)].map((b) => b.toString(16).padStart(2, "0")).join("")}`;
}

const llmBody = JSON.stringify({
  model: "bot",
  stream: true,
  messages: [{ role: "user", content: "what's running?" }],
  elevenlabs_extra_body: { call_id: "c1" },
});

describe("auth", () => {
  it("rejects a daemon without the token", async () => {
    const res = await SELF.fetch("https://relay/daemon", { headers: { Upgrade: "websocket", Authorization: "Bearer nope" } });
    expect(res.status).toBe(401);
  });
  it("rejects llm calls without the secret header", async () => {
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", body: llmBody });
    expect(res.status).toBe(401);
  });
  it("rejects post-call webhooks with a bad signature", async () => {
    const res = await SELF.fetch("https://relay/status", { method: "POST", headers: { "ElevenLabs-Signature": "t=1,v0=00" }, body: "{}" });
    expect(res.status).toBe(401);
  });
});

describe("health", () => {
  it("reports daemon presence without secrets", async () => {
    const res = await SELF.fetch("https://relay/health");
    expect(res.status).toBe(200);
    expect(Object.keys((await res.json()) as object)).toEqual(["daemon"]);
  });
});

describe("llm", () => {
  it("speaks a goodbye when no daemon is connected", async () => {
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    expect(res.headers.get("content-type")).toContain("text/event-stream");
    const text = await res.text();
    expect(text).toContain("can't reach your bots right now");
    expect(text).toContain("data: [DONE]");
    expect(text.toLowerCase()).not.toContain("orole");
  });

  it("forwards a turn to the daemon and streams its deltas as SSE", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type !== "llm.request") return;
      expect(f.callId).toBe("c1");
      expect(f.messages.at(-1).content).toBe("what's running?");
      ws.send(JSON.stringify({ type: "llm.delta", id: f.id, text: "Two tasks " }));
      ws.send(JSON.stringify({ type: "llm.delta", id: f.id, text: "are running." }));
      ws.send(JSON.stringify({ type: "llm.done", id: f.id }));
    });
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    const text = await res.text();
    expect(text).toContain('"content":"Two tasks "');
    expect(text).toContain('"content":"are running."');
    expect(text.trim().endsWith("data: [DONE]")).toBe(true);
    ws.close();
  });
});

describe("inbound", () => {
  it("returns the daemon's answer as conversation initiation data", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id, accept: true, vars: { call_id: "c9", bot_name: "Atlas" } }));
    });
    const res = await SELF.fetch("https://relay/inbound", {
      method: "POST", headers: { "x-orole-relay": "ls" },
      body: JSON.stringify({ caller_id: "+32470000000", called_number: "+3220000000", call_sid: "CA1", agent_id: "ag" }),
    });
    expect(res.status).toBe(200);
    const body = (await res.json()) as { type: string; dynamic_variables: Record<string, string> };
    expect(body.type).toBe("conversation_initiation_client_data");
    expect(body.dynamic_variables.call_id).toBe("c9");
    ws.close();
  });
  it("rejects when the daemon refuses", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id, accept: false }));
    });
    const res = await SELF.fetch("https://relay/inbound", { method: "POST", headers: { "x-orole-relay": "ls" }, body: JSON.stringify({ caller_id: "+1" }) });
    expect(res.status).toBe(403);
    ws.close();
  });
  it("treats a missing accept field as a rejection", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id }));
    });
    const res = await SELF.fetch("https://relay/inbound", { method: "POST", headers: { "x-orole-relay": "ls" }, body: JSON.stringify({ caller_id: "+1" }) });
    expect(res.status).toBe(403);
    ws.close();
  });
});

describe("status", () => {
  it("forwards a validly signed post-call webhook as a call.status frame", async () => {
    const ws = await daemon();
    const got = new Promise<Record<string, unknown>>((resolve) => {
      ws.addEventListener("message", (e) => {
        const f = JSON.parse(e.data as string);
        if (f.type === "call.status") resolve(f);
      });
    });
    const body = JSON.stringify({
      data: { conversation_id: "conv1", status: "done", metadata: { call_duration_secs: 42 }, conversation_initiation_client_data: { dynamic_variables: { call_id: "c9" } } },
    });
    const res = await SELF.fetch("https://relay/status", { method: "POST", headers: { "ElevenLabs-Signature": await sign(body) }, body });
    expect(res.status).toBe(200);
    expect(await got).toMatchObject({ type: "call.status", callId: "c9", conversationId: "conv1", status: "done", durationSecs: 42 });
    ws.close();
  });
});

describe("fail closed without secrets", () => {
  const unset = { ...env, RELAY_TOKEN: "", LLM_SECRET: "", WEBHOOK_SECRET: "" } as unknown as Env;
  const hit = (path: string, init: RequestInit) => worker.fetch(new Request(`https://relay${path}`, init), unset);

  it("refuses /daemon with empty or 'Bearer undefined' tokens", async () => {
    for (const Authorization of ["Bearer ", "Bearer undefined", ""]) {
      const res = await hit("/daemon", { headers: { Upgrade: "websocket", Authorization } });
      expect(res.status).toBe(503);
    }
  });
  it("refuses /llm and /inbound with no header", async () => {
    expect((await hit("/llm/chat/completions", { method: "POST", body: llmBody })).status).toBe(503);
    expect((await hit("/inbound", { method: "POST", body: "{}" })).status).toBe(503);
    expect((await hit("/inbound", { method: "POST", headers: { "x-orole-relay": "" }, body: "{}" })).status).toBe(503);
  });
  it("refuses /status even with a well-formed signature", async () => {
    const body = "{}";
    const res = await hit("/status", { method: "POST", headers: { "ElevenLabs-Signature": await sign(body) }, body });
    expect(res.status).toBe(503);
  });
});

describe("bounded pending turns", () => {
  it("speaks LOST when the daemon stays connected but silent", async () => {
    const ws = await daemon();
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    const text = await res.text();
    expect(text).toContain("Lost connection");
    expect(text).toContain("data: [DONE]");
    ws.close();
  });
  it("speaks LOST when the socket closes mid-turn", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      if (JSON.parse(e.data as string).type === "llm.request") ws.close();
    });
    const res = await SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    const text = await res.text();
    expect(text).toContain("Lost connection");
    expect(text).toContain("data: [DONE]");
  });
});
