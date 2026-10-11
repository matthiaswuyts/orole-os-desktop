import { act, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Defaults, Model, ProviderStatus } from "@/mygo";

const bridge = vi.hoisted(() => ({
  providers: vi.fn(), defaults: vi.fn(), models: vi.fn(), connect: vi.fn(), disconnect: vi.fn(), test: vi.fn(),
  setDefaults: vi.fn(), authStart: vi.fn(), authFinish: vi.fn(),
  changed: null as ((event: { id?: string }) => void) | null,
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  LLM: {
    providers: bridge.providers, defaults: bridge.defaults, models: bridge.models, connect: bridge.connect, disconnect: bridge.disconnect,
    test: bridge.test, setDefaults: bridge.setDefaults, openRouterAuthStart: bridge.authStart, openRouterAuthFinish: bridge.authFinish,
  },
  Daemon: { health: vi.fn() },
  events: {
    llmChanged: { on: (listener: (event: { id?: string }) => void) => { bridge.changed = listener; return () => { bridge.changed = null; }; } },
    daemonState: { on: () => () => {} },
  },
}));
import { useLLM } from "@/stores/llm";
import { useDaemon } from "@/stores/daemon";

const none: ProviderStatus = { id: "openrouter", name: "OpenRouter", kind: "builtin", connected: false };
const connected: ProviderStatus = { ...none, connected: true, keyHint: "abcd", valid: true };
const defaults: Defaults = { chatModel: "", memoryModel: "" };
const caps = { tools: true, vision: false, pdf: false, reasoning: false, hostedTools: [], deepResearch: false, contextWindow: 0 };
const model = (id: string): Model => ({ id, name: id, caps, reasoning: [] });
let stopSync: (() => void) | undefined;

beforeEach(() => {
  for (const fn of Object.values(bridge)) if (vi.isMockFunction(fn)) fn.mockReset();
  bridge.providers.mockResolvedValue([none]);
  bridge.defaults.mockResolvedValue(defaults);
  localStorage.clear(); // the stores keep a last-known snapshot there
  useDaemon.setState({ link: "connected" });
  useLLM.setState({ providers: [], defaults: null, models: {}, busy: {}, ready: false, loading: false, error: null });
});
afterEach(() => { stopSync?.(); stopSync = undefined; });

describe("llm snapshot", () => {
  it("ignores a list response that a newer request has superseded", async () => {
    let finishFirst!: (value: ProviderStatus[]) => void;
    bridge.providers.mockImplementationOnce(() => new Promise<ProviderStatus[]>((resolve) => { finishFirst = resolve; }));
    bridge.providers.mockResolvedValueOnce([connected]);
    const first = useLLM.getState().load();
    await useLLM.getState().load();
    expect(useLLM.getState().providers).toEqual([connected]);
    finishFirst([none]);
    await first;
    expect(useLLM.getState().providers).toEqual([connected]);
  });

  it("reloads when the daemon announces a change", async () => {
    stopSync = useLLM.getState().startSync();
    await waitFor(() => expect(useLLM.getState().ready).toBe(true));
    bridge.providers.mockResolvedValue([connected]);
    act(() => bridge.changed?.({ id: "openrouter" }));
    await waitFor(() => expect(useLLM.getState().providers[0]?.connected).toBe(true));
  });

  it("shows the last known providers at once while the daemon is still answering", async () => {
    localStorage.setItem("orole.snapshot.v1.llm-providers", JSON.stringify({ providers: [connected], defaults }));
    bridge.providers.mockReturnValue(new Promise(() => {}));
    stopSync = useLLM.getState().startSync();
    expect(useLLM.getState().ready).toBe(true);
    expect(useLLM.getState().providers).toEqual([connected]);
  });

  it("retracts readiness and cached models when the daemon goes offline mid-request", async () => {
    let finish!: (value: ProviderStatus[]) => void;
    bridge.providers.mockImplementation(() => new Promise<ProviderStatus[]>((resolve) => { finish = resolve; }));
    useLLM.setState({ models: { openrouter: { models: [model("a")], loading: false, error: null } } });
    stopSync = useLLM.getState().startSync();
    act(() => useDaemon.setState({ link: "offline" }));
    await act(async () => finish([connected]));
    const state = useLLM.getState();
    expect(state.ready).toBe(false);
    expect(state.error).toContain("offline");
    expect(state.providers).toEqual([]);
    expect(state.models).toEqual({});
  });

  it("discards a model catalogue that was invalidated while it loaded", async () => {
    let finish!: (value: Model[]) => void;
    bridge.models.mockImplementationOnce(() => new Promise<Model[]>((resolve) => { finish = resolve; }));
    stopSync = useLLM.getState().startSync();
    const pending = useLLM.getState().loadModels("openrouter");
    act(() => bridge.changed?.({ id: "openrouter" }));
    await act(async () => { finish([model("stale")]); await pending; });
    expect(useLLM.getState().models.openrouter).toBeUndefined();
  });

  it("loads a catalogue once and retries a failure only when forced", async () => {
    bridge.models.mockRejectedValueOnce(new Error("bad_request: upstream unavailable"));
    await useLLM.getState().loadModels("openrouter");
    expect(useLLM.getState().models.openrouter).toMatchObject({ models: [], loading: false, error: "upstream unavailable" });
    await useLLM.getState().loadModels("openrouter");
    expect(bridge.models).toHaveBeenCalledTimes(1);
    bridge.models.mockResolvedValueOnce([model("a")]);
    await useLLM.getState().loadModels("openrouter", true);
    expect(useLLM.getState().models.openrouter?.models).toEqual([model("a")]);
  });

  it("refcounts sync so the last stop clears the snapshot", async () => {
    const stopA = useLLM.getState().startSync();
    const stopB = useLLM.getState().startSync();
    await waitFor(() => expect(useLLM.getState().ready).toBe(true));
    stopA();
    expect(useLLM.getState().ready).toBe(true);
    stopB();
    expect(useLLM.getState().ready).toBe(false);
    expect(bridge.changed).toBeNull();
  });
});

describe("llm actions", () => {
  const secret = "sk-or-v1-supersecretvalue1234";

  it("forwards the key to the daemon without ever keeping it in store state", async () => {
    bridge.connect.mockResolvedValue(connected);
    const result = await useLLM.getState().connect("openrouter", secret);
    expect(bridge.connect).toHaveBeenCalledWith({ id: "openrouter", apiKey: secret });
    expect(result).toEqual({ ok: true, value: connected });
    expect(JSON.stringify(useLLM.getState())).not.toContain(secret);
    expect(useLLM.getState().providers).toEqual([connected]);
  });

  it("surfaces a rejected key as a message and redacts any echo of it", async () => {
    bridge.connect.mockRejectedValue(new Error(`bad_request: invalid key ${secret}`));
    const result = await useLLM.getState().connect("openrouter", secret);
    expect(result).toEqual({ ok: false, error: "invalid key …" });
    expect(JSON.stringify(useLLM.getState())).not.toContain(secret);
    expect(useLLM.getState().busy).toEqual({});
  });

  it("marks the provider busy while connecting", async () => {
    let finish!: (value: ProviderStatus) => void;
    bridge.connect.mockImplementation(() => new Promise<ProviderStatus>((resolve) => { finish = resolve; }));
    const pending = useLLM.getState().connect("openrouter", secret);
    expect(useLLM.getState().busy).toEqual({ openrouter: "connect" });
    finish(connected);
    await pending;
    expect(useLLM.getState().busy).toEqual({});
  });

  it("disconnect drops the key hint and cached models", async () => {
    useLLM.setState({ providers: [{ ...connected, account: { label: "me" } }], models: { openrouter: { models: [model("a")], loading: false, error: null } } });
    bridge.disconnect.mockResolvedValue(undefined);
    expect((await useLLM.getState().disconnect("openrouter")).ok).toBe(true);
    expect(useLLM.getState().providers).toEqual([none]);
    expect(useLLM.getState().models).toEqual({});
  });

  it("keeps the OpenRouter code out of state and reports the sign-in URL", async () => {
    bridge.authStart.mockResolvedValue({ url: "https://openrouter.ai/auth?x=1" });
    expect(await useLLM.getState().openRouterAuthStart()).toEqual({ ok: true, value: "https://openrouter.ai/auth?x=1" });
    bridge.authFinish.mockResolvedValue(connected);
    expect((await useLLM.getState().openRouterAuthFinish("one-time-code-9999")).ok).toBe(true);
    expect(JSON.stringify(useLLM.getState())).not.toContain("one-time-code-9999");
    expect(useLLM.getState().providers).toEqual([connected]);
  });

  it("stores the defaults the daemon returns", async () => {
    bridge.setDefaults.mockResolvedValue({ chatModel: "openrouter/a", memoryModel: "openrouter/b" });
    await useLLM.getState().setDefaults({ chatModel: "openrouter/a", memoryModel: "openrouter/b" });
    expect(useLLM.getState().defaults).toEqual({ chatModel: "openrouter/a", memoryModel: "openrouter/b" });
  });
});
