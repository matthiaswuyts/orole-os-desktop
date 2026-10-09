import { useId, useState, type FormEvent } from "react";
import { Eye, EyeOff, ExternalLink } from "lucide-react";
import type { ProviderStatus } from "@/mygo";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { useLLM } from "@/stores/llm";

/** Where the user creates an API key for a built-in provider. */
const KEY_PAGES: Record<string, string> = {
  openrouter: "https://openrouter.ai/keys",
  openai: "https://platform.openai.com/api-keys",
};

/**
 * Collects an API key (or, for OpenRouter, a sign-in code) and hands it to the daemon.
 * The secret lives only in this component's state: the parent mounts it per open and unmounts it
 * on close, and every submit clears the field whatever the outcome.
 */
export function ConnectProviderDialog({ provider, onClose }: { provider: ProviderStatus; onClose: () => void }) {
  const formId = useId();
  const pending = useLLM((state) => state.busy[provider.id] === "connect");
  const [apiKey, setApiKey] = useState("");
  const [reveal, setReveal] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const keyPage = KEY_PAGES[provider.id];
  const keyPageLabel = keyPage ? new URL(keyPage).host + new URL(keyPage).pathname : "";
  const [authUrl, setAuthUrl] = useState<string | null>(null);
  const [code, setCode] = useState("");
  const oauth = provider.id === "openrouter";

  async function submitKey(event: FormEvent) {
    event.preventDefault();
    const key = apiKey.trim();
    if (!key || pending) return;
    setError(null);
    const result = await useLLM.getState().connect(provider.id, key);
    setApiKey("");
    setReveal(false);
    if (result.ok) onClose();
    else setError(result.error);
  }

  async function startSignIn() {
    setError(null);
    setStarting(true);
    const result = await useLLM.getState().openRouterAuthStart();
    setStarting(false);
    if (!result.ok) { setError(result.error); return; }
    if (!isWebURL(result.value)) { setError("OpenRouter returned an address that is not a web link, so nothing was opened."); return; }
    setAuthUrl(result.value);
    await openExternal(result.value);
  }

  async function finishSignIn(event: FormEvent) {
    event.preventDefault();
    const value = code.trim();
    if (!value || pending) return;
    setError(null);
    const result = await useLLM.getState().openRouterAuthFinish(value);
    setCode("");
    if (result.ok) onClose();
    else setError(result.error);
  }

  return (
    <Dialog open onOpenChange={(open) => { if (!open) onClose(); }}>
      <DialogPopup>
        <DialogHeader>
          <DialogTitle>{provider.connected ? `Update ${provider.name} key` : `Connect ${provider.name}`}</DialogTitle>
          <DialogDescription>
            The key is checked with {provider.name}, then stored in Windows Credential Manager. Requests go directly from this computer to {provider.name}; no Orole server is in between.
          </DialogDescription>
        </DialogHeader>
        <DialogPanel className="flex flex-col gap-4">
          {oauth && (
            <section aria-label="Sign in with OpenRouter" className="flex flex-col gap-2">
              <p className="text-muted-foreground">Approve access in your browser, then paste the code OpenRouter shows you. OpenRouter creates a key for Orole OS; you never copy it yourself.</p>
              {authUrl === null ? (
                <Button variant="outline" className="self-start" loading={starting} disabled={pending} onClick={() => void startSignIn()}>
                  <ExternalLink data-icon="inline-start" />Sign in with OpenRouter
                </Button>
              ) : (
                <form onSubmit={(event) => void finishSignIn(event)} className="flex flex-col gap-2">
                  <Field>
                    <FieldLabel>Authorization code</FieldLabel>
                    <Input autoFocus value={code} onChange={(event) => setCode(event.target.value)} autoComplete="off" spellCheck={false} className="font-mono" placeholder="Paste the code from OpenRouter" />
                    <FieldDescription>
                      The code is single-use. <button type="button" className="text-info-foreground underline underline-offset-2" onClick={() => void openExternal(authUrl)}>Open the OpenRouter page again</button>
                    </FieldDescription>
                  </Field>
                  <Button type="submit" className="self-start" loading={pending} disabled={!code.trim()}>Finish sign-in</Button>
                </form>
              )}
            </section>
          )}
          {oauth && <p className="text-center text-xs text-muted-foreground">or paste an API key</p>}
          <form id={formId} onSubmit={(event) => void submitKey(event)} className="flex flex-col gap-2">
            <Field>
              <FieldLabel>API key</FieldLabel>
              <div className="flex w-full gap-1.5">
                <Input
                  autoFocus={!oauth}
                  type={reveal ? "text" : "password"}
                  value={apiKey}
                  onChange={(event) => setApiKey(event.target.value)}
                  autoComplete="off"
                  spellCheck={false}
                  className="font-mono"
                  placeholder={provider.id === "openai" ? "sk-…" : "Paste your API key"}
                />
                <Button type="button" variant="outline" size="icon" aria-label={reveal ? "Hide API key" : "Show API key"} aria-pressed={reveal} onClick={() => setReveal(!reveal)}>
                  {reveal ? <EyeOff /> : <Eye />}
                </Button>
              </div>
              {keyPage && (
                <FieldDescription>
                  Create a key on the {provider.name} site:{" "}
                  <button type="button" className="inline-flex items-center gap-0.5 text-info-foreground underline underline-offset-2" onClick={() => void openExternal(keyPage)}>
                    {keyPageLabel}<ExternalLink aria-hidden className="size-3" />
                  </button>
                </FieldDescription>
              )}
            </Field>
          </form>
          {error && (
            <Alert variant="error">
              <AlertTitle>Could not connect {provider.name}</AlertTitle>
              <AlertDescription className="selectable break-words">{error}</AlertDescription>
            </Alert>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>Cancel</Button>
          <Button type="submit" form={formId} loading={pending} disabled={!apiKey.trim()}>{provider.connected ? "Replace key" : "Connect"}</Button>
        </DialogFooter>
      </DialogPopup>
    </Dialog>
  );
}
