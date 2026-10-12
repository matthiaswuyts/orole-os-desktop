import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUI } from "@/stores/ui";
import { Sidebar } from "./Sidebar";

// The two sidebar bodies are covered by their own tests; here only which one the mode shows.
vi.mock("./SidebarProjects", () => ({ SidebarProjects: () => <section aria-label="Projects" /> }));
vi.mock("./BotRail", () => ({ BotRail: () => <section aria-label="Bot rail" /> }));

beforeEach(() => {
  localStorage.clear();
  useUI.setState({ view: "home", mode: "code", lastCodeView: "home", lastBotsView: "chat", paletteOpen: false });
});
afterEach(cleanup);

describe("sidebar mode tabs", () => {
  it("shows the Code navigation and projects in Code mode, without the bot rail or Automations", () => {
    render(<Sidebar />);
    expect(screen.getByRole("tab", { name: "Code" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Bots" })).toHaveAttribute("aria-selected", "false");
    for (const label of ["Home", "Tasks", "Review", "Issues"]) expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Automations" })).toBeNull();
    expect(screen.getByRole("region", { name: "Projects" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Bot rail" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Chat" })).toBeNull();
  });

  it("shows Search, Automations and the bot rail in Bots mode, replacing navigation and projects", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "chat" });
    expect(screen.getByRole("region", { name: "Bot rail" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Projects" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Tasks" })).toBeNull();
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Ctrl\+K/ }));
    expect(useUI.getState().paletteOpen).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Automations" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "automations" });
    expect(screen.getByRole("button", { name: "Automations" })).toHaveAttribute("aria-current", "page");
  });

  it("returns to the last view of each mode when switching tabs", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: "Issues" }));
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    fireEvent.click(screen.getByRole("button", { name: "Automations" }));
    fireEvent.click(screen.getByRole("tab", { name: "Code" }));
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "issues" });
    expect(screen.getByRole("button", { name: "Issues" })).toHaveAttribute("aria-current", "page");
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "automations" });
  });

  it("keeps Settings reachable in Bots mode and the mode tab leads back out of it", () => {
    useUI.setState({ mode: "chat", view: "chat" });
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "settings" });
    expect(screen.getByRole("region", { name: "Bot rail" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    expect(useUI.getState().view).toBe("chat");
  });
});
