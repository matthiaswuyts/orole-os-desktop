import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const bridge = vi.hoisted(() => ({ openSettings: vi.fn() }));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async (importOriginal) => ({ ...(await importOriginal<Record<string, unknown>>()), Desktop: { openSettings: bridge.openSettings } }));
vi.mock("@/stores/boxes", async (importOriginal) => ({ ...(await importOriginal<Record<string, unknown>>()), startBoxesPolling: () => () => {} }));

import { useBoxes } from "@/stores/boxes";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { useChat } from "@/stores/chat";
import { StatusBar } from "./StatusBar";

beforeEach(() => {
  bridge.openSettings.mockReset().mockResolvedValue(undefined);
  useDaemon.setState({ link: "connected", health: { version: "1.2.3" } as never });
  useBoxes.setState({ machines: [], observations: {}, loaded: true, error: undefined, now: Date.now(), mcp: [{ status: "connected" }, { status: "disabled" }] as never, mcpError: undefined });
});
afterEach(cleanup);

describe("status bar", () => {
  it("opens the settings window at Integrations from the MCP count", () => {
    render(<StatusBar />);
    fireEvent.click(screen.getByRole("button", { name: /mcp/ }));
    expect(bridge.openSettings).toHaveBeenCalledWith("integrations");
  });

  it("shows a live daemon with a success-coloured dot, not a grey one", () => {
    render(<StatusBar />);
    const dot = document.querySelector("[data-state=connected]")!;
    expect(dot.className).toContain("bg-success");
    cleanup();
    useDaemon.setState({ link: "offline" });
    render(<StatusBar />);
    expect(document.querySelector("[data-state=offline]")!.className).not.toContain("bg-success");
  });

  it("opens the agents view from the tasks count", () => {
    render(<StatusBar />);
    fireEvent.click(screen.getByRole("button", { name: /tasks/ }));
    expect(useUI.getState().view).toBe("agents");
  });

  it("counts bots replying now out of all bots, ignoring stale entries for deleted bots, and opens chat", () => {
    useChat.setState({ ready: false, bots: [], running: {} });
    render(<StatusBar />);
    expect(screen.getByRole("button", { name: /bots/ })).toHaveTextContent("bots —");
    cleanup();
    useChat.setState({ ready: true, bots: [{ id: "a" }, { id: "b" }, { id: "c" }] as never, running: { a: true, b: false, gone: true } });
    render(<StatusBar />);
    expect(screen.getByRole("button", { name: /bots/ })).toHaveTextContent("bots 1/3");
    fireEvent.click(screen.getByRole("button", { name: /bots/ }));
    expect(useUI.getState().view).toBe("chat");
  });
});
