import { DurableObject } from "cloudflare:workers";

type Frame = { type: string; id?: string; callId?: string; text?: string; tool?: string; args?: string; accept?: boolean; vars?: Record<string, string>; overrides?: unknown; [k: string]: unknown };
type Pending = { onFrame: (f: Frame) => void; socket: WebSocket; socketGone?: () => void };

export interface HubEnv {
  // Optional timer overrides in milliseconds (tests use short values).
  IDLE_MS?: string;
  GRACE_MS?: string;
  MAX_MS?: string;
}

// The relay's own spoken lines. The daemon tells it the call language as `lang` in custom_llm_extra_body ("nl" or
// "en"); anything else, including an old daemon that sends none, gets English.
const LINES = {
  en: {
    goodbye: "Sorry, I can't reach your bots right now. I'll message you instead.",
    lost: "Lost connection, I'll message you.",
    failed: "That failed.",
  },
  nl: {
    goodbye: "Sorry, ik kan je bots nu niet bereiken. Ik stuur je een bericht.",
    lost: "De verbinding is weggevallen, ik stuur je een bericht.",
    failed: "Dat is mislukt.",
  },
};
const DEFAULT_IDLE_MS = 20_000; // no frame for this long while the daemon is connected -> give up
const DEFAULT_GRACE_MS = 5_000; // daemon socket gone: wait this long, then give up
const DEFAULT_MAX_MS = 120_000; // absolute cap per request
const INBOUND_TIMEOUT_MS = 4_000;
// ElevenLabs is configured with <relay>/llm; depending on the client it appends /chat/completions or
// /v1/chat/completions, or posts to the bare path. All of them are the same custom-LLM endpoint.
export const LLM_PATHS = ["/llm/chat/completions", "/llm", "/llm/v1/chat/completions"];
const enc = new TextEncoder();

function sseEvent(delta: object, finishReason: string | null): string {
  return `data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta, finish_reason: finishReason }] })}\n\n`;
}
function sseChunk(content: string): Uint8Array {
  return enc.encode(sseEvent({ content }, null));
}
const sseDone = enc.encode(`${sseEvent({}, "stop")}data: [DONE]\n\n`);

// A function call in the OpenAI streaming format, which ElevenLabs executes (for example its skip_turn system tool):
// one chunk whose delta carries role "assistant" (OpenAI streams the role in the first assistant delta) and
// tool_calls holding the complete call (index, id, type, function.name, function.arguments as a JSON string), then a
// chunk with finish_reason "tool_calls" and an empty delta, then [DONE].
function sseTool(name: string, args: string): Uint8Array {
  const call = { index: 0, id: `call_${crypto.randomUUID().replaceAll("-", "").slice(0, 24)}`, type: "function", function: { name, arguments: args } };
  return enc.encode(`${sseEvent({ role: "assistant", tool_calls: [call] }, null)}${sseEvent({}, "tool_calls")}data: [DONE]\n\n`);
}

// The arguments of a tool call must be a JSON object string; anything else becomes "{}".
function toolArgs(raw: unknown): string {
  if (typeof raw !== "string" || raw === "") return "{}";
  try {
    const v = JSON.parse(raw);
    return v !== null && typeof v === "object" && !Array.isArray(v) ? raw : "{}";
  } catch {
    return "{}";
  }
}

function ms(v: string | undefined, d: number): number {
  const n = Number(v);
  return v && Number.isFinite(n) && n > 0 ? n : d;
}

// Hub holds the one daemon socket and the in-flight request table. It persists nothing.
export class Hub extends DurableObject<HubEnv> {
  private daemon: WebSocket | null = null;
  private pending = new Map<string, Pending>();

  async fetch(req: Request): Promise<Response> {
    const path = new URL(req.url).pathname;
    if (path === "/health") return Response.json({ daemon: this.daemon !== null });
    if (path === "/daemon") return req.headers.get("Upgrade") === "websocket" ? this.attach() : new Response("expected websocket", { status: 426 });
    let body: any;
    try { body = await req.json(); } catch { return new Response("bad json", { status: 400 }); }
    if (LLM_PATHS.includes(path)) return this.llm(body ?? {});
    if (path === "/inbound") return this.inbound(body ?? {});
    if (path === "/status") {
      this.send(this.statusFrame(body ?? {}));
      return new Response("ok");
    }
    return new Response("not found", { status: 404 });
  }

  private attach(): Response {
    const pair = new WebSocketPair();
    const [client, server] = [pair[0], pair[1]];
    server.accept();
    const old = this.daemon;
    this.daemon = server;
    if (old) {
      this.dropSocket(old);
      try { old.close(4000, "replaced"); } catch { /* already closed */ }
    }
    server.addEventListener("message", (e) => {
      let f: Frame;
      try { f = JSON.parse(e.data as string); } catch { return; }
      if (f.id) this.pending.get(f.id)?.onFrame(f);
    });
    const gone = () => this.dropSocket(server);
    server.addEventListener("close", gone);
    server.addEventListener("error", gone);
    return new Response(null, { status: 101, webSocket: client });
  }

  // The socket is gone: clear it if current, and start the grace period for every request it carried.
  private dropSocket(sock: WebSocket) {
    if (this.daemon === sock) this.daemon = null;
    for (const p of [...this.pending.values()]) if (p.socket === sock) p.socketGone?.();
  }

  private send(f: Frame): boolean {
    if (!this.daemon) return false;
    try { this.daemon.send(JSON.stringify(f)); return true; } catch { return false; }
  }

  private llm(body: { messages?: { role: string; content: string }[]; elevenlabs_extra_body?: Record<string, string> }): Response {
    const { readable, writable } = new TransformStream<Uint8Array>();
    const w = writable.getWriter();
    const headers = { "content-type": "text/event-stream", "cache-control": "no-cache" };
    const id = crypto.randomUUID();
    const callId = body.elevenlabs_extra_body?.call_id ?? "";
    const lines = body.elevenlabs_extra_body?.lang === "nl" ? LINES.nl : LINES.en;
    const idleMs = ms(this.env.IDLE_MS, DEFAULT_IDLE_MS);
    const graceMs = ms(this.env.GRACE_MS, DEFAULT_GRACE_MS);
    const maxMs = ms(this.env.MAX_MS, DEFAULT_MAX_MS);

    let idle: number | null = null;
    let grace: number | null = null;
    let cap: number | null = null;
    let done = false;
    // Ends the stream: an optional last spoken line, or a function call instead of the normal stop. Text deltas
    // written earlier are queued ahead of either, so they stay before the tool call.
    const finish = async (spoken?: string, tool?: { name: string; args: string }) => {
      if (done) return;
      done = true;
      clearTimeout(idle); clearTimeout(grace); clearTimeout(cap);
      this.pending.delete(id);
      try {
        if (spoken) await w.write(sseChunk(spoken));
        await w.write(tool ? sseTool(tool.name, tool.args) : sseDone);
        await w.close();
      } catch { /* client went away */ }
    };

    const sock = this.daemon;
    const sent = sock !== null && this.send({ type: "llm.request", id, callId, messages: (body.messages ?? []).map(({ role, content }) => ({ role, content })) });
    if (!sent || !sock) {
      void finish(lines.goodbye);
      return new Response(readable, { headers });
    }
    const armIdle = () => { clearTimeout(idle); idle = setTimeout(() => void finish(lines.lost), idleMs); };
    armIdle();
    cap = setTimeout(() => void finish(lines.lost), maxMs);
    this.pending.set(id, {
      socket: sock,
      socketGone: () => {
        clearTimeout(idle);
        if (grace === null) grace = setTimeout(() => void finish(lines.lost), graceMs);
      },
      onFrame: (f) => {
        if (done) return;
        armIdle();
        if (f.type === "llm.delta" && f.text) void w.write(sseChunk(f.text)).catch(() => {});
        else if (f.type === "llm.done") void finish();
        // A tool frame without a function name is ignored: the idle timer ends the request with the lost line.
        else if (f.type === "llm.tool" && f.tool) void finish(undefined, { name: f.tool, args: toolArgs(f.args) });
        else if (f.type === "llm.error") void finish(f.text || lines.failed);
      },
    });
    return new Response(readable, { headers });
  }

  private async inbound(body: { caller_id?: string; called_number?: string; call_sid?: string }): Promise<Response> {
    const sock = this.daemon;
    if (!sock) return new Response("daemon offline", { status: 503 });
    const id = crypto.randomUUID();
    let t: number | null = null;
    // null: the daemon went away or never answered (503, the daemon is unavailable); a frame: its decision.
    const answer = new Promise<Frame | null>((resolve) => {
      t = setTimeout(() => { this.pending.delete(id); resolve(null); }, INBOUND_TIMEOUT_MS);
      this.pending.set(id, {
        socket: sock,
        socketGone: () => { clearTimeout(t); this.pending.delete(id); resolve(null); },
        onFrame: (f) => { if (f.type === "inbound.answer") { clearTimeout(t); this.pending.delete(id); resolve(f); } },
      });
    });
    if (!this.send({ type: "inbound.lookup", id, caller: body.caller_id ?? "", called: body.called_number ?? "", callSid: body.call_sid ?? "" })) {
      clearTimeout(t);
      this.pending.delete(id);
      return new Response("daemon offline", { status: 503 });
    }
    const f = await answer;
    if (!f) return new Response("daemon offline", { status: 503 });
    // Go omits `accept` when false, so a missing field is a rejection.
    if (f.accept !== true) return new Response("rejected", { status: 403 });
    const vars = f.vars ?? {};
    // custom_llm_extra_body comes back on every /llm request as elevenlabs_extra_body, which is how a turn
    // finds its call (call_id) and learns the call language for the relay's own spoken lines (lang, when the daemon
    // sent one).
    const extra: Record<string, string> = { call_id: vars.call_id ?? "" };
    if (vars.lang) extra.lang = vars.lang;
    return Response.json({
      type: "conversation_initiation_client_data",
      dynamic_variables: vars,
      conversation_config_override: f.overrides ?? {},
      custom_llm_extra_body: extra,
    });
  }

  private statusFrame(body: { data?: { conversation_id?: string; status?: string; metadata?: { call_duration_secs?: number }; conversation_initiation_client_data?: { dynamic_variables?: Record<string, string> } } }): Frame {
    const d = body.data ?? {};
    return {
      type: "call.status",
      callId: d.conversation_initiation_client_data?.dynamic_variables?.call_id ?? "",
      conversationId: d.conversation_id ?? "",
      status: d.status ?? "",
      durationSecs: d.metadata?.call_duration_secs ?? 0,
    };
  }
}
