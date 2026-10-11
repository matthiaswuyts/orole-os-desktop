import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUI } from "@/stores/ui";
import { Sidebar } from "./Sidebar";

// The two sidebar bodies are covered by their own tests; here only which one the mode shows.
vi.mock("./SidebarProjects", () => ({ SidebarProjects: () => <section aria-label="Projects" /> }));
vi.mock("./BotRail", () => ({ BotRail: () => <section aria-label="Bot rail" /> }));

beforeEach(() => {
  localStorage.clear();
  useUI.setState({ view: "home", mode: "code", lastCodeView: "home" });
});
afterEach(cleanup);

describe("sidebar mode tabs", () => {
  it("shows the Code navigation and projects in Code mode, without the bot rail", () => {
    render(<Sidebar />);
    expect(screen.getByRole("tab", { name: "Code" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: "Bots" })).toHaveAttribute("aria-selected", "false");
    for (const label of ["Home", "Tasks", "Review", "Issues", "Automations"]) expect(screen.getByRole("button", { name: label })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Projects" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Bot rail" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Chat" })).toBeNull();
  });

  it("switches to the bot rail in Chat mode, replacing navigation and projects", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "chat" });
    expect(screen.getByRole("region", { name: "Bot rail" })).toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Projects" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Tasks" })).toBeNull();
    // The palette button is a Code-mode control; Settings stays.
    expect(screen.queryByRole("button", { name: /Ctrl\+K/ })).toBeNull();
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
  });

  it("returns to the last Code view when switching back", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: "Issues" }));
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    fireEvent.click(screen.getByRole("tab", { name: "Code" }));
    expect(useUI.getState()).toMatchObject({ mode: "code", view: "issues" });
    expect(screen.getByRole("button", { name: "Issues" })).toHaveAttribute("aria-current", "page");
  });

  it("keeps Settings reachable in Chat mode and the mode tab leads back out of it", () => {
    useUI.setState({ mode: "chat", view: "chat" });
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(useUI.getState()).toMatchObject({ mode: "chat", view: "settings" });
    expect(screen.getByRole("region", { name: "Bot rail" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab", { name: "Bots" }));
    expect(useUI.getState().view).toBe("chat");
  });
});
