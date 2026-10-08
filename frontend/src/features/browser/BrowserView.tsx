import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { ExternalLink, Globe, Monitor, RefreshCw, Smartphone, Tablet } from "lucide-react";
import { Shell } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

type Viewport = "mobile" | "tablet" | "desktop";
type LoadState = "idle" | "loading" | "loaded" | "slow" | "error";
const widths: Record<Viewport, number> = { mobile: 390, tablet: 768, desktop: 1280 };

/** Only explicit web URLs enter the iframe or the desktop shell. */
export function parsePreviewURL(raw: string): string {
  let url: URL;
  try { url = new URL(raw.trim()); }
  catch { throw new Error("Enter a complete http:// or https:// URL."); }
  if (url.protocol !== "http:" && url.protocol !== "https:") throw new Error("Only http:// and https:// URLs can be previewed.");
  return url.href;
}

export function BrowserView() {
  const [input, setInput] = useState("");
  const [navigation, setNavigation] = useState({ url: "", id: 0 });
  const [viewport, setViewport] = useState<Viewport>("desktop");
  const [status, setStatus] = useState<LoadState>("idle");
  const [urlError, setUrlError] = useState<string | null>(null);
  const [externalError, setExternalError] = useState<string | null>(null);
  const [opening, setOpening] = useState(false);
  const [online, setOnline] = useState(() => navigator.onLine);

  useEffect(() => {
    const update = () => setOnline(navigator.onLine);
    window.addEventListener("online", update);
    window.addEventListener("offline", update);
    return () => { window.removeEventListener("online", update); window.removeEventListener("offline", update); };
  }, []);
  useEffect(() => {
    if (!navigation.url) return;
    const timeout = window.setTimeout(() => setStatus((value) => value === "loading" ? "slow" : value), 15000);
    return () => window.clearTimeout(timeout);
  }, [navigation]);

  const navigate = (event?: FormEvent) => {
    event?.preventDefault();
    try {
      const url = parsePreviewURL(input);
      setInput(url);
      setUrlError(null);
      setExternalError(null);
      setStatus("loading");
      setNavigation((current) => ({ url, id: current.id + 1 }));
    } catch (error) { setUrlError(error instanceof Error ? error.message : String(error)); }
  };
  const reload = () => {
    setStatus("loading");
    setNavigation((current) => ({ ...current, id: current.id + 1 }));
  };
  const openExternal = async () => {
    setOpening(true);
    setExternalError(null);
    try { await Shell.openExternal(parsePreviewURL(navigation.url)); }
    catch (error) { setExternalError(error instanceof Error ? error.message : String(error)); }
    finally { setOpening(false); }
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex shrink-0 flex-col gap-2 border-b bg-background p-3">
        <form onSubmit={navigate} className="flex items-end gap-2">
          <Field className="min-w-0 flex-1" invalid={Boolean(urlError)}>
            <FieldLabel htmlFor="browser-preview-url" className="sr-only">Preview URL</FieldLabel>
            <Input id="browser-preview-url" type="text" inputMode="url" autoComplete="off" spellCheck={false} placeholder="https://example.com or http://localhost:3000" value={input} onChange={(event) => setInput(event.target.value)} aria-invalid={Boolean(urlError)} aria-describedby={urlError ? "browser-preview-url-error" : undefined} />
          </Field>
          <Button type="submit" size="sm" disabled={!input.trim()}><Globe data-icon="inline-start" />Go</Button>
          <Button size="icon-sm" variant="outline" disabled={!navigation.url} aria-label="Reload preview" onClick={reload}><RefreshCw /></Button>
        </form>
        {urlError && <p id="browser-preview-url-error" role="alert" className="text-destructive-foreground">{urlError}</p>}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <ToggleGroup variant="outline" size="sm" value={[viewport]} aria-label="Preview viewport" onValueChange={(values) => { const value = values[0]; if (value === "mobile" || value === "tablet" || value === "desktop") setViewport(value); }}>
            <ToggleGroupItem value="mobile"><Smartphone />Mobile</ToggleGroupItem>
            <ToggleGroupItem value="tablet"><Tablet />Tablet</ToggleGroupItem>
            <ToggleGroupItem value="desktop"><Monitor />Desktop</ToggleGroupItem>
          </ToggleGroup>
          <div className="flex items-center gap-2">
            <span className="text-xs tabular-nums text-muted-foreground">{widths[viewport]} px</span>
            <Tooltip><TooltipTrigger render={<span tabIndex={!navigation.url || !inDesktop || opening ? 0 : -1} className="inline-flex" aria-label="Open in browser" />}><Button size="sm" variant="outline" disabled={!navigation.url || !inDesktop || opening} onClick={() => void openExternal()}><ExternalLink data-icon="inline-start" />Open in browser</Button></TooltipTrigger><TooltipPopup>{!inDesktop ? "Opening the default browser requires the desktop app" : !navigation.url ? "Enter a URL first" : opening ? "Opening your default browser…" : "Open the entered URL in your default browser"}</TooltipPopup></Tooltip>
          </div>
        </div>
        <p className="text-xs text-muted-foreground">Sandboxed preview. Some sites block embedding or require permissions unavailable here. If a page is blank, open it in your browser. The address bar shows the requested URL, not navigation inside the page.</p>
        <p className="text-xs text-muted-foreground">Console and network capture are not implemented. App DevTools are development-only; use your browser’s tools to inspect a site.{!inDesktop && " Opening the default browser requires the Orole desktop app."}</p>
      </header>
      {(!online || externalError || status === "error" || status === "slow") && <div className="flex shrink-0 flex-col gap-2 p-3">
        {!online && <Alert variant="warning"><AlertTitle>Browser reports no network connection</AlertTitle><AlertDescription>Internet pages may not load. Local development servers can still be reachable.</AlertDescription></Alert>}
        {externalError && <Alert variant="error"><AlertTitle>Could not open your browser</AlertTitle><AlertDescription>{externalError}</AlertDescription></Alert>}
        {(status === "error" || status === "slow") && <Alert variant="warning"><AlertTitle>{status === "error" ? "Preview could not load" : "Still waiting for the page"}</AlertTitle><AlertDescription>{status === "error" ? "Reload the preview or open the URL in your browser." : "No load signal arrived. The site may be slow or may block embedding. Reload or open it in your browser; this preview cannot inspect cross-origin failures."}</AlertDescription></Alert>}
      </div>}
      <div role="status" className="flex min-h-6 shrink-0 items-center gap-2 px-3 text-xs text-muted-foreground">{status === "loading" && <><Spinner />Loading preview…</>}{status === "loaded" && "Page load signal received. Embedded content cannot be inspected by the app."}</div>
      {!navigation.url ? <Empty><EmptyHeader><EmptyTitle>Preview a web page</EmptyTitle><EmptyDescription>Enter an HTTP or HTTPS URL above. Switch viewport widths to preview a site or local development server.</EmptyDescription></EmptyHeader></Empty> : <div className="min-h-0 flex-1 overflow-auto bg-muted p-3">
        <iframe key={navigation.id} title={`Web preview: ${navigation.url}`} src={navigation.url} sandbox="allow-scripts allow-forms" referrerPolicy="no-referrer" className="mx-auto block min-h-[480px] border bg-background" style={{ width: widths[viewport], height: "100%" }} onLoad={() => setStatus("loaded")} onError={() => setStatus("error")} />
      </div>}
    </div>
  );
}
