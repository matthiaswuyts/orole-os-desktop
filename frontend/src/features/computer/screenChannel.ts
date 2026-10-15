import { Sandbox, events, type ScreenChunk } from "@/mygo";

type CloseInfo = { code: number; reason: string; wasClean: boolean };
type Receiver = ((event: { data: ArrayBuffer }) => void) | null;

/** Chunks kept while the screen id is unknown (the guest's VNC server speaks first). */
const EARLY_MAX = 256;

/** Bytes to base64 in slices, so a large chunk never overflows the argument list. */
export function toBase64(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(bin);
}

/** Base64 to bytes with a plain loop. */
export function fromBase64(s: string): Uint8Array {
  const bin = atob(s);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/**
 * The WebSocket-shaped object noVNC's RFB accepts instead of a URL (noVNC 1.7.0 `Websock.attach`), carried by the
 * MyGo `Sandbox` calls and the `sandbox:screen` event: no socket exists anywhere. The daemon decides what input
 * reaches the computer; this only moves bytes. Outgoing bytes are copied at once (noVNC reuses its send buffer) and
 * sent in order, one call at a time, joining what queued meanwhile.
 */
export class ScreenChannel {
  binaryType = "arraybuffer";
  protocol = "";
  readyState: "connecting" | "open" | "closing" | "closed" = "connecting";
  onopen: ((event: Event) => void) | null = null;
  onclose: ((event: CloseInfo) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  readonly botId: string;
  private id: string | null = null;
  private receiver: Receiver = null;
  private inbound: ArrayBuffer[] = [];
  private early: ScreenChunk[] = [];
  private outbound: Uint8Array[] = [];
  private sending = false;
  private readonly off: () => void;

  constructor(botId: string) {
    this.botId = botId;
    this.off = events.sandboxScreen.on((chunk) => this.onChunk(chunk));
  }

  get onmessage(): Receiver { return this.receiver; }
  /** Bytes that came before noVNC assigned its handler are handed over now. */
  set onmessage(fn: Receiver) {
    this.receiver = fn;
    if (fn) for (const data of this.inbound.splice(0)) fn({ data });
  }

  /** Opens the daemon's screen stream for the bot. */
  async open(): Promise<void> {
    let id: string;
    try {
      id = await Sandbox.openScreen(this.botId);
    } catch (e) {
      this.fail(e instanceof Error ? e.message : String(e));
      return;
    }
    if (this.readyState !== "connecting") { void Sandbox.closeScreen(id).catch(() => {}); return; }
    this.id = id;
    this.readyState = "open";
    this.onopen?.(new Event("open"));
    for (const chunk of this.early.splice(0)) this.onChunk(chunk);
    this.pump();
  }

  send(data: ArrayBuffer | Uint8Array): void {
    if (this.readyState !== "open" && this.readyState !== "connecting") return;
    this.outbound.push(data instanceof Uint8Array ? data.slice() : new Uint8Array(data.slice(0)));
    this.pump();
  }

  close(): void {
    if (this.readyState === "closed") return;
    const id = this.id;
    this.finish({ code: 1000, reason: "", wasClean: true });
    if (id) void Sandbox.closeScreen(id).catch(() => {});
  }

  private pump(): void {
    if (this.sending || !this.id || this.readyState !== "open" || this.outbound.length === 0) return;
    const parts = this.outbound.splice(0);
    const joined = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
    let at = 0;
    for (const p of parts) { joined.set(p, at); at += p.length; }
    this.sending = true;
    Sandbox.screenInput(this.id, toBase64(joined)).then(
      () => { this.sending = false; this.pump(); },
      (e: unknown) => { this.sending = false; this.fail(e instanceof Error ? e.message : String(e)); },
    );
  }

  private onChunk(chunk: ScreenChunk): void {
    if (this.readyState === "closed") return;
    if (this.id === null) {
      if (this.early.length >= EARLY_MAX) { this.fail("the screen stream outran the connection setup"); return; }
      this.early.push(chunk);
      return;
    }
    if (chunk.id !== this.id) return;
    if (chunk.data) {
      const data = fromBase64(chunk.data).buffer as ArrayBuffer;
      if (this.receiver) this.receiver({ data }); else this.inbound.push(data);
    }
    if (chunk.closed) this.finish(chunk.error ? { code: 1011, reason: chunk.error, wasClean: false } : { code: 1000, reason: "", wasClean: true });
  }

  private fail(reason: string): void {
    if (this.readyState === "closed") return;
    const id = this.id;
    this.onerror?.(new Event("error"));
    this.finish({ code: 1006, reason, wasClean: false });
    if (id) void Sandbox.closeScreen(id).catch(() => {});
  }

  private finish(info: CloseInfo): void {
    this.readyState = "closed";
    this.off();
    this.outbound = [];
    this.early = [];
    this.onclose?.(info);
  }
}
