import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useUI } from "@/stores/ui";
import { Sidebar } from "./Sidebar";

// The two section bodies are covered by their own tests; here only their place in the one sidebar.
vi.mock("./SidebarProjects", () => ({ SidebarProjects: () => <section aria-label="Code" /> }));
vi.mock("./BotRail", () => ({ BotRail: () => <section aria-label="Bots" /> }));

beforeEach(() => {
  localStorage.clear();
  useUI.setState({ view: "home", paletteOpen: false });
});
afterEach(cleanup);

describe("sidebar", () => {
  it("shows navigation with Automations last, then Bots, then Code, with no mode tabs", () => {
    render(<Sidebar />);
    expect(screen.queryByRole("tab")).toBeNull();
    const links = ["Home", "Tasks", "Review", "Issues", "Automations"].map((name) => screen.getByRole("button", { name }));
    const bots = screen.getByRole("region", { name: "Bots" });
    const code = screen.getByRole("region", { name: "Code" });
    const order = [...links, bots, code];
    for (let i = 1; i < order.length; i++) {
      expect(order[i - 1]!.compareDocumentPosition(order[i]!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    }
  });

  it("navigates between Code views and Automations and marks the current one", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: "Automations" }));
    expect(useUI.getState().view).toBe("automations");
    expect(screen.getByRole("button", { name: "Automations" })).toHaveAttribute("aria-current", "page");
    fireEvent.click(screen.getByRole("button", { name: "Issues" }));
    expect(useUI.getState().view).toBe("issues");
    expect(screen.getByRole("button", { name: "Automations" })).not.toHaveAttribute("aria-current");
  });

  it("keeps Search and Settings", () => {
    render(<Sidebar />);
    fireEvent.click(screen.getByRole("button", { name: /Ctrl\+K/ }));
    expect(useUI.getState().paletteOpen).toBe(true);
    fireEvent.click(screen.getByRole("button", { name: "Settings" }));
    expect(useUI.getState().view).toBe("settings");
  });
});
