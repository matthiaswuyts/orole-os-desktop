import { Terminal } from "lucide-react";
import claude from "@lobehub/icons-static-svg/icons/claude.svg?raw";
import openai from "@lobehub/icons-static-svg/icons/openai.svg?raw";
import gemini from "@lobehub/icons-static-svg/icons/gemini.svg?raw";
import grok from "@lobehub/icons-static-svg/icons/grok.svg?raw";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg?raw";
import omp from "@/assets/oh-my-pi.svg?raw";

const marks: Record<string, string> = { claude, codex: openai, gemini, grok, opencode, omp };

/** Whether a harness has a bundled mark, for callers that would rather show nothing than the generic terminal glyph. */
export const hasProviderLogo = (kind: string) => kind in marks;

/** Vetted bundled SVGs only; never render server-supplied markup here. */
export function ProviderLogo({ kind }: { kind: string }) {
  const mark = marks[kind];
  return mark ? <span aria-hidden="true" className="inline-flex size-4 shrink-0 items-center justify-center [&>svg]:size-full" dangerouslySetInnerHTML={{ __html: mark }} /> : <Terminal aria-hidden="true" className="size-4 shrink-0" />;
}
