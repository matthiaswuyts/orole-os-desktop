import { memo, useEffect, useState, type ReactNode } from "react";
import { EditorState } from "@codemirror/state";
import { ensureSyntaxTree } from "@codemirror/language";
import { highlightTree, tagHighlighter, tags } from "@lezer/highlight";
import { Check, Copy } from "lucide-react";
import { languageFor } from "@/features/editor/languages";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

const aliases: Record<string, string> = { javascript: "js", typescript: "ts", python: "py", rust: "rs", markdown: "md" };
const colors = tagHighlighter([
  { tag: [tags.keyword, tags.modifier], class: "text-info-foreground" },
  { tag: [tags.string, tags.regexp], class: "text-success" },
  { tag: [tags.number, tags.bool, tags.null], class: "text-warning-foreground" },
  { tag: tags.comment, class: "text-muted-foreground italic" },
  { tag: [tags.typeName, tags.className], class: "text-info-foreground" },
]);

/** Parser output becomes React text nodes, never HTML. Unknown languages remain inert text. */
export async function highlightCode(code: string, language: string): Promise<ReactNode[]> {
  if (!language || code.length > 100_000) return [code];
  const extension = await languageFor(`code.${aliases[language.toLowerCase()] ?? language.toLowerCase()}`);
  const state = EditorState.create({ doc: code, extensions: [extension] });
  const tree = ensureSyntaxTree(state, code.length, 50);
  if (!tree || tree.length < code.length) return [code];
  const result: ReactNode[] = [];
  let end = 0;
  highlightTree(tree, colors, (from, to, className) => {
    if (from > end) result.push(code.slice(end, from));
    result.push(<span key={from} className={className}>{code.slice(from, to)}</span>);
    end = to;
  });
  if (end < code.length) result.push(code.slice(end));
  return result;
}

export const SyntaxHighlighter = memo(function SyntaxHighlighter({ code, language = "", className }: { code: string; language?: string; className?: string }) {
  const [highlighted, setHighlighted] = useState<{ code: string; language: string; nodes: ReactNode[] } | null>(null);
  const [copy, setCopy] = useState<{ code: string; state: "copied" | "failed" } | null>(null);
  useEffect(() => {
    let active = true;
    void highlightCode(code, language).then((nodes) => { if (active) setHighlighted({ code, language, nodes }); }, () => { /* Plain text remains usable if a grammar cannot load. */ });
    return () => { active = false; };
  }, [code, language]);
  const copied = copy?.code === code && copy.state === "copied";
  return <div className={cn("my-2 min-w-0 overflow-hidden rounded-md border bg-muted/30", className)} data-slot="syntax-highlighter">
    <div className="flex items-center justify-between gap-2 border-b px-2 py-1 text-xs text-muted-foreground">
      <span className="truncate">{language || "Plain text"}</span>
      <Button size="xs" variant="ghost" aria-label="Copy code" onClick={async () => {
        try { await navigator.clipboard.writeText(code); setCopy({ code, state: "copied" }); }
        catch { setCopy({ code, state: "failed" }); }
      }}>{copied ? <Check aria-hidden /> : <Copy aria-hidden />}{copied ? "Copied" : "Copy"}</Button>
    </div>
    {copy?.code === code && copy.state === "failed" && <p role="alert" className="px-2 py-1 text-xs text-destructive-foreground">Could not copy. Select the code to copy it manually.</p>}
    <pre tabIndex={0} aria-label={language ? `${language} code` : "Code"} className="selectable max-h-96 overflow-auto p-3 font-mono text-xs leading-relaxed outline-none focus-visible:ring-2 focus-visible:ring-ring"><code>{highlighted?.code === code && highlighted.language === language ? highlighted.nodes : code}</code></pre>
  </div>;
});
