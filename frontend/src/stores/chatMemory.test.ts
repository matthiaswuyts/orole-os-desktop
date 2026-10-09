import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MemoryChange, MemoryEntry, MemoryScope } from "@/mygo";

const bridge = vi.hoisted(() => ({
  memoryScopes: vi.fn(),
  memoryEntries: vi.fn(),
  saveMemoryEntry: vi.fn(),
  deleteMemoryEntry: vi.fn(),
  resolveMemory: vi.fn(),
  createMemoryScope: vi.fn(),
  deleteMemoryScope: vi.fn(),
  memory: null as ((c: MemoryChange) => void) | null,
}));

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  Chat: bridge,
  Daemon: { health: vi.fn() },
  events: { chatMemory: { on: (f: (c: MemoryChange) => void) => { bridge.memory = f; return () => { bridge.memory = null; }; } } },
}));

import { pendingOf, useChatMemory } from "@/stores/chatMemory";

const entry = (id: string, over: Partial<MemoryEntry> = {}): MemoryEntry => ({ id, scope: "user", kind: "fact", text: id, trust: "user", author: "user", pending: false, createdAt: 1, updatedAt: 1, ...over });
const scope = (id: string): MemoryScope => ({ id, name: id, kind: "shared", count: 0, used: 0, limit: 100 });

beforeEach(() => {
  vi.clearAllMocks();
  bridge.memory = null;
  useChatMemory.setState({ scopes: {}, entries: {} });
  bridge.memoryScopes.mockResolvedValue([]);
  bridge.memoryEntries.mockResolvedValue([]);
});

describe("chat memory store", () => {
  it("drops a response that a newer request has overtaken", async () => {
    let finish!: (v: MemoryEntry[]) => void;
    bridge.memoryEntries.mockImplementationOnce(() => new Promise<MemoryEntry[]>((resolve) => { finish = resolve; }));
    const slow = useChatMemory.getState().loadEntries("user", { query: "old", history: false });
    bridge.memoryEntries.mockResolvedValueOnce([entry("fresh")]);
    await useChatMemory.getState().loadEntries("user", { query: "new", history: false });
    finish([entry("stale")]);
    await slow;
    expect(useChatMemory.getState().entries.user).toMatchObject({ items: [{ id: "fresh" }], loaded: true, filter: { query: "new" } });
  });

  it("asks for pending entries, optional history and a trimmed query", async () => {
    await useChatMemory.getState().loadEntries("shared:home", { query: "  tea ", history: true });
    expect(bridge.memoryEntries).toHaveBeenCalledWith({ scope: "shared:home", kind: "", includePending: true, includeInvalidated: true, query: "tea", limit: 500 });
  });

  it("keeps the previous entries and reports the error when a reload fails", async () => {
    bridge.memoryEntries.mockResolvedValueOnce([entry("a")]).mockRejectedValueOnce(new Error("internal: database is locked"));
    await useChatMemory.getState().loadEntries("user", { query: "", history: false });
    await useChatMemory.getState().loadEntries("user", { query: "", history: false });
    expect(useChatMemory.getState().entries.user).toMatchObject({ items: [{ id: "a" }], error: "database is locked", loading: false });
  });

  it("reloads the changed scope with its own filter and every loaded bot's scopes on a memory event", async () => {
    const stop = useChatMemory.getState().startSync();
    await useChatMemory.getState().loadScopes("b1");
    await useChatMemory.getState().loadEntries("user", { query: "tea", history: true });
    await useChatMemory.getState().loadEntries("shared:home", { query: "", history: false });
    vi.clearAllMocks();
    bridge.memory?.({ scope: "user" });
    await vi.waitFor(() => expect(bridge.memoryScopes).toHaveBeenCalledWith("b1"));
    expect(bridge.memoryEntries).toHaveBeenCalledTimes(1);
    expect(bridge.memoryEntries).toHaveBeenCalledWith(expect.objectContaining({ scope: "user", query: "tea", includeInvalidated: true }));
    stop();
    expect(bridge.memory).toBeNull();
  });

  it("subscribes once for several listeners and unsubscribes with the last", () => {
    const a = useChatMemory.getState().startSync();
    const b = useChatMemory.getState().startSync();
    expect(bridge.memory).not.toBeNull();
    a();
    a();
    expect(bridge.memory).not.toBeNull();
    b();
    expect(bridge.memory).toBeNull();
  });

  it("reports failed actions as outcomes with the daemon's sentence", async () => {
    bridge.saveMemoryEntry.mockRejectedValue(new Error("bad_request: Memory text is empty."));
    expect(await useChatMemory.getState().saveEntry({ id: "", scope: "user", kind: "fact", text: "" })).toEqual({ ok: false, error: "Memory text is empty." });
    bridge.resolveMemory.mockResolvedValue(undefined);
    expect(await useChatMemory.getState().resolve("m1", true, "user")).toEqual({ ok: true, value: undefined });
    expect(bridge.resolveMemory).toHaveBeenCalledWith({ id: "m1", approve: true });
  });

  it("forgets a deleted scope's entries and reloads the bot's scopes", async () => {
    bridge.deleteMemoryScope.mockResolvedValue(undefined);
    bridge.memoryEntries.mockResolvedValue([entry("x", { scope: "shared:home" })]);
    await useChatMemory.getState().loadEntries("shared:home", { query: "", history: false });
    bridge.memoryScopes.mockResolvedValue([scope("user")]);
    const outcome = await useChatMemory.getState().deleteScope("shared:home", "b1");
    expect(outcome.ok).toBe(true);
    expect(bridge.deleteMemoryScope).toHaveBeenCalledWith("shared:home");
    expect(useChatMemory.getState().entries["shared:home"]).toBeUndefined();
    await vi.waitFor(() => expect(useChatMemory.getState().scopes.b1?.items).toEqual([scope("user")]));
  });

  it("separates pending writes from the rest", () => {
    const list = [entry("a"), entry("b", { pending: true }), entry("c", { pending: true, invalidatedAt: 5 })];
    expect(pendingOf(list).map((e) => e.id)).toEqual(["b"]);
  });
});
