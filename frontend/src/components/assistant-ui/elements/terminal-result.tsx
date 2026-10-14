/*
 * Orole's own shell command card. Its layout follows the public demo of the AI SDK Agents "Terminal result" component
 * (https://www.aisdkagents.com/ai-components/); none of that (paid) source was copied.
 */
import { CheckIcon, ChevronRightIcon, CopyIcon, LoaderCircleIcon, ShieldAlertIcon } from "lucide-react";
import { useId, useState, type ComponentProps } from "react";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";
import type { ToolCallState } from "./tool-call-card";
import { Elapsed } from "./working-status";

const ESC = String.fromCharCode(27), BEL = String.fromCharCode(7);
/** CSI sequences (colours, cursor moves) and OSC sequences (titles, links). The feed has no ANSI renderer, so they are dropped. */
const ANSI = new RegExp(`${ESC}\\[[0-?]*[ -/]*[@-~]|${ESC}\\][^${BEL}${ESC}]*(?:${BEL}|${ESC}\\\\)|${ESC}[@-_]`, "g");

/**
 * One shell command and what it printed. The header shows `$ command`, the outcome (`Exit 1` or `Failed` only when it failed,
 * `Needs approval` while a permission holds it, a spinner while it runs), how long it took and a Copy action for the command.
 * Output sits on a muted panel in monospace with ANSI escapes removed; long output folds to its last `tailLines` lines behind
 * an "N earlier lines" toggle. Failed commands open by default, others stay closed; once the reader toggles, their choice wins.
 */
export function TerminalResult({ command, output, state, exitCode, start, end, tailLines = 12, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  /** The command line as it ran (a shell wrapper already removed). May span several lines. */
  command: string;
  output: string;
  state: ToolCallState;
  /** Reported only by some harnesses; without it a failure reads `Failed`. */
  exitCode?: number;
  /** When it started and finished (Unix ms). */
  start?: number | null;
  end?: number | null;
  tailLines?: number;
}) {
  const [chosen, setChosen] = useState<boolean | null>(null);
  const [unfolded, setUnfolded] = useState(false);
  const [copied, setCopied] = useState(false);
  const panelId = useId();
  const failed = state === "error";
  const open = chosen ?? failed;
  const text = output.replace(ANSI, "").replace(/\s+$/, "");
  const lines = text ? text.split(/\r?\n/) : [];
  // Folding one or two lines saves nothing.
  const hidden = lines.length > tailLines + 2 ? lines.length - tailLines : 0;
  const shown = hidden && !unfolded ? lines.slice(hidden) : lines;
  const firstLine = command.split(/\r?\n/).map((line) => line.trim()).find(Boolean) ?? "";
  const multiline = command.trim() !== firstLine;
  const outcome = failed ? (exitCode !== undefined ? `Exit ${exitCode}` : "Failed") : state === "approval" ? "Needs approval" : state === "unknown" ? "No result" : null;
  const copy = async () => {
    try { await navigator.clipboard.writeText(command); setCopied(true); setTimeout(() => setCopied(false), 1500); }
    catch { setCopied(false); }
  };
  // A plain disclosure rather than the Collapsible primitive: a live run mounts one of these per command, and the panel needs
  // none of the primitive's measured height animation.
  return <div data-slot="terminal-result" data-state={state} className={cn(surfaces.paper, "min-w-0 overflow-hidden", className)} {...props}>
    <div className="flex min-w-0 items-center gap-2 py-1 pe-1.5 ps-1">
      <button type="button" aria-expanded={open} aria-controls={open ? panelId : undefined} onClick={() => setChosen(!open)}
        className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1.5 py-1 text-left font-mono text-xs outline-none hover:bg-muted/50 focus-visible:ring-2 focus-visible:ring-ring">
        <ChevronRightIcon aria-hidden className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform duration-150 motion-reduce:transition-none", open && "rotate-90")} />
        <span aria-hidden className="shrink-0 text-muted-foreground">$</span>
        <span className="min-w-0 truncate text-foreground">{firstLine || "command"}</span>
        {outcome && <span className="sr-only">({outcome})</span>}
      </button>
      {state === "running" && <LoaderCircleIcon aria-label="Running" className="size-3.5 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" />}
      {outcome && <span aria-hidden className={cn("flex shrink-0 items-center gap-1 text-xs", failed ? "text-destructive-foreground" : state === "approval" ? "text-warning-foreground" : "text-muted-foreground")}>
        {state === "approval" && <ShieldAlertIcon className="size-3.5" />}{outcome}
      </span>}
      <Elapsed start={start} end={end} live={state === "running"} className="text-xs text-muted-foreground" />
      <button type="button" onClick={() => void copy()} aria-label={copied ? "Command copied" : "Copy command"}
        className="inline-flex shrink-0 items-center gap-1 rounded-md px-1.5 py-1 text-xs text-muted-foreground outline-none hover:bg-muted/60 hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
        {copied ? <CheckIcon aria-hidden className="size-3.5" /> : <CopyIcon aria-hidden className="size-3.5" />}{copied ? "Copied" : "Copy"}
      </button>
    </div>
    {open && <div id={panelId} data-slot="terminal-output" className="mx-1.5 mb-1.5 min-w-0 overflow-hidden rounded-lg bg-muted/60 font-mono text-xs leading-relaxed">
      {multiline && <pre className="selectable m-0 whitespace-pre-wrap break-words border-b border-border/60 px-3 py-2 text-muted-foreground">{command.trim()}</pre>}
      {hidden > 0 && <button type="button" aria-expanded={unfolded} onClick={() => setUnfolded((value) => !value)}
        className="block w-full px-3 pt-1.5 text-center text-[11px] text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">
        {unfolded ? "Hide earlier lines" : `${hidden} earlier ${hidden === 1 ? "line" : "lines"}`}
      </button>}
      {lines.length > 0
        ? <pre tabIndex={0} aria-label="Command output" className="selectable m-0 max-h-96 overflow-auto whitespace-pre-wrap break-words px-3 py-2 text-foreground/90 outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring">{shown.join("\n")}</pre>
        : <p className="m-0 px-3 py-2 text-muted-foreground">{state === "running" ? "Running…" : state === "approval" ? "Waiting for your approval." : "No output was recorded."}</p>}
    </div>}
  </div>;
}
