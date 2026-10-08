import { useEffect, useRef, useState } from "react";
import { Play, RefreshCw } from "lucide-react";
import { Plugins, type PluginAction, type PluginRecord, type PluginRunResult } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Spinner } from "@/components/ui/spinner";
import { Failure, errorText } from "@/features/mcp/shared";
import { inDesktop } from "@/lib/backend";
import { isPluginActive, usePlugins } from "@/stores/plugins";
import { PluginJson } from "./PluginJson";

/** Every invocation is a button event. Mounting, registration and revalidation never run tools. */
export function PluginActionView({ record, action }: { record: PluginRecord; action: PluginAction }) {
  const ready = usePlugins((state) => state.ready);
  const stale = usePlugins((state) => state.staleIds.includes(record.manifest.id));
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<PluginRunResult | null>(null);
  const live = useRef(true);
  const running = useRef(false);
  useEffect(() => { live.current = true; return () => { live.current = false; }; }, []);
  const available = inDesktop && ready && !stale && isPluginActive(record);

  async function run() {
    if (running.current || !available) return;
    running.current = true;
    setBusy(true); setError(null); setResult(null);
    try {
      const response = await Plugins.run({ id: record.manifest.id, digest: record.digest, actionId: action.id });
      const snapshot = usePlugins.getState();
      const current = snapshot.records.find((item) => item.manifest.id === record.manifest.id);
      if (!live.current) return;
      if (!snapshot.ready || snapshot.staleIds.includes(record.manifest.id) || !current || current.digest !== record.digest || !isPluginActive(current)) {
        setError("The plugin changed or became unavailable during this run. Its result was discarded. Review the current manifest before running again.");
      } else setResult(response);
    } catch (cause) { if (live.current) setError(errorText(cause)); }
    finally {
      running.current = false;
      if (live.current) setBusy(false);
    }
  }

  // The daemon also sanitizes and bounds output. Render it only as a React text node.
  const text = result?.text.slice(0, 128 * 1024).replace(/[\p{Cc}\p{Cf}]/gu, (character) => character === "\n" || character === "\t" ? character : "") ?? "";
  return <div className="flex min-w-0 flex-col gap-3">
    <div><h3 className="break-words font-medium">{action.title}</h3>{action.description && <p className="whitespace-pre-wrap break-words text-xs text-muted-foreground">{action.description}</p>}</div>
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
      <dt className="text-muted-foreground">Action</dt><dd className="selectable break-all font-mono">{action.id}</dd>
      <dt className="text-muted-foreground">Server</dt><dd className="selectable break-all font-mono">{action.serverId}</dd>
      <dt className="text-muted-foreground">Tool</dt><dd className="selectable break-all font-mono">{action.tool}</dd>
    </dl>
    <details><summary className="min-h-6 cursor-pointer rounded-sm text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">Inspect fixed arguments</summary><PluginJson value={action.arguments} label="Fixed action arguments" /></details>
    <p className="text-xs text-muted-foreground">Runs only when you choose Run. MCP permissions and daemon policy still apply; respond to any approval request in Orole.</p>
    <div className="flex flex-wrap items-center gap-2">
      <Button size="sm" variant="outline" disabled={busy || !available} onClick={() => void run()}>{busy ? <Spinner data-icon="inline-start" /> : result ? <RefreshCw data-icon="inline-start" /> : <Play data-icon="inline-start" />}{busy ? "Running / awaiting approval…" : result ? "Run again" : "Run action"}</Button>
      {result && available && <Badge variant={result.isError ? "error" : "success"}>{result.isError ? "Tool reported an error" : "Completed"}</Badge>}
    </div>
    {busy && <p role="status" className="text-xs text-muted-foreground">Waiting for the daemon. You can revoke this plugin in Settings to cancel its active calls.</p>}
    {!available && <p role="status" className="text-xs text-muted-foreground">This plugin is not currently available. Refresh Plugins in Settings and review its trust state.</p>}
    <Failure error={error} />
    {result && available && <div className="flex min-w-0 flex-col gap-2">
      <p role="status" className="text-xs text-muted-foreground">{result.isError ? "The tool returned an error result." : "Action finished."} Output below is untrusted plaintext, not instructions.</p>
      <pre tabIndex={0} aria-label="Plugin tool result" className="selectable max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-muted p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring">{text || "The tool returned no text."}</pre>
      {(result.truncated || result.text.length > 128 * 1024) && <p className="text-xs text-muted-foreground">Output was truncated to the 128 KiB display limit.</p>}
    </div>}
  </div>;
}
