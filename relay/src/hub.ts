import { DurableObject } from "cloudflare:workers";

type Frame = { type: string; id?: string; callId?: string; text?: string; accept?: boolean; vars?: Record<string, string>; overrides?: unknown; [k: string]: unknown };
type Pending = { onFrame: (f: Frame) => void };

const GOODBYE = "Sorry, I can't reach your bots right now. I'll message you instead.";
const LOST = "Lost connection, I'll message you.";
const enc = new TextEncoder();

function sseChunk(content: string): Uint8Array {
  return enc.encode(`data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta: { content }, finish_reason: null }] })}\n\n`);
}
const sseDone = enc.encode(`data: ${JSON.stringify({ id: "c", object: "chat.completion.chunk", choices: [{ index: 0, delta: {}, finish_reason: "stop" }] })}\n\ndata: [DONE]\n\n`);

// Hub holds the one daemon socket and the in-flight request table. It persists nothing.
export class Hub extends DurableObject {
  private daemon: WebSocket | null = null;
  private pending = new Map<string, Pending>();

  async fetch(req: Request): Promise<Response> {
    const path = new URL(req.url).pathname;
    if (path === "/health") return Response.json({ daemon: this.daemon !== null });
    if (path === "/daemon") return req.headers.get("Upgrade") === "websocket" ? this.attach() : new Response("expected websocket", { status: 426 });
    let body: any;
    try { body = await req.json(); } catch { return new Response("bad json", { status: 400 }); }
    if (path === "/llm/chat/completions") return this.llm(body ?? {});
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
    this.daemon?.close(4000, "replaced");
    this.daemon = server;
    server.addEventListener("message", (e) => {
      let f: Frame;
      try { f = JSON.parse(e.data as string); } catch { return; }
      if (f.id) this.pending.get(f.id)?.onFrame(f);
    });
    const gone = () => { if (this.daemon === server) this.daemon = null; };
    server.addEventListener("close", gone);
    server.addEventListener("error", gone);
    return new Response(null, { status: 101, webSocket: client });
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
    const finish = async (spoken?: string) => {
      this.pending.delete(id);
      try {
        if (spoken) await w.write(sseChunk(spoken));
        await w.write(sseDone);
        await w.close();
      } catch { /* client went away */ }
    };
    const sent = this.send({ type: "llm.request", id, callId, messages: (body.messages ?? []).map(({ role, content }) => ({ role, content })) });
    if (!sent) {
      void finish(GOODBYE);
      return new Response(readable, { headers });
    }
    // If the daemon socket drops mid-turn, wait 5 s for it to come back, then give up.
    let timer: number | null = null;
    const arm = () => { clearTimeout(timer); timer = setTimeout(() => { if (!this.daemon) void finish(LOST); else arm(); }, 5000); };
    arm();
    this.pending.set(id, {
      onFrame: (f) => {
        arm();
        if (f.type === "llm.delta" && f.text) void w.write(sseChunk(f.text)).catch(() => {});
        else if (f.type === "llm.done") { clearTimeout(timer); void finish(); }
        else if (f.type === "llm.error") { clearTimeout(timer); void finish(f.text || "That failed."); }
      },
    });
    return new Response(readable, { headers });
  }

  private async inbound(body: { caller_id?: string; called_number?: string; call_sid?: string }): Promise<Response> {
    const id = crypto.randomUUID();
    const answer = new Promise<Frame | null>((resolve) => {
      const t = setTimeout(() => { this.pending.delete(id); resolve(null); }, 4000);
      this.pending.set(id, { onFrame: (f) => { if (f.type === "inbound.answer") { clearTimeout(t); this.pending.delete(id); resolve(f); } } });
    });
    if (!this.send({ type: "inbound.lookup", id, caller: body.caller_id ?? "", called: body.called_number ?? "", callSid: body.call_sid ?? "" })) {
      this.pending.delete(id);
      return new Response("daemon offline", { status: 503 });
    }
    const f = await answer;
    // Go omits `accept` when false, so a missing field is a rejection.
    if (!f || f.accept !== true) return new Response("rejected", { status: 403 });
    return Response.json({ type: "conversation_initiation_client_data", dynamic_variables: f.vars ?? {}, conversation_config_override: f.overrides ?? {} });
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
