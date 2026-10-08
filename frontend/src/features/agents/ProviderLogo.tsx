import { Terminal } from "lucide-react";
import claude from "@lobehub/icons-static-svg/icons/claude.svg?raw";
import openai from "@lobehub/icons-static-svg/icons/openai.svg?raw";
import gemini from "@lobehub/icons-static-svg/icons/gemini.svg?raw";
import opencode from "@lobehub/icons-static-svg/icons/opencode.svg?raw";

const marks: Record<string, string> = { claude, codex: openai, gemini, opencode };

/** Package-owned SVGs only; never render server-supplied markup here. */
export function ProviderLogo({ kind }: { kind: string }) {
  const mark = marks[kind];
  return mark ? <span aria-hidden="true" className="inline-flex size-4 shrink-0 items-center justify-center [&>svg]:size-full" dangerouslySetInnerHTML={{ __html: mark }} /> : <Terminal aria-hidden="true" className="size-4 shrink-0" />;
}
