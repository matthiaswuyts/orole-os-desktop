import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Workspace } from "@/mygo";
import { useWorkspaces } from "./workspaces";
const bridge = vi.hoisted(() => ({ list: vi.fn(), save: vi.fn(), delete: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true, previewKey: "workspace-test" }));
vi.mock("@/mygo", () => ({ Workspaces: bridge }));
const stored = new Map<string, Workspace>();
const workspace = (id: string): Workspace => ({ id, name: id, position: id === "a" ? 0 : 1, layout: { widgets: [] } });
beforeEach(async () => {
  vi.useFakeTimers();
  vi.resetAllMocks();
  localStorage.clear();
  stored.clear();
  stored.set("a", workspace("a")); stored.set("b", workspace("b"));
  bridge.list.mockImplementation(async () => structuredClone([...stored.values()]));
  bridge.save.mockImplementation(async (value: Workspace) => { stored.set(value.id, structuredClone(value)); return value; });
  bridge.delete.mockImplementation(async (id: string) => { stored.delete(id); });
  await useWorkspaces.getState().load();
});
afterEach(async () => { await vi.runAllTimersAsync(); vi.useRealTimers(); });

it("persists edits to both workspaces when switching before the debounce fires", async () => {
  useWorkspaces.getState().addWidget("notes", 4, 5);
  useWorkspaces.getState().select("b");
  useWorkspaces.getState().addWidget("clock", 4, 5);
  await vi.runAllTimersAsync();
  await useWorkspaces.getState().load();
  expect(useWorkspaces.getState().list.map((ws) => [ws.id, ws.layout.widgets.map((w) => w.type)])).toEqual([["a", ["notes"]], ["b", ["clock"]]]);
});

it("does not let a slow older save overwrite a newer layout", async () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  bridge.save.mockImplementationOnce(async (value: Workspace) => { await gate; stored.set(value.id, structuredClone(value)); return value; });
  useWorkspaces.getState().addWidget("notes", 4, 5);
  await vi.advanceTimersByTimeAsync(400);
  useWorkspaces.getState().addWidget("clock", 4, 5);
  await vi.advanceTimersByTimeAsync(400);
  release();
  await vi.runAllTimersAsync();
  await useWorkspaces.getState().load();
  expect(useWorkspaces.getState().list[0]!.layout.widgets.map((w) => w.type)).toEqual(["notes", "clock"]);
});

it("orders deletion after an in-flight save without recreating the deleted workspace", async () => {
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  bridge.save.mockImplementationOnce(async (value: Workspace) => { await gate; stored.set(value.id, structuredClone(value)); return value; });
  useWorkspaces.getState().addWidget("notes", 4, 5);
  await vi.advanceTimersByTimeAsync(400);
  const removing = useWorkspaces.getState().remove("a");
  release();
  await removing;
  await vi.runAllTimersAsync();
  await useWorkspaces.getState().load();
  expect(useWorkspaces.getState().list.map((w) => w.id)).toEqual(["b"]);
});
