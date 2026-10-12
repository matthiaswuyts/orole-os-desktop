import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Skill } from "@/mygo";

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), syncState: vi.fn(), save: vi.fn(), setEnabled: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Skills: api }));
vi.mock("@/features/projects/dialogs", () => ({ ConfirmDialog: () => null, errText: String, toastError: api.toast }));
import { SkillsView } from "./SkillsView";

const now = Math.floor(Date.now() / 1000);
const fullSkill: Skill = { name: "alpha", description: "Alpha skill", body: "---\nname: alpha\ndescription: Alpha skill\n---\nFull instructions", files: ["reference.txt"], enabled: true, source: "imported:codex", addedAt: now - 3 * 86400, updatedAt: now - 3 * 86400 };
const beta: Skill = { ...fullSkill, name: "beta", description: "Beta skill", body: "", files: [], source: "skills.sh:acme/skills/beta", addedAt: now - 3600, updatedAt: now - 3600 };

beforeEach(() => {
  vi.clearAllMocks();
  api.list.mockResolvedValue([{ ...fullSkill, body: "" }, beta]);
  api.syncState.mockResolvedValue([]);
  api.get.mockResolvedValue(fullSkill);
});
afterEach(cleanup);

it("lists every skill, newest first, with its source and when it was added", async () => {
  render(<SkillsView />);
  const list = await screen.findByRole("list", { name: "Your skills" });
  const rows = within(list).getAllByRole("listitem");
  expect(rows.map((r) => within(r).getByRole("button", { name: /alpha|beta/ }).textContent)).toEqual([expect.stringContaining("beta"), expect.stringContaining("alpha")]);
  expect(within(rows[0]!).getByText("From skills.sh")).toBeTruthy();
  expect(within(rows[0]!).getByText("Added 1 hour ago")).toBeTruthy();
  expect(within(rows[1]!).getByText("Imported from Codex")).toBeTruthy();
  expect(within(rows[1]!).getByText("Added 3 days ago")).toBeTruthy();
  expect(within(rows[0]!).getByText("New")).toBeTruthy();
});

it("opens a skill on its own page and saves edits to its contents", async () => {
  api.save.mockImplementation(async (name: string, body: string) => ({ ...fullSkill, name, body }));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  expect(await screen.findByRole("heading", { name: "alpha" })).toBeTruthy();
  expect(api.get).toHaveBeenCalledWith("alpha");
  expect(screen.queryByRole("list", { name: "Your skills" })).toBeNull();
  expect(within(screen.getByRole("complementary", { name: "Details" })).getByText("Codex")).toBeTruthy();

  fireEvent.click(screen.getByRole("tab", { name: /Contents/ }));
  const editor = await screen.findByLabelText("SKILL.md");
  expect(editor).toHaveValue(fullSkill.body);
  fireEvent.change(editor, { target: { value: `${fullSkill.body}\nmore` } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.save).toHaveBeenCalledWith("alpha", `${fullSkill.body}\nmore`));
});

it("goes back to the list", async () => {
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  await screen.findByRole("heading", { name: "alpha" });
  fireEvent.click(screen.getByRole("button", { name: "Your skills" }));
  expect(await screen.findByRole("list", { name: "Your skills" })).toBeTruthy();
});

it("creates a skill on a new page, templating the body from the name", async () => {
  api.save.mockImplementation(async (name: string, body: string) => ({ ...fullSkill, name, body }));
  render(<SkillsView />);
  await screen.findByRole("list", { name: "Your skills" });
  fireEvent.click(screen.getByRole("button", { name: "Add" }));
  fireEvent.click(await screen.findByRole("menuitem", { name: "New skill" }));
  fireEvent.change(await screen.findByLabelText("Skill name"), { target: { value: "My Skill" } });
  expect((screen.getByLabelText("SKILL.md") as HTMLTextAreaElement).value).toContain("name: my-skill");
  fireEvent.change(screen.getByLabelText("SKILL.md"), { target: { value: "---\nname: my-skill\ndescription: Does things\n---\nSteps" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.save).toHaveBeenCalledWith("my-skill", expect.stringContaining("Steps")));
});

it("reports a failed read instead of showing an empty editor", async () => {
  api.get.mockRejectedValue(new Error("Skill read failed"));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  expect(await screen.findByRole("alert")).toHaveTextContent("Skill read failed");
  expect(screen.queryByLabelText("SKILL.md")).toBeNull();
});
