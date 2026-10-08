import { Fragment, useMemo, type ReactNode } from "react";
import { parseMarkdown, type Block, type Inline } from "./mdparse";

function Inlines({ c }: { c: Inline[] }): ReactNode {
  return c.map((n, i) => {
    switch (n.t) {
      case "text": return <Fragment key={i}>{n.v}</Fragment>;
      case "br": return <br key={i} />;
      case "code": return <code key={i} className="rounded bg-muted px-1 py-px font-mono text-[0.85em]">{n.v}</code>;
      case "strong": return <strong key={i}><Inlines c={n.c} /></strong>;
      case "em": return <em key={i}><Inlines c={n.c} /></em>;
      case "del": return <del key={i}><Inlines c={n.c} /></del>;
      case "link": return <a key={i} href={n.href} target="_blank" rel="noopener noreferrer nofollow" className="text-info-foreground underline underline-offset-2"><Inlines c={n.c} /></a>;
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

/** Renders untrusted markdown as React elements only: no HTML is ever injected. */
export function Markdown({ source, className }: { source: string; className?: string }) {
  const tree = useMemo(() => parseMarkdown(source), [source]);
  if (!source.trim()) return <p className={`text-muted-foreground italic ${className ?? ""}`}>No description provided.</p>;
  return <div className={`selectable text-sm leading-relaxed ${className ?? ""}`}><Blocks blocks={tree} /></div>;
}
