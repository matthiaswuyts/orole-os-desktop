import claude from "@lobehub/icons-static-svg/icons/claude-color.svg?raw";
import grok from "@lobehub/icons-static-svg/icons/grok.svg?raw";
import openai from "@lobehub/icons-static-svg/icons/openai.svg?raw";
import openrouter from "@lobehub/icons-static-svg/icons/openrouter-color.svg?raw";
import { CompanyLogo } from "@/components/CompanyLogo";
import { cn } from "@/lib/utils";

/** Marks of the providers Orole connects to, by provider id. Account CLIs use the product's mark. */
const MARKS: Record<string, string> = { claude, codex: openai, grok, openai, openrouter };

/** Fallbacks by provider name for ids with no mark of their own (declarative manifests, other APIs). */
const COMPANY: Record<string, string> = { anthropic: "Anthropic", google: "Google", mistral: "Mistral", deepseek: "DeepSeek", xai: "xAI" };

/**
 * A provider's logo in a small rounded tile, or its first letter when none is bundled. Decorative: the name is always
 * written next to it. Only vetted bundled SVGs are rendered, never server-supplied markup.
 */
export function ProviderLogo({ id, name, className }: { id: string; name: string; className?: string }) {
  const mark = MARKS[id];
  const company = COMPANY[id.toLowerCase()];
  return (
    <span aria-hidden="true" className={cn("grid size-8 shrink-0 place-items-center rounded-lg bg-muted text-foreground", className)}>
      {mark
        ? <span className="inline-flex size-5 items-center justify-center [&>svg]:size-full" dangerouslySetInnerHTML={{ __html: mark }} />
        : company
          ? <CompanyLogo company={company} className="size-5" />
          : <span className="font-semibold text-sm uppercase text-muted-foreground">{name.charAt(0)}</span>}
    </span>
  );
}
