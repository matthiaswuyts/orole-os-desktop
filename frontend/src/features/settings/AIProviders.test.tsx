import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { Model, ProviderStatus } from "@/mygo";

const bridge = vi.hoisted(() => ({
  providers: vi.fn(), defaults: vi.fn(), models: vi.fn(), connect: vi.fn(), disconnect: vi.fn(), test: vi.fn(),
  setDefaults: vi.fn(), authStart: vi.fn(), authFinish: vi.fn(), openExternal: vi.fn(),
}));
vi.mock("@/lib/backend", () => ({ inDesktop: true }));
vi.mock("@/mygo", () => ({
  LLM: {
    providers: bridge.providers, defaults: bridge.defaults, models: bridge.models, connect: bridge.connect, disconnect: bridge.disconnect,
    test: bridge.test, setDefaults: bridge.setDefaults, openRouterAuthStart: bridge.authStart, openRouterAuthFinish: bridge.authFinish,
  },
  Shell: { openExternal: bridge.openExternal },
  Daemon: { health: vi.fn() },
  events: { llmChanged: { on: () => () => {} }, daemonState: { on: () => () => {} } },
}));
import { AIProviders } from "@/features/settings/AIProviders";
import { useLLM } from "@/stores/llm";
import { useDaemon } from "@/stores/daemon";

const openrouter: ProviderStatus = { id: "openrouter", name: "OpenRouter", kind: "builtin", connected: false };
const openai: ProviderStatus = { id: "openai", name: "OpenAI", kind: "builtin", connected: false };
const caps = { tools: true, vision: false, pdf: false, reasoning: false, hostedTools: [], deepResearch: false, contextWindow: 0 };
const models: Model[] = [
  { id: "anthropic/claude-sonnet", name: "Claude Sonnet", caps: { ...caps, vision: true, pdf: true, contextWindow: 200_000 }, reasoning: [] },
  { id: "openai/gpt-mini", name: "GPT Mini", caps: { ...caps, tools: false }, reasoning: [] },
];
const KEY = "sk-or-v1-abcdefghijklmnop1234";

function seed(providers: ProviderStatus[]) {
  bridge.providers.mockResolvedValue(providers);
  useLLM.setState({ providers, ready: true, defaults: { chatModel: "", memoryModel: "" } });
}

beforeEach(() => {
  for (const fn of Object.values(bridge)) fn.mockReset();
  bridge.providers.mockResolvedValue([openrouter, openai]);
  bridge.defaults.mockResolvedValue({ chatModel: "", memoryModel: "" });
  bridge.models.mockResolvedValue(models);
  useDaemon.setState({ link: "connected" });
  useLLM.setState({ providers: [], defaults: null, models: {}, busy: {}, ready: false, loading: false, error: null });
});
afterEach(cleanup);

const row = (name: string) => screen.getByRole("listitem", { name });

describe("AI providers settings", () => {
  it("shows unconnected providers and where keys live", async () => {
    seed([openrouter, openai]);
    render(<AIProviders />);
    expect(screen.getByText(/Windows Credential Manager/)).toBeInTheDocument();
    expect(screen.getByText(/directly from this computer to the provider; there is no Orole server in between/)).toBeInTheDocument();
    for (const name of ["OpenRouter", "OpenAI"]) {
      expect(within(row(name)).getByText("Not connected")).toBeInTheDocument();
      expect(within(row(name)).getByRole("button", { name: "Connect" })).toBeEnabled();
    }
  });

  it("connects with a pasted key, then closes and leaves no key behind", async () => {
    seed([openrouter, openai]);
    bridge.connect.mockResolvedValue({ ...openai, connected: true, keyHint: "1234", valid: true, account: { label: "Personal", remaining: 4.5, limit: 10 } });
    render(<AIProviders />);
    fireEvent.click(within(row("OpenAI")).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    const input = within(dialog).getByLabelText("API key");
    expect(input).toHaveAttribute("type", "password");
    expect(within(dialog).getByRole("button", { name: "Connect" })).toBeDisabled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Show API key" }));
    expect(input).toHaveAttribute("type", "text");
    fireEvent.change(input, { target: { value: KEY } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Connect" }));
    await waitFor(() => expect(bridge.connect).toHaveBeenCalledWith({ id: "openai", apiKey: KEY }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(screen.queryByDisplayValue(KEY)).toBeNull();
    expect(document.body.textContent).not.toContain(KEY);
    expect(JSON.stringify(useLLM.getState())).not.toContain(KEY);
    expect(within(row("OpenAI")).getByText(/Connected • \*\*\*\*1234/)).toBeInTheDocument();
    expect(within(row("OpenAI")).getByText("Personal · $4.50 credits left of $10.00")).toBeInTheDocument();
  });

  it("announces an invalid key, clears the field and keeps the dialog open", async () => {
    seed([openrouter, openai]);
    bridge.connect.mockRejectedValue(new Error("bad_request: OpenAI rejected this key (invalid API key)"));
    render(<AIProviders />);
    const openaiRow = row("OpenAI");
    fireEvent.click(within(openaiRow).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.change(within(dialog).getByLabelText("API key"), { target: { value: KEY } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Connect" }));
    const alert = await within(dialog).findByRole("alert");
    expect(alert).toHaveTextContent("OpenAI rejected this key (invalid API key)");
    expect(alert).not.toHaveTextContent(KEY);
    expect(within(dialog).getByLabelText("API key")).toHaveValue("");
    expect(within(openaiRow).getByText("Not connected")).toBeInTheDocument();
  });

  it("links to the provider's key page through the external-link helper", async () => {
    seed([openrouter, openai]);
    render(<AIProviders />);
    fireEvent.click(within(row("OpenAI")).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: /platform\.openai\.com\/api-keys/ }));
    expect(bridge.openExternal).toHaveBeenCalledWith("https://platform.openai.com/api-keys");
  });

  it("signs in with OpenRouter by pasting the code from the browser", async () => {
    seed([openrouter, openai]);
    bridge.authStart.mockResolvedValue({ url: "https://openrouter.ai/auth?callback_url=x&code_challenge=y" });
    bridge.authFinish.mockResolvedValue({ ...openrouter, connected: true, keyHint: "9f2c", valid: true });
    render(<AIProviders />);
    fireEvent.click(within(row("OpenRouter")).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    expect(within(dialog).queryByLabelText("Authorization code")).toBeNull();
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign in with OpenRouter" }));
    const code = await within(dialog).findByLabelText("Authorization code");
    expect(bridge.openExternal).toHaveBeenCalledWith("https://openrouter.ai/auth?callback_url=x&code_challenge=y");
    expect(within(dialog).getByRole("button", { name: "Finish sign-in" })).toBeDisabled();
    fireEvent.change(code, { target: { value: "  code-123  " } });
    fireEvent.click(within(dialog).getByRole("button", { name: "Finish sign-in" }));
    await waitFor(() => expect(bridge.authFinish).toHaveBeenCalledWith("code-123"));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(within(row("OpenRouter")).getByText(/Connected • \*\*\*\*9f2c/)).toBeInTheDocument();
  });

  it("does not open a sign-in address that is not a web link", async () => {
    seed([openrouter, openai]);
    bridge.authStart.mockResolvedValue({ url: "file:///C:/Windows/System32/calc.exe" });
    render(<AIProviders />);
    fireEvent.click(within(row("OpenRouter")).getByRole("button", { name: "Connect" }));
    const dialog = await screen.findByRole("dialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Sign in with OpenRouter" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent(/not a web link/);
    expect(bridge.openExternal).not.toHaveBeenCalled();
    expect(within(dialog).queryByLabelText("Authorization code")).toBeNull();
  });

  it("tests a connection and shows an invalid key", async () => {
    seed([{ ...openai, connected: true, keyHint: "1234", valid: true }, openrouter]);
    bridge.test.mockResolvedValue({ ...openai, connected: true, keyHint: "1234", valid: false, error: "401 incorrect API key" });
    render(<AIProviders />);
    fireEvent.click(within(row("OpenAI")).getByRole("button", { name: "Test" }));
    await waitFor(() => expect(bridge.test).toHaveBeenCalledWith("openai"));
    expect(await within(row("OpenAI")).findByText("Invalid key")).toBeInTheDocument();
    expect(within(row("OpenAI")).getByRole("alert")).toHaveTextContent("401 incorrect API key");
    expect(within(row("OpenAI")).getByRole("button", { name: "Update key" })).toBeEnabled();
  });

  it("asks before disconnecting and only then calls the daemon", async () => {
    seed([{ ...openai, connected: true, keyHint: "1234", valid: true }, openrouter]);
    bridge.disconnect.mockResolvedValue(undefined);
    render(<AIProviders />);
    fireEvent.click(within(row("OpenAI")).getByRole("button", { name: "Disconnect" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(within(dialog).getByText("Disconnect OpenAI?")).toBeInTheDocument();
    expect(within(dialog).getByText(/Windows Credential Manager/)).toBeInTheDocument();
    expect(bridge.disconnect).not.toHaveBeenCalled();
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    expect(bridge.disconnect).not.toHaveBeenCalled();
    fireEvent.click(within(row("OpenAI")).getByRole("button", { name: "Disconnect" }));
    fireEvent.click(await within(await screen.findByRole("alertdialog")).findByRole("button", { name: "Disconnect" }));
    await waitFor(() => expect(bridge.disconnect).toHaveBeenCalledWith("openai"));
    await waitFor(() => expect(within(row("OpenAI")).getByText("Not connected")).toBeInTheDocument());
  });

  it("shows the daemon error with a retry when providers cannot load", async () => {
    bridge.providers.mockRejectedValueOnce(new Error("daemon unreachable"));
    render(<AIProviders />);
    expect(await screen.findByText("daemon unreachable")).toBeInTheDocument();
    bridge.providers.mockResolvedValue([openrouter]);
    fireEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("listitem", { name: "OpenRouter" })).toBeInTheDocument();
  });

  it("lists manifest providers with the normal Connect flow", async () => {
    seed([{ id: "custom", name: "Custom", kind: "manifest", connected: false }]);
    render(<AIProviders />);
    expect(within(row("Custom")).getByText("Manifest")).toBeInTheDocument();
    expect(within(row("Custom")).getByRole("button", { name: "Connect" })).toBeEnabled();
  });
});
