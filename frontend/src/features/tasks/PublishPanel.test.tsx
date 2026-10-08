import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { PublishSpec, Session } from "@/mygo";

const api = vi.hoisted(() => ({ publish: vi.fn(), get: vi.fn(), upsert: vi.fn(), toastError: vi.fn(), openExternal: vi.fn() }));
vi.mock("@/mygo", () => ({ Agents: { publish: api.publish, get: api.get }, Shell: { openExternal: api.openExternal } }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/stores/agents", () => ({ useAgents: { getState: () => ({ upsert: api.upsert }) } }));
vi.mock("@/stores/boxes", () => ({ useBoxes: vi.fn() }));
vi.mock("@/features/projects/dialogs", () => ({ toastError: api.toastError }));
import { PublishPanel } from "./PublishPanel";
import { PublishError, publishBlocker, runPublish, specFor, stepsFor, type PublishForm } from "./publish";

const session = (patch: Partial<Session> = {}): Session => ({ id: "s1", title: "Add login", task: "add login", dir: "C:\\wt", branch: "orole/login", status: "idle", mode: "structured", machine: "local", prUrl: "", prNumber: 0, pending: [], ...patch }) as Session;
const form: PublishForm = { message: " feat: login ", title: "", body: "", base: "", draft: false };

beforeEach(() => { vi.clearAllMocks(); api.publish.mockImplementation((_id: string, spec: PublishSpec) => Promise.resolve(session(spec.pr ? { prUrl: "https://github.com/o/r/pull/7", prNumber: 7 } : {}))); api.get.mockResolvedValue(session()); });
afterEach(cleanup);

describe("publish logic", () => {
  it("runs each goal's steps in order", () => {
    expect(stepsFor("commit")).toEqual(["commit"]);
    expect(stepsFor("push")).toEqual(["commit", "push"]);
    expect(stepsFor("pr")).toEqual(["commit", "push", "pr"]);
  });

  it("sends confirmed only on the push and pull request steps", () => {
    expect(specFor("commit", form, true)).toMatchObject({ message: "feat: login", push: false, pr: false, confirmed: false });
    expect(specFor("push", form, false)).toMatchObject({ push: true, pr: false, confirmed: false });
    expect(specFor("push", form, true)).toMatchObject({ push: true, pr: false, confirmed: true });
    expect(specFor("pr", { ...form, title: "T", body: "B", base: "main", draft: true }, true)).toMatchObject({ push: true, pr: true, confirmed: true, title: "T", body: "B", base: "main", draft: true });
  });

  it("reports step states and attributes a failure to its step, leaving later steps pending", async () => {
    const seen: string[] = [];
    api.publish.mockImplementation((_id: string, spec: PublishSpec) => spec.push ? Promise.reject(new Error("publish: push: rejected")) : Promise.resolve(session()));
    const err = await runPublish(session(), "pr", form, true, api.publish, (step, state) => seen.push(`${step}:${state}`)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(PublishError);
    expect((err as PublishError).step).toBe("push");
    expect((err as PublishError).message).toBe("publish: push: rejected");
    expect(seen).toEqual(["commit:pending", "push:pending", "pr:pending", "commit:running", "commit:done", "push:running", "push:failed"]);
    expect(api.publish).toHaveBeenCalledTimes(2);
  });

  it("explains why a session cannot be published", () => {
    expect(publishBlocker(session())).toBeNull();
    expect(publishBlocker(session({ branch: "" }))).toMatch(/no branch/);
    expect(publishBlocker(session({ dir: "" }))).toMatch(/working directory/);
    expect(publishBlocker(session({ mode: "process", machine: "box-1" }))).toMatch(/SSH/);
  });
});

describe("PublishPanel", () => {
  it("is disabled with a reason when the session has no branch", () => {
    render(<PublishPanel session={session({ branch: "" })} />);
    expect(screen.getByRole("status")).toHaveTextContent("no branch");
    for (const name of ["Commit", "Push branch", "Open pull request"]) expect(screen.getByRole("button", { name })).toBeDisabled();
  });

  it("commits without a confirmation dialog", async () => {
    render(<PublishPanel session={session()} />);
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    await waitFor(() => expect(api.publish).toHaveBeenCalledTimes(1));
    expect(api.publish).toHaveBeenCalledWith("s1", expect.objectContaining({ push: false, pr: false, confirmed: false }));
    expect(screen.queryByRole("alertdialog")).toBeNull();
  });

  it("asks for confirmation before pushing, listing branch and remote, and sends confirmed after", async () => {
    render(<PublishPanel session={session()} />);
    fireEvent.click(screen.getByRole("button", { name: "Push branch" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("orole/login");
    expect(dialog).toHaveTextContent("origin");
    expect(api.publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(api.publish).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button", { name: "Push branch" }));
    fireEvent.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Push branch" }));
    await waitFor(() => expect(api.publish).toHaveBeenCalledTimes(2));
    expect(api.publish).toHaveBeenLastCalledWith("s1", expect.objectContaining({ push: true, pr: false, confirmed: true }));
  });

  it("shows the pull request title and base in the confirmation, opens it, and links the result", async () => {
    render(<PublishPanel session={session()} />);
    fireEvent.click(screen.getByRole("button", { name: "Open pull request" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("Add login");
    expect(dialog).toHaveTextContent("default branch");
    fireEvent.click(screen.getByRole("button", { name: "Push and open pull request" }));
    await waitFor(() => expect(api.publish).toHaveBeenCalledTimes(3));
    expect(api.publish).toHaveBeenLastCalledWith("s1", expect.objectContaining({ pr: true, confirmed: true }));
    await waitFor(() => expect(api.upsert).toHaveBeenCalled());
    const list = screen.getByRole("list", { name: "Publish progress" });
    expect(list).toHaveTextContent("Open pull request");
    expect(list.querySelectorAll("li")).toHaveLength(3);
  });

  it("maps a failed step to a toast and keeps the failed step visible", async () => {
    api.publish.mockRejectedValue(new Error("publish: commit: nothing"));
    render(<PublishPanel session={session()} />);
    fireEvent.click(screen.getByRole("button", { name: "Commit" }));
    await waitFor(() => expect(api.toastError).toHaveBeenCalledWith("Could not commit the changes", expect.any(PublishError)));
    expect(screen.getByRole("list", { name: "Publish progress" })).toHaveTextContent("Failed");
  });

  it("shows the existing pull request link instead of the open button", () => {
    render(<PublishPanel session={session({ prUrl: "https://github.com/o/r/pull/7", prNumber: 7 })} />);
    expect(screen.queryByRole("button", { name: "Open pull request" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /Pull request #7/ }));
    expect(api.openExternal).toHaveBeenCalledWith("https://github.com/o/r/pull/7");
  });
});
