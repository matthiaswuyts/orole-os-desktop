import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { Skill } from "@/mygo";

const api = vi.hoisted(() => ({ list: vi.fn(), get: vi.fn(), syncState: vi.fn(), save: vi.fn(), toast: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({ Skills: api }));
vi.mock("@/features/projects/dialogs", () => ({ ConfirmDialog: () => null, errText: String, toastError: api.toast }));
import { SkillsView } from "./SkillsView";

const fullSkill: Skill = { name: "alpha", description: "Alpha skill", body: "---\nname: alpha\ndescription: Alpha skill\n---\nFull instructions", files: ["reference.txt"], enabled: true, source: "orole", updatedAt: 0 };

beforeEach(() => {
  vi.clearAllMocks();
  api.list.mockResolvedValue([{ ...fullSkill, body: "" }, { ...fullSkill, name: "beta", description: "Beta skill", body: "" }]);
  api.syncState.mockResolvedValue([]);
  api.get.mockResolvedValue(fullSkill);
});
afterEach(cleanup);

it("fetches the full skill before editing and saving metadata-only list entries", async () => {
  api.save.mockImplementation(async (name: string, body: string) => ({ ...fullSkill, name, body }));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  const editor = await screen.findByLabelText("SKILL.md");
  expect(api.get).toHaveBeenCalledWith("alpha");
  expect(editor).toHaveValue(fullSkill.body);
  fireEvent.change(editor, { target: { value: `${fullSkill.body}\nMore instructions` } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => expect(api.save).toHaveBeenCalledWith("alpha", `${fullSkill.body}\nMore instructions`));
});

it("ignores out-of-order skill responses", async () => {
  let resolveAlpha!: (skill: Skill) => void;
  api.get.mockImplementation((name: string) => name === "alpha" ? new Promise<Skill>((resolve) => { resolveAlpha = resolve; }) : Promise.resolve({ ...fullSkill, name: "beta", body: "Beta full body" }));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  fireEvent.click(screen.getByRole("button", { name: /beta/ }));
  expect(await screen.findByLabelText("SKILL.md")).toHaveValue("Beta full body");
  await act(async () => { resolveAlpha(fullSkill); });
  expect(screen.getByLabelText("SKILL.md")).toHaveValue("Beta full body");
});

it("does not replace a new draft when a prior skill request finishes", async () => {
  let resolve!: (skill: Skill) => void;
  api.get.mockReturnValue(new Promise<Skill>((done) => { resolve = done; }));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  fireEvent.click(screen.getByRole("button", { name: "New skill" }));
  fireEvent.change(screen.getByLabelText("SKILL.md"), { target: { value: "New instructions" } });
  await act(async () => { resolve(fullSkill); });
  expect(screen.getByLabelText("Skill name")).toHaveValue("");
  expect(screen.getByLabelText("SKILL.md")).toHaveValue("New instructions");
});

it("reports failed reads without exposing a blank editable replacement", async () => {
  api.get.mockRejectedValue(new Error("Skill read failed"));
  render(<SkillsView />);
  fireEvent.click(await screen.findByRole("button", { name: /alpha/ }));
  await waitFor(() => expect(api.toast).toHaveBeenCalledWith("Could not open skill", expect.any(Error)));
  expect(screen.queryByLabelText("SKILL.md")).toBeNull();
});
