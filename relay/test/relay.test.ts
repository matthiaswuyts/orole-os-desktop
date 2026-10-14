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

const dutchBody = JSON.stringify({
  model: "bot",
  stream: true,
  messages: [{ role: "user", content: "wat draait er?" }],
  elevenlabs_extra_body: { call_id: "c1", lang: "nl" },
});

type Chunk = { object: string; choices: { index: number; delta: Record<string, any>; finish_reason: string | null }[] };

// The SSE body as its events: parsed chunks, and the literal "[DONE]".
function sseEvents(text: string): (Chunk | "[DONE]")[] {
  return text.split("\n\n").filter((e) => e.trim() !== "").map((e) => {
    expect(e.startsWith("data: ")).toBe(true);
    const data = e.slice("data: ".length);
    return data === "[DONE]" ? "[DONE]" : (JSON.parse(data) as Chunk);
  });
}

// A daemon that answers every llm.request with the given frames (their id is filled in).
async function answeringDaemon(frames: Record<string, unknown>[]) {
  const ws = await daemon();
  ws.addEventListener("message", (e) => {
    const f = JSON.parse(e.data as string);
    if (f.type !== "llm.request") return;
    for (const r of frames) ws.send(JSON.stringify({ ...r, id: f.id }));
  });
  return ws;
}

async function llm(body = llmBody) {
  return SELF.fetch("https://relay/llm/chat/completions", { method: "POST", headers: { "x-orole-relay": "ls" }, body });
}

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

  it("speaks the goodbye in Dutch when the call language is nl", async () => {
    const text = await (await llm(dutchBody)).text();
    expect(text).toContain("Sorry, ik kan je bots nu niet bereiken. Ik stuur je een bericht.");
    expect(text).not.toContain("can't reach");
    expect(text.trim().endsWith("data: [DONE]")).toBe(true);
  });

  it("speaks English for an unknown or missing language", async () => {
    for (const lang of ["en", "xx", undefined]) {
      const body = JSON.stringify({ messages: [], elevenlabs_extra_body: { call_id: "c1", lang } });
      expect(await (await llm(body)).text()).toContain("can't reach your bots right now");
    }
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

describe("llm tool calls", () => {
  it("streams an llm.tool frame as an OpenAI tool call after the text sent before it", async () => {
    const ws = await answeringDaemon([
      { type: "llm.delta", text: "One moment. " },
      { type: "llm.tool", callId: "c1", tool: "skip_turn", args: "{}" },
    ]);
    const res = await llm();
    expect(res.headers.get("content-type")).toContain("text/event-stream");
    const text = await res.text();
    const events = sseEvents(text);
    expect(events).toHaveLength(4);
    const [say, call, finish, done] = events as [Chunk, Chunk, Chunk, "[DONE]"];
    expect(say.choices[0].delta).toEqual({ content: "One moment. " });
    expect(say.choices[0].finish_reason).toBeNull();
    // One complete call in a single delta: index 0, a call_ id, type function, the arguments as a JSON string.
    expect(call.object).toBe("chat.completion.chunk");
    expect(call.choices[0].finish_reason).toBeNull();
    expect(call.choices[0].delta.content).toBeUndefined();
    expect(call.choices[0].delta.tool_calls).toEqual([
      { index: 0, id: expect.stringMatching(/^call_[0-9a-f]{24}$/), type: "function", function: { name: "skip_turn", arguments: "{}" } },
    ]);
    // Then the finish chunk: empty delta and finish_reason tool_calls, never "stop"; then [DONE], and nothing after.
    expect(finish.choices[0]).toEqual({ index: 0, delta: {}, finish_reason: "tool_calls" });
    expect(done).toBe("[DONE]");
    expect(text).not.toContain('"finish_reason":"stop"');
    expect(text.endsWith("data: [DONE]\n\n")).toBe(true);
    ws.close();
  });

  it("answers with just the tool call when no text came first", async () => {
    const ws = await answeringDaemon([{ type: "llm.tool", tool: "skip_turn" }]);
    const events = sseEvents(await (await llm()).text());
    expect(events).toHaveLength(3);
    expect((events[0] as Chunk).choices[0].delta.tool_calls).toMatchObject([{ index: 0, type: "function", function: { name: "skip_turn", arguments: "{}" } }]);
    expect((events[1] as Chunk).choices[0].finish_reason).toBe("tool_calls");
    expect(events[2]).toBe("[DONE]");
    ws.close();
  });

  it("passes JSON object arguments through and replaces anything else with {}", async () => {
    const run = async (args: unknown) => {
      const ws = await answeringDaemon([{ type: "llm.tool", tool: "end_call", args }]);
      const events = sseEvents(await (await llm()).text());
      ws.close();
      return (events[0] as Chunk).choices[0].delta.tool_calls![0].function.arguments;
    };
    expect(await run('{"reason":"done"}')).toBe('{"reason":"done"}');
    for (const bad of ["", "not json", "[1]", "null", "7", 5, null]) expect(await run(bad)).toBe("{}");
  });

  it("gives every call its own id", async () => {
    const ws = await answeringDaemon([{ type: "llm.tool", tool: "skip_turn" }]);
    const ids = new Set<string>();
    for (let i = 0; i < 3; i++) ids.add(((sseEvents(await (await llm()).text())[0] as Chunk).choices[0].delta.tool_calls![0] as { id: string }).id);
    expect(ids.size).toBe(3);
    ws.close();
  });

  it("ignores frames after the tool call and a tool frame without a name", async () => {
    const ws = await answeringDaemon([
      { type: "llm.tool", tool: "skip_turn" },
      { type: "llm.delta", text: "late" },
      { type: "llm.done" },
    ]);
    const text = await (await llm()).text();
    expect(text).not.toContain("late");
    expect(sseEvents(text)).toHaveLength(3);
    ws.close();

    // A nameless tool frame does not end the request: the idle timer does, with the lost line.
    const ws2 = await answeringDaemon([{ type: "llm.tool", tool: "" }]);
    const lost = await (await llm()).text();
    expect(lost).toContain("Lost connection");
    expect(lost).not.toContain("tool_calls");
    ws2.close();
  });
});

describe("llm path aliases", () => {
  for (const path of ["/llm", "/llm/v1/chat/completions"]) {
    it(`serves ${path} like /llm/chat/completions`, async () => {
      expect((await SELF.fetch(`https://relay${path}`, { method: "POST", body: llmBody })).status).toBe(401);
      const ws = await daemon();
      ws.addEventListener("message", (e) => {
        const f = JSON.parse(e.data as string);
        if (f.type !== "llm.request") return;
        expect(f.callId).toBe("c1");
        ws.send(JSON.stringify({ type: "llm.delta", id: f.id, text: "Hi." }));
        ws.send(JSON.stringify({ type: "llm.done", id: f.id }));
      });
      const res = await SELF.fetch(`https://relay${path}`, { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
      expect(res.headers.get("content-type")).toContain("text/event-stream");
      const text = await res.text();
      expect(text).toContain('"content":"Hi."');
      expect(text.trim().endsWith("data: [DONE]")).toBe(true);
      ws.close();
    });
  }
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
    const body = (await res.json()) as { type: string; dynamic_variables: Record<string, string>; custom_llm_extra_body: Record<string, string> };
    expect(body.type).toBe("conversation_initiation_client_data");
    expect(body.dynamic_variables.call_id).toBe("c9");
    // ElevenLabs echoes this on every /llm request, which is how an inbound turn finds its call.
    expect(body.custom_llm_extra_body).toEqual({ call_id: "c9" });
    ws.close();
  });
  it("puts the call language in custom_llm_extra_body when the daemon sends one", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      const f = JSON.parse(e.data as string);
      if (f.type === "inbound.lookup") ws.send(JSON.stringify({ type: "inbound.answer", id: f.id, accept: true, vars: { call_id: "c9", bot_id: "b1", lang: "nl" } }));
    });
    const res = await SELF.fetch("https://relay/inbound", { method: "POST", headers: { "x-orole-relay": "ls" }, body: JSON.stringify({ caller_id: "+32470000000" }) });
    expect(res.status).toBe(200);
    const body = (await res.json()) as { dynamic_variables: Record<string, string>; custom_llm_extra_body: Record<string, string> };
    expect(body.custom_llm_extra_body).toEqual({ call_id: "c9", lang: "nl" });
    expect(body.dynamic_variables).toMatchObject({ call_id: "c9", bot_id: "b1" });
    ws.close();
  });
  it("returns 503 when the daemon socket drops before answering", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      if (JSON.parse(e.data as string).type === "inbound.lookup") ws.close();
    });
    const res = await SELF.fetch("https://relay/inbound", { method: "POST", headers: { "x-orole-relay": "ls" }, body: JSON.stringify({ caller_id: "+1" }) });
    expect(res.status).toBe(503);
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
  it("refuses the llm aliases with no secret", async () => {
    for (const p of ["/llm", "/llm/v1/chat/completions"]) {
      expect((await hit(p, { method: "POST", headers: { "x-orole-relay": "" }, body: llmBody })).status).toBe(503);
    }
  });
  it("fails closed per path: a missing webhook secret leaves /llm working", async () => {
    const noWebhook = { ...env, WEBHOOK_SECRET: "" } as unknown as Env;
    const call = (path: string, init: RequestInit) => worker.fetch(new Request(`https://relay${path}`, init), noWebhook);
    expect((await call("/status", { method: "POST", headers: { "ElevenLabs-Signature": await sign("{}") }, body: "{}" })).status).toBe(503);
    const res = await call("/llm", { method: "POST", headers: { "x-orole-relay": "ls" }, body: llmBody });
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("data: [DONE]");
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
  it("speaks LOST in Dutch when the call language is nl", async () => {
    const ws = await daemon();
    const text = await (await llm(dutchBody)).text();
    expect(text).toContain("De verbinding is weggevallen, ik stuur je een bericht.");
    expect(text).not.toContain("Lost connection");
    expect(text).toContain("data: [DONE]");
    ws.close();
  });
  it("speaks LOST in Dutch when the socket closes mid-turn", async () => {
    const ws = await daemon();
    ws.addEventListener("message", (e) => {
      if (JSON.parse(e.data as string).type === "llm.request") ws.close();
    });
    const text = await (await llm(dutchBody)).text();
    expect(text).toContain("De verbinding is weggevallen");
    expect(text).toContain("data: [DONE]");
  });
  it("speaks the failure line in the call language when llm.error has no text", async () => {
    const ws = await answeringDaemon([{ type: "llm.error" }]);
    expect(await (await llm(dutchBody)).text()).toContain("Dat is mislukt.");
    expect(await (await llm()).text()).toContain("That failed.");
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
