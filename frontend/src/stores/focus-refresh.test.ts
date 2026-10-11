import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { FOCUS_MIN_INTERVAL_MS } from "@/lib/focus";

/** Every backend call the stores make, keyed "Namespace.method". Each call fails, which the stores report as state. */
const calls = vi.hoisted(() => ({ log: [] as string[] }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => {
  const api = (namespace: string) => new Proxy({}, { get: (_, method: string) => async () => { calls.log.push(`${namespace}.${method}`); throw new Error("offline"); } });
  return {
    Chat: api("Chat"), LLM: api("LLM"), Plugins: api("Plugins"), Notifications: api("Notifications"), GitHub: api("GitHub"),
    Projects: api("Projects"), Worktrees: api("Worktrees"), Daemon: api("Daemon"),
    events: new Proxy({}, { get: () => ({ on: () => () => {} }) }),
  };
});

import { useChat } from "@/stores/chat";
import { useChatMessages } from "@/stores/chatMessages";
import { useDaemon } from "@/stores/daemon";
import { useLLM } from "@/stores/llm";
import { useNotifications } from "@/stores/notifications";
import { usePlugins } from "@/stores/plugins";
import { useProjects } from "@/stores/projects";
import { useGitHub } from "@/features/github/store";

const focus = () => window.dispatchEvent(new Event("focus"));
const settle = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
const counts = () => Object.fromEntries([...new Set(calls.log)].sort().map((name) => [name, calls.log.filter((c) => c === name).length]));
let stops: (() => void)[] = [];

beforeEach(async () => {
  vi.useFakeTimers({ toFake: ["Date"] });
  vi.setSystemTime(1_000_000);
  useDaemon.setState({ link: "connected" });
  stops = [
    useChat.getState().startSync(), useChatMessages.getState().startSync(), useLLM.getState().startSync(), useNotifications.getState().startSync(),
    usePlugins.getState().startSync(), useProjects.getState().startSync(), useGitHub.getState().startSync(), useGitHub.getState().startSync(),
  ];
  await settle();
  calls.log.length = 0;
});
afterEach(() => { stops.forEach((stop) => stop()); vi.useRealTimers(); });

describe("window focus fans out to every store through one throttled signal", () => {
  it("makes no backend call for a burst of focus events right after start-up", async () => {
    for (let i = 0; i < 10; i++) focus();
    await settle();
    expect(counts()).toEqual({});
  });

  it("refreshes each store exactly once per focus burst, however many events and subscribers", async () => {
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    for (let i = 0; i < 10; i++) focus();
    await settle();
    expect(counts()).toEqual({
      "Chat.bots": 1, "GitHub.status": 1, "LLM.defaults": 1, "LLM.manifests": 1, "LLM.providers": 1,
      "Notifications.list": 1, "Notifications.prefs": 1, "Plugins.list": 1, "Projects.list": 1,
    });
    calls.log.length = 0;
    vi.setSystemTime(Date.now() + 5_000);
    for (let i = 0; i < 10; i++) focus();
    await settle();
    expect(counts()).toEqual({});
  });

  it("never fetches worktrees on focus while no view needs them", async () => {
    useProjects.setState({ projects: [{ id: "a", name: "a", isGit: true, missing: false } as never], worktrees: {}, worktreesAt: {} });
    vi.setSystemTime(Date.now() + FOCUS_MIN_INTERVAL_MS);
    focus();
    await settle();
    expect(calls.log.filter((c) => c.startsWith("Worktrees."))).toEqual([]);
  });
});
