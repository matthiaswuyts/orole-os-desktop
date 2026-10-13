import { isValidElement, memo, type ComponentProps } from "react";
import ReactMarkdown, { type Components } from "react-markdown";
import remarkGfm from "remark-gfm";
import { isWebURL, openExternal } from "@/lib/openExternal";
import { cn } from "@/lib/utils";
import { SyntaxHighlighter } from "@/components/SyntaxHighlighter";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

const remarkPlugins = [remarkGfm];

// Untrusted agent text: no rehype-raw, so inline HTML is shown as text and never parsed; images are
// not fetched from model-chosen URLs (generated images render through the artifact path instead);
// links open only http(s), outside the app.
const components: Components = {
  a({ href, children }) {
    if (!href || !isWebURL(href)) return <span className="underline decoration-dotted">{children}</span>;
    return <Tooltip><TooltipTrigger render={<a href={href} target="_blank" rel="noopener noreferrer" className="text-info-foreground underline underline-offset-2 hover:opacity-80" onClick={(e) => { e.preventDefault(); void openExternal(href); }} />}>{children}</TooltipTrigger><TooltipPopup className="max-w-sm break-all">{href}</TooltipPopup></Tooltip>;
  },
  img({ alt }) { return <span className="text-muted-foreground">[image{alt ? `: ${alt}` : ""}]</span>; },
  pre({ children }) {
    if (isValidElement<{ className?: string; children?: string }>(children)) {
      const language = /language-([^\s]+)/.exec(children.props.className ?? "")?.[1] ?? "";
      return <SyntaxHighlighter language={language} code={String(children.props.children ?? "").replace(/\n$/, "")} />;
    }
    return <pre className="selectable my-2 overflow-auto rounded-md border bg-muted/50 p-2 font-mono text-xs leading-relaxed">{children}</pre>;
  },
  code({ className, children }) {
    const block = /language-/.test(className ?? "");
    return <code className={cn(block ? "font-mono" : "rounded bg-muted px-1 py-0.5 font-mono text-[0.85em]", className)}>{children}</code>;
  },
  table({ children }) { return <div className="my-2 overflow-x-auto"><table className="w-full border-collapse text-xs">{children}</table></div>; },
  th({ children }) { return <th className="border bg-muted/50 px-2 py-1 text-left font-medium">{children}</th>; },
  td({ children }) { return <td className="border px-2 py-1 align-top">{children}</td>; },
  ul({ children, className }) { return <ul className={cn("my-1.5 list-disc pl-5", className?.includes("contains-task-list") && "list-none pl-1")}>{children}</ul>; },
  ol({ children }) { return <ol className="my-1.5 list-decimal pl-5">{children}</ol>; },
  p({ children }) { return <p className="my-1.5 first:mt-0 last:mb-0">{children}</p>; },
  h1({ children }) { return <h3 className="mt-3 mb-1.5 text-base font-semibold">{children}</h3>; },
  h2({ children }) { return <h3 className="mt-3 mb-1.5 text-base font-semibold">{children}</h3>; },
  h3({ children }) { return <h4 className="mt-2.5 mb-1 text-sm font-semibold">{children}</h4>; },
  h4({ children }) { return <h5 className="mt-2 mb-1 text-sm font-medium">{children}</h5>; },
  h5({ children }) { return <h6 className="mt-2 mb-1 text-sm font-medium">{children}</h6>; },
  h6({ children }) { return <h6 className="mt-2 mb-1 text-sm font-medium text-muted-foreground">{children}</h6>; },
  blockquote({ children }) { return <blockquote className="my-2 border-l-2 pl-3 text-muted-foreground">{children}</blockquote>; },
  hr() { return <hr className="my-3" />; },
};

/** Markdown (GitHub-flavoured) for untrusted agent output. Memoized: feeds re-render on every event. */
export const Markdown = memo(function Markdown({ children, className }: { children: string } & Pick<ComponentProps<"div">, "className">) {
  return <div className={cn("selectable min-w-0 break-words text-sm leading-relaxed", className)}><ReactMarkdown remarkPlugins={remarkPlugins} components={components}>{children}</ReactMarkdown></div>;
});
