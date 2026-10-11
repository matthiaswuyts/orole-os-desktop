import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { useLLM } from "@/stores/llm";
import { useUI } from "@/stores/ui";
import { ModelPicker } from "./ModelPicker";
import { useModelOptions } from "./modelOptions";
import { model, provider, resetBridge } from "./testing";

// jsdom has no layout; the virtualized model list renders nothing in a zero-height scroller.
Object.defineProperty(HTMLElement.prototype, "offsetHeight", { configurable: true, value: 320 });
Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, value: 560 });

function Harness({ selected, onPick }: { selected: string; onPick: (value: string) => void }) {
  const models = useModelOptions(selected);
  return <ModelPicker id="m" models={models} onPick={(o) => { if (o) onPick(o.value); }} />;
}

beforeEach(() => {
  Element.prototype.scrollTo = () => {};
  resetBridge();
  useLLM.setState({
    providers: [provider(), provider({ id: "claude", name: "Claude", kind: "account" })],
    defaults: { chatModel: "", memoryModel: "" },
    models: {
      openrouter: { models: [model("acme/alpha", { name: "Alpha" }), model("acme/beta", { name: "Beta" })], loading: false, error: null },
      claude: { models: [model("sonnet", { name: "Claude Sonnet" })], loading: false, error: null },
    },
    ready: true,
  });
});
afterEach(cleanup);

describe("provider chooser", () => {
  it("offers each connected provider, says whether it is an account or an API key, and narrows the list", () => {
    const picked: string[] = [];
    render(<Harness selected="openrouter/acme/alpha" onPick={(v) => picked.push(v)} />);
    expect(screen.getByRole("button", { name: /Alpha/ })).toHaveTextContent("via OpenRouter");
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));

    const group = screen.getByRole("radiogroup", { name: "Provider" });
    expect(within(group).getByRole("radio", { name: /All/ })).toBeChecked();
    expect(within(group).getByRole("radio", { name: /Claude.*Your account/ })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: /OpenRouter.*API key/ })).toBeInTheDocument();
    expect(screen.getAllByRole("option").length).toBeGreaterThan(2);

    fireEvent.click(within(group).getByRole("radio", { name: /Claude/ }));
    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(1);
    expect(options[0]).toHaveTextContent("Claude Sonnet");
    expect(options[0]).toHaveTextContent("via Claude");
    fireEvent.click(options[0]!);
    expect(picked).toEqual(["claude/sonnet"]);
  });

  it("shows the account behind the chosen model and a way to connect more", () => {
    render(<Harness selected="claude/sonnet" onPick={() => {}} />);
    expect(screen.getByRole("button", { name: /Claude Sonnet/ })).toHaveTextContent("via Claude account");
    fireEvent.click(screen.getByRole("button", { name: /Claude Sonnet/ }));
    fireEvent.click(screen.getByRole("button", { name: /Use a Claude, ChatGPT or Grok account/ }));
    expect(useUI.getState().view).toBe("settings");
    expect(useUI.getState().settingsSection).toBe("ai");
  });

  it("has no chooser when only one provider is connected", () => {
    useLLM.setState({ providers: [provider()] });
    render(<Harness selected="openrouter/acme/alpha" onPick={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    expect(screen.queryByRole("radiogroup", { name: "Provider" })).toBeNull();
  });
});
