import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MemoryEntry, MemoryScope } from "@/mygo";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { useChat } from "@/stores/chat";
import { useChatMemory } from "@/stores/chatMemory";
import { initialForm } from "./botForm";
import { MemoryTab } from "./MemoryTab";
import { bot, bridge, emit, resetBridge } from "./testing";

const scopes: MemoryScope[] = [
  { id: "bot:b1", name: "Travel notes", kind: "bot", count: 2, used: 100, limit: 2200 },
  { id: "user", name: "About you", kind: "user", count: 1, used: 10, limit: 1400 },
  { id: "shared:home", name: "Home", kind: "shared", count: 3, used: 50, limit: 2200 },
];

const entry = (id: string, text: string, over: Partial<MemoryEntry> = {}): MemoryEntry => ({
  id, scope: "bot:b1", kind: "fact", text, trust: "bot", author: "bot:b1", pending: false, createdAt: 1_700_000_000_000, updatedAt: 1_700_000_000_000, ...over,
});

function renderTab(botId: string | null = "b1", dispatch = vi.fn()) {
  render(<MemoryTab botId={botId} form={initialForm()} dispatch={dispatch} />);
  return dispatch;
}

beforeEach(() => {
  resetBridge();
  useChatMemory.setState({ scopes: {}, entries: {} });
  useChat.setState({ bots: [bot("b1", { name: "Travel" })] });
  bridge.chat.memoryScopes.mockResolvedValue(scopes);
});
afterEach(cleanup);

describe("scopes", () => {
  it("asks to save a new bot first and loads nothing", () => {
    renderTab(null);
    expect(screen.getByText(/Save the bot first/)).toBeInTheDocument();
    expect(bridge.chat.memoryScopes).not.toHaveBeenCalled();
    expect(bridge.chat.memoryEntries).not.toHaveBeenCalled();
  });

  it("shows usage and says memory is local and sent to the provider", async () => {
    renderTab();
    expect(await screen.findByText(/100 of 2,200 core characters · 2 entries/)).toBeInTheDocument();
    expect(screen.getByText(/1 entry/)).toBeInTheDocument();
    expect(screen.getByText(/stored on this computer/)).toBeInTheDocument();
    expect(screen.getByText(/included in its prompts/)).toBeInTheDocument();
    expect(screen.getByRole("meter", { name: "Core memory used in Travel notes" })).toBeInTheDocument();
  });

  it("offers access toggles for shared scopes only and reports them to the form", async () => {
    const dispatch = renderTab();
    await screen.findByText("Travel notes");
    expect(screen.queryByRole("switch", { name: "Read Travel notes" })).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Write Home" }));
    expect(dispatch).toHaveBeenCalledWith({ type: "access", scope: "shared:home", read: false, write: true });
  });

  it("creates a shared scope and shows daemon errors verbatim", async () => {
    bridge.chat.createMemoryScope.mockRejectedValueOnce(new Error("bad_request: A scope with that name already exists.")).mockResolvedValue({ id: "shared:household", name: "Household", kind: "shared", count: 0, used: 0, limit: 2200 });
    renderTab();
    await screen.findByText("Travel notes");
    fireEvent.change(screen.getByLabelText("New shared scope name"), { target: { value: "Household" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("A scope with that name already exists.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() => expect(bridge.chat.createMemoryScope).toHaveBeenCalledTimes(2));
    expect(bridge.chat.createMemoryScope).toHaveBeenLastCalledWith("Household");
    await waitFor(() => expect(bridge.chat.memoryScopes.mock.calls.length).toBeGreaterThan(1));
  });

  it("deletes a shared scope only after confirmation and drops its grant", async () => {
    bridge.chat.deleteMemoryScope.mockResolvedValue(undefined);
    const dispatch = renderTab();
    await screen.findByRole("button", { name: "Delete shared scope Home" });
    fireEvent.click(screen.getByRole("button", { name: "Delete shared scope Home" }));
    expect(await screen.findByText(/All 3 entries in it are deleted/)).toBeInTheDocument();
    expect(bridge.chat.deleteMemoryScope).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByText(/All 3 entries/)).toBeNull());
    fireEvent.click(screen.getByRole("button", { name: "Delete shared scope Home" }));
    fireEvent.click(await screen.findByRole("button", { name: "Delete scope" }));
    await waitFor(() => expect(bridge.chat.deleteMemoryScope).toHaveBeenCalledWith("shared:home"));
    expect(dispatch).toHaveBeenCalledWith({ type: "access", scope: "shared:home", read: false, write: false });
  });

  it("never offers to delete the private or user scope", async () => {
    renderTab();
    await screen.findAllByText("About you");
    expect(screen.queryByRole("button", { name: "Delete shared scope Travel notes" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Delete shared scope About you" })).toBeNull();
  });
});

describe("entries", () => {
  it("lists entries with kind, trust, author and pending state", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([
      entry("m1", "Prefers trains", { trust: "user", author: "user", kind: "core" }),
      entry("m2", "Booked via a shady site", { trust: "untrusted" }),
      entry("m3", "Likes window seats", { pending: true }),
    ]);
    renderTab();
    expect(await screen.findByText("Prefers trains")).toBeInTheDocument();
    expect(bridge.chat.memoryEntries).toHaveBeenCalledWith({ scope: "bot:b1", kind: "", includePending: true, includeInvalidated: false, query: "", limit: 500 });
    const active = within(screen.getByRole("list", { name: "Memories" }));
    expect(active.getByText("You")).toBeInTheDocument();
    expect(active.getByText("Core note")).toBeInTheDocument();
    expect(active.getByText("Untrusted")).toBeInTheDocument();
    const pending = within(screen.getByRole("list", { name: "Pending memories" }));
    expect(pending.getByText("Likes window seats")).toBeInTheDocument();
    expect(pending.getByText("Pending approval")).toBeInTheDocument();
    expect(pending.getAllByText(/Travel/).length).toBeGreaterThan(0);
  });

  it("approves and rejects pending writes through the daemon", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([entry("m1", "Likes trains", { pending: true }), entry("m2", "Hates planes", { pending: true })]);
    bridge.chat.resolveMemory.mockResolvedValue(undefined);
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Approve memory: Likes trains" }));
    await waitFor(() => expect(bridge.chat.resolveMemory).toHaveBeenCalledWith({ id: "m1", approve: true }));
    fireEvent.click(screen.getByRole("button", { name: "Reject memory: Hates planes" }));
    await waitFor(() => expect(bridge.chat.resolveMemory).toHaveBeenCalledWith({ id: "m2", approve: false }));
    await waitFor(() => expect(bridge.chat.memoryEntries.mock.calls.length).toBeGreaterThan(2));
  });

  it("shows why a pending decision failed", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([entry("m1", "Likes trains", { pending: true })]);
    bridge.chat.resolveMemory.mockRejectedValue(new Error("not_found: that memory is no longer pending"));
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Approve memory: Likes trains" }));
    expect(await screen.findByText("that memory is no longer pending")).toBeInTheDocument();
  });

  it("hides invalidated entries until history is shown, then strikes them through", async () => {
    bridge.chat.memoryEntries.mockImplementation(async (req: { includeInvalidated: boolean }) => [
      entry("m1", "Lives in Oslo"),
      ...(req.includeInvalidated ? [entry("m0", "Lives in Bergen", { invalidatedAt: 1_700_000_100_000 })] : []),
    ]);
    renderTab();
    await screen.findByText("Lives in Oslo");
    expect(screen.queryByText("Lives in Bergen")).toBeNull();
    fireEvent.click(screen.getByRole("switch", { name: "Show history" }));
    const old = await screen.findByText("Lives in Bergen");
    expect(old).toHaveClass("line-through");
    expect(screen.getByText("Replaced or forgotten")).toBeInTheDocument();
    expect(bridge.chat.memoryEntries).toHaveBeenLastCalledWith(expect.objectContaining({ includeInvalidated: true }));
    expect(within(old.closest("li")!).queryByRole("button", { name: /Edit memory/ })).toBeNull();
  });

  it("adds an entry as the user and keeps the draft when the daemon refuses", async () => {
    bridge.chat.saveMemoryEntry.mockRejectedValueOnce(new Error("bad_request: This memory is too long.")).mockResolvedValue(entry("new", "Allergic to nuts", { trust: "user", author: "user" }));
    renderTab();
    await screen.findByText("Travel notes");
    fireEvent.click(screen.getByRole("button", { name: "Add memory" }));
    fireEvent.change(await screen.findByLabelText("New memory"), { target: { value: "  Allergic to nuts " } });
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    expect(await screen.findByText("This memory is too long.")).toBeInTheDocument();
    expect(screen.getByLabelText("New memory")).toHaveValue("  Allergic to nuts ");
    fireEvent.click(screen.getByRole("button", { name: "Add" }));
    await waitFor(() => expect(bridge.chat.saveMemoryEntry).toHaveBeenCalledTimes(2));
    expect(bridge.chat.saveMemoryEntry).toHaveBeenLastCalledWith({ id: "", scope: "bot:b1", kind: "fact", text: "Allergic to nuts" });
    await waitFor(() => expect(screen.queryByLabelText("New memory")).toBeNull());
  });

  it("edits an entry inline", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([entry("m1", "Prefers trains", { kind: "core" })]);
    bridge.chat.saveMemoryEntry.mockResolvedValue(entry("m1", "Prefers night trains", { kind: "core", trust: "user" }));
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Edit memory: Prefers trains" }));
    fireEvent.change(screen.getByLabelText("Edit memory"), { target: { value: "Prefers night trains" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(bridge.chat.saveMemoryEntry).toHaveBeenCalledWith({ id: "m1", scope: "bot:b1", kind: "core", text: "Prefers night trains" }));
  });

  it("deletes an entry only after confirmation", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([entry("m1", "Prefers trains")]);
    bridge.chat.deleteMemoryEntry.mockResolvedValue(undefined);
    renderTab();
    fireEvent.click(await screen.findByRole("button", { name: "Delete memory: Prefers trains" }));
    expect(bridge.chat.deleteMemoryEntry).not.toHaveBeenCalled();
    fireEvent.click(await screen.findByRole("button", { name: "Delete memory" }));
    await waitFor(() => expect(bridge.chat.deleteMemoryEntry).toHaveBeenCalledWith("m1"));
  });

  it("searches through the daemon", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([]);
    renderTab();
    await screen.findByText("Nothing is remembered here yet.");
    fireEvent.change(screen.getByLabelText("Search memory"), { target: { value: "trains" } });
    await waitFor(() => expect(bridge.chat.memoryEntries).toHaveBeenLastCalledWith(expect.objectContaining({ scope: "bot:b1", query: "trains" })));
    expect(await screen.findByText("Nothing matches your search.")).toBeInTheDocument();
  });

  it("refreshes when the daemon reports a memory change", async () => {
    bridge.chat.memoryEntries.mockResolvedValueOnce([]).mockResolvedValue([entry("m9", "Just learned this")]);
    renderTab();
    await screen.findByText("Nothing is remembered here yet.");
    emit("chatMemory", { scope: "bot:b1" });
    expect(await screen.findByText("Just learned this")).toBeInTheDocument();
    expect(bridge.chat.memoryScopes.mock.calls.length).toBeGreaterThan(1);
  });

  it("ignores a change in another scope", async () => {
    bridge.chat.memoryEntries.mockResolvedValue([]);
    renderTab();
    await screen.findByText("Nothing is remembered here yet.");
    const before = bridge.chat.memoryEntries.mock.calls.length;
    emit("chatMemory", { scope: "shared:home" });
    await waitFor(() => expect(bridge.chat.memoryScopes.mock.calls.length).toBeGreaterThan(1));
    expect(bridge.chat.memoryEntries.mock.calls.length).toBe(before);
  });
});
