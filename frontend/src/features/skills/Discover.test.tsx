import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { HubEntry, HubPreview, Skill, SyncState } from "@/mygo";

const api = vi.hoisted(() => ({ browse: vi.fn(), search: vi.fn(), describe: vi.fn(), preview: vi.fn(), install: vi.fn(), topics: vi.fn(), topic: vi.fn() }));
vi.mock("@/mygo", () => ({ Skills: { hubBrowse: api.browse, hubSearch: api.search, hubDescribe: api.describe, hubPreview: api.preview, hubInstall: api.install, hubTopics: api.topics, hubTopic: api.topic } }));
vi.mock("@/features/projects/dialogs", () => ({ errText: (e: unknown) => (e instanceof Error ? e.message : String(e)) }));

import { DiscoverTab } from "./DiscoverTab";
import { HarnessStatus } from "./HarnessStatus";

const entry = (source: string, skillId: string, installs: number, official = false): HubEntry => ({ source, skillId, name: skillId, installs, official });
const page = (skills: HubEntry[], hasMore = false) => ({ skills, page: 0, hasMore, total: skills.length });
const preview = (over: Partial<HubPreview> = {}): HubPreview => ({ source: "acme/skills", skillId: "seo-audit", name: "seo-audit", description: "Audit SEO", body: "---\nname: seo-audit\n---\nDo it", files: [], scripts: [], url: "", ...over });
const installed: Skill = { name: "seo-audit", description: "Audit SEO", body: "", files: [], enabled: true, source: "skills.sh:acme/skills/seo-audit", addedAt: 0, updatedAt: 0 };

function renderDiscover(over: Partial<Parameters<typeof DiscoverTab>[0]> = {}) {
  const props = { query: "", added: new Set<string>(), canUpdateAgents: true, onAdded: vi.fn(), onUpdateAgents: vi.fn().mockResolvedValue(undefined), ...over };
  return { ...props, ...render(<DiscoverTab {...props} />) };
}

beforeEach(() => {
  vi.clearAllMocks();
  api.browse.mockResolvedValue(page([entry("acme/skills", "seo-audit", 225_469), entry("vercel-labs/skills", "find-skills", 3_807_588, true)]));
  api.describe.mockResolvedValue([{ source: "acme/skills", skillId: "seo-audit", description: "Audit SEO issues" }]);
  api.preview.mockResolvedValue(preview());
  api.install.mockResolvedValue(installed);
  api.topics.mockResolvedValue([{ id: "marketing", label: "Marketing" }, { id: "design", label: "Design & UI" }]);
  api.topic.mockResolvedValue(page([entry("acme/skills", "copywriting", 0)]));
});
afterEach(cleanup);

describe("DiscoverTab", () => {
  it("lists the catalogue with install counts, then fills in descriptions", async () => {
    renderDiscover();
    expect(await screen.findByRole("button", { name: "Add seo-audit" })).toBeTruthy();
    expect(screen.getByText(/225\.5K/)).toBeTruthy();
    expect(await screen.findByText("Audit SEO issues")).toBeTruthy();
    expect(api.browse).toHaveBeenCalledWith("all-time", 0);
  });

  it("browses by topic, leaving out the featured skill and the install count it does not have", async () => {
    renderDiscover();
    fireEvent.click(await screen.findByRole("button", { name: "Marketing" }));
    expect(await screen.findByRole("button", { name: "Add copywriting" })).toBeTruthy();
    expect(api.topic).toHaveBeenCalledWith("marketing");
    expect(screen.getByRole("heading", { name: "Marketing" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Featured skill" })).toBeNull();
    expect(screen.queryByText(/installs/)).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "All" }));
    expect(await screen.findByRole("button", { name: "Add seo-audit" })).toBeTruthy();
  });

  it("features an official skill and leaves it out of the grid", async () => {
    renderDiscover();
    const hero = await screen.findByRole("region", { name: "Featured skill" });
    expect(within(hero).getByText("find-skills")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Add find-skills" })).toBeNull();
  });

  it("marks a skill that is already added and does not feature it", async () => {
    renderDiscover({ added: new Set(["vercel-labs/skills/find-skills", "acme/skills/seo-audit"]) });
    expect(await screen.findByRole("button", { name: "View seo-audit" })).toBeTruthy();
    expect(screen.queryByRole("region", { name: "Featured skill" })).toBeNull();
  });

  it("searches skills.sh once the query has two characters, instead of browsing", async () => {
    api.search.mockResolvedValue(page([entry("acme/skills", "seo-audit", 5)]));
    const { rerender, ...rest } = renderDiscover();
    await screen.findByRole("button", { name: "Add seo-audit" });
    rerender(<DiscoverTab query="s" added={rest.added} canUpdateAgents onAdded={rest.onAdded} onUpdateAgents={rest.onUpdateAgents} />);
    const { promise: settled, resolve } = Promise.withResolvers<void>();
    setTimeout(resolve, 450);
    await settled;
    expect(api.search).not.toHaveBeenCalled();
    rerender(<DiscoverTab query="seo" added={rest.added} canUpdateAgents onAdded={rest.onAdded} onUpdateAgents={rest.onUpdateAgents} />);
    await waitFor(() => expect(api.search).toHaveBeenCalledExactlyOnceWith("seo"));
    expect(await screen.findByText("Results for “seo”")).toBeTruthy();
  });

  it("shows a retryable error when skills.sh cannot be reached", async () => {
    api.browse.mockRejectedValueOnce(new Error("skills.sh unavailable: timeout"));
    renderDiscover();
    expect(await screen.findByText(/skills\.sh unavailable: timeout/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(await screen.findByRole("button", { name: "Add seo-audit" })).toBeTruthy();
  });

  it("shows the whole SKILL.md and warns about scripts before anything is added", async () => {
    api.preview.mockResolvedValue(preview({ scripts: ["scripts/run.sh"], files: ["scripts/run.sh", "notes.md"] }));
    const { onAdded } = renderDiscover();
    fireEvent.click(await screen.findByRole("button", { name: "Add seo-audit" }));
    expect(await screen.findByLabelText("SKILL.md contents")).toHaveTextContent("Do it");
    expect(screen.getByText("Includes scripts")).toBeTruthy();
    expect(api.install).not.toHaveBeenCalled();
    expect(onAdded).not.toHaveBeenCalled();
  });

  it("adds from the preview, then offers to update the agents", async () => {
    const { onAdded, onUpdateAgents } = renderDiscover();
    fireEvent.click(await screen.findByRole("button", { name: "Add seo-audit" }));
    await screen.findByLabelText("SKILL.md contents");
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(await screen.findByText("Added to your skills")).toBeTruthy();
    expect(api.install).toHaveBeenCalledExactlyOnceWith("acme/skills", "seo-audit");
    expect(onAdded).toHaveBeenCalledWith(installed);
    fireEvent.click(screen.getByRole("button", { name: "Update agents" }));
    await waitFor(() => expect(onUpdateAgents).toHaveBeenCalledOnce());
    expect(await screen.findByText("Copied into your coding agents.")).toBeTruthy();
  });

  it("reports a failed add and keeps the dialog open", async () => {
    api.install.mockRejectedValue(new Error("invalid skill: already in the registry"));
    renderDiscover();
    fireEvent.click(await screen.findByRole("button", { name: "Add seo-audit" }));
    await screen.findByLabelText("SKILL.md contents");
    fireEvent.click(screen.getByRole("button", { name: "Add skill" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("already in the registry");
    expect(screen.getByRole("button", { name: "Add skill" })).toBeTruthy();
  });
});

describe("HarnessStatus", () => {
  const state = (over: Partial<SyncState>): SyncState => ({ harness: "claude", name: "Claude Code", available: true, dir: "C:\\skills", synced: [], outdated: [], missing: [], gateway: "installed", error: "", ...over });
  const mount = (states: SyncState[], onSync = vi.fn()) => ({ onSync, ...render(<HarnessStatus states={states} error={null} syncing={null} onSync={onSync} onRetry={vi.fn()} />) });

  it("says everything is up to date when no agent drifted", () => {
    mount([state({}), state({ harness: "codex", name: "Codex", available: false })]);
    expect(screen.getByText("All agents up to date")).toBeTruthy();
    expect(screen.queryByRole("button", { name: /^Update/ })).toBeNull();
  });

  it("offers one button that updates every agent that needs it", () => {
    const { onSync } = mount([state({ missing: ["a"] }), state({ harness: "codex", name: "Codex", outdated: ["b"] })]);
    fireEvent.click(screen.getByRole("button", { name: "Update 2 agents" }));
    expect(onSync).toHaveBeenCalledExactlyOnceWith("");
  });
});
