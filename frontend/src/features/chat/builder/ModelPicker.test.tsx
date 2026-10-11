import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", async () => (await import("./testing")).mygoMock);

import { useLLM } from "@/stores/llm";
import { ModelPicker } from "./ModelPicker";
import { useModelOptions } from "./modelOptions";
import { bridge, model, provider, resetBridge } from "./testing";

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
    expect(bridge.desktop.openSettings).toHaveBeenCalledWith("ai"); // settings open in their own window
  });

  it("offers the company filter for All and for an aggregator, not for a single-vendor account", () => {
    useLLM.setState({ models: {
      openrouter: { models: [model("anthropic/claude-x", { name: "Claude X" }), model("openai/gpt-x", { name: "GPT X" })], loading: false, error: null },
      claude: { models: [model("sonnet", { name: "Claude Sonnet" }), model("opus", { name: "Claude Opus" })], loading: false, error: null },
    } });
    render(<Harness selected="claude/sonnet" onPick={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Claude Sonnet/ }));
    const group = screen.getByRole("radiogroup", { name: "Provider" });
    expect(screen.getByRole("group", { name: "Filter by company" })).toBeInTheDocument();
    fireEvent.click(within(group).getByRole("radio", { name: /Claude.*Your account/ }));
    expect(screen.queryByRole("group", { name: "Filter by company" })).toBeNull();
    fireEvent.click(within(group).getByRole("radio", { name: /OpenRouter/ }));
    const companies = screen.getByRole("group", { name: "Filter by company" });
    fireEvent.click(within(companies).getByRole("button", { name: /OpenAI/ }));
    expect(screen.getAllByRole("option")).toHaveLength(1);
    expect(screen.getAllByRole("option")[0]).toHaveTextContent("GPT X");
  });

  it("shows only handpicked models under All but every model once a provider is chosen", () => {
    useLLM.setState({ models: {
      openrouter: { models: [model("anthropic/claude-opus-5-5", { name: "Claude Opus 5.5" }), model("acme/obscure-7b", { name: "Obscure 7B" }), model("acme/other-3b", { name: "Other 3B" })], loading: false, error: null },
      claude: { models: [model("sonnet", { name: "Claude Sonnet" })], loading: false, error: null },
    } });
    render(<Harness selected="claude/sonnet" onPick={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Claude Sonnet/ }));
    expect(screen.queryByText("Obscure 7B")).toBeNull();
    fireEvent.click(within(screen.getByRole("radiogroup", { name: "Provider" })).getByRole("radio", { name: /OpenRouter/ }));
    expect(screen.getAllByRole("option")).toHaveLength(3);
    expect(screen.getByText("Obscure 7B")).toBeInTheDocument();
    // Searching under All reaches every model too.
    fireEvent.click(within(screen.getByRole("radiogroup", { name: "Provider" })).getByRole("radio", { name: /All/ }));
    fireEvent.change(screen.getByRole("combobox", { name: "Search models" }), { target: { value: "other" } });
    expect(screen.getByText("Other 3B")).toBeInTheDocument();
  });

  it("marks the keyboard position only: no row looks selected until the arrow keys are used", () => {
    render(<Harness selected="openrouter/acme/alpha" onPick={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    const ring = () => document.querySelectorAll("[role=option].ring-1").length;
    expect(ring()).toBe(0);
    fireEvent.keyDown(screen.getByRole("combobox", { name: "Search models" }), { key: "ArrowDown" });
    expect(ring()).toBe(1);
  });

  it("never lists a Use default entry, and shows the model a bot without one really runs on", () => {
    useLLM.setState({ defaults: { chatModel: "claude/sonnet", memoryModel: "" } });
    render(<Harness selected="" onPick={() => {}} />);
    expect(screen.getByRole("button", { name: /Claude Sonnet/ })).toHaveTextContent("via Claude account");
    fireEvent.click(screen.getByRole("button", { name: /Claude Sonnet/ }));
    expect(screen.queryByText(/default/i)).toBeNull();
  });

  it("asks for a model when there is neither a choice nor a default", () => {
    render(<Harness selected="" onPick={() => {}} />);
    expect(screen.getByRole("button", { name: /Choose a model/ })).toBeInTheDocument();
  });

  it("has no chooser when only one provider is connected", () => {
    useLLM.setState({ providers: [provider()] });
    render(<Harness selected="openrouter/acme/alpha" onPick={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /Alpha/ }));
    expect(screen.queryByRole("radiogroup", { name: "Provider" })).toBeNull();
  });
});
