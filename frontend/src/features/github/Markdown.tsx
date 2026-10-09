import { Fragment, createContext, useContext, useMemo, type ReactNode } from "react";
import { openExternal } from "@/lib/openExternal";
import { parseMarkdown, type Block, type Inline } from "./mdparse";
import { splitReferences } from "./references";

/** Repository that `#123` references resolve against; without one they stay plain text. */
const RepoContext = createContext<string | undefined>(undefined);

function Reference({ value, kind }: { value: string; kind: "mention" | "issue" }) {
  const repo = useContext(RepoContext);
  const href = kind === "mention" ? `https://github.com/${value.slice(1)}` : repo ? `https://github.com/${repo}/issues/${value.slice(1)}` : "";
  if (!href) return <>{value}</>;
  return <a href={href} target="_blank" rel="noopener noreferrer nofollow" onClick={(e) => { e.preventDefault(); void openExternal(href); }} className={kind === "mention" ? "rounded bg-info/10 px-0.5 font-medium text-info-foreground hover:underline" : "text-info-foreground hover:underline"}>{value}</a>;
}

function Text({ v, linked }: { v: string; linked: boolean }) {
  if (linked) return <>{v}</>;
  return splitReferences(v).map((token, i) => token.kind === "text" ? <Fragment key={i}>{token.value}</Fragment> : <Reference key={i} kind={token.kind} value={token.value} />);
}

function Inlines({ c, linked = false }: { c: Inline[]; linked?: boolean }): ReactNode {
  return c.map((n, i) => {
    switch (n.t) {
      case "text": return <Text key={i} v={n.v} linked={linked} />;
      case "br": return <br key={i} />;
      case "code": return <code key={i} className="rounded bg-muted px-1 py-px font-mono text-[0.85em]">{n.v}</code>;
      case "strong": return <strong key={i}><Inlines c={n.c} linked={linked} /></strong>;
      case "em": return <em key={i}><Inlines c={n.c} linked={linked} /></em>;
      case "del": return <del key={i}><Inlines c={n.c} linked={linked} /></del>;
      case "link": return <a key={i} href={n.href} target="_blank" rel="noopener noreferrer nofollow" className="text-info-foreground underline underline-offset-2"><Inlines c={n.c} linked /></a>;
    }
  });
}

function Blocks({ blocks }: { blocks: Block[] }): ReactNode {
  return blocks.map((b, i) => {
    switch (b.t) {
      case "heading": {
        const cls = b.level <= 2 ? "mt-3 mb-1 text-base font-semibold" : "mt-2 mb-1 text-sm font-semibold";
        return <p key={i} role="heading" aria-level={b.level} className={cls}><Inlines c={b.c} /></p>;
      }
      case "p": return <p key={i} className="my-1.5 break-words"><Inlines c={b.c} /></p>;
      case "code": return <pre key={i} className="my-2 overflow-auto rounded-md border bg-muted/50 p-2 font-mono text-xs"><code>{b.v}</code></pre>;
      case "quote": return <blockquote key={i} className="my-2 border-l-2 pl-3 text-muted-foreground"><Blocks blocks={b.c} /></blockquote>;
      case "hr": return <hr key={i} className="my-3" />;
      case "list": {
        const Tag = b.ordered ? "ol" : "ul";
        return (
          <Tag key={i} start={b.ordered ? b.start : undefined} className={`my-1.5 space-y-0.5 pl-5 ${b.ordered ? "list-decimal" : "list-disc"}`}>
            {b.items.map((it, j) => (
              <li key={j} className={it.checked === null ? "" : "list-none -ml-4"}>
                {it.checked !== null && <input type="checkbox" checked={it.checked} disabled readOnly aria-label={it.checked ? "done" : "not done"} className="mr-1.5 align-middle" />}
                <div className="inline-block align-top"><Blocks blocks={it.c} /></div>
              </li>
            ))}
          </Tag>
        );
      }
    }
  });
}

/** Renders untrusted markdown as React elements only: no HTML is ever injected. `repo` ("owner/name") enables `#123` links. */
export function Markdown({ source, className, repo }: { source: string; className?: string; repo?: string }) {
  const tree = useMemo(() => parseMarkdown(source), [source]);
  if (!source.trim()) return <p className={`text-muted-foreground italic ${className ?? ""}`}>No description provided.</p>;
  return <RepoContext.Provider value={repo}><div className={`selectable text-sm leading-relaxed ${className ?? ""}`}><Blocks blocks={tree} /></div></RepoContext.Provider>;
}
