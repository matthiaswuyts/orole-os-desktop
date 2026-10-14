import { BotIcon } from "lucide-react";
import anthropic from "@lobehub/icons-static-svg/icons/anthropic.svg?raw";
import openai from "@lobehub/icons-static-svg/icons/openai.svg?raw";
import gemini from "@lobehub/icons-static-svg/icons/gemini.svg?raw";
import meta from "@lobehub/icons-static-svg/icons/meta.svg?raw";
import xai from "@lobehub/icons-static-svg/icons/xai.svg?raw";
import kimi from "@lobehub/icons-static-svg/icons/kimi.svg?raw";
import xiaomi from "@lobehub/icons-static-svg/icons/xiaomimimo.svg?raw";
import qwen from "@lobehub/icons-static-svg/icons/qwen.svg?raw";
import zai from "@lobehub/icons-static-svg/icons/zai.svg?raw";
import deepseek from "@lobehub/icons-static-svg/icons/deepseek.svg?raw";
import mistral from "@lobehub/icons-static-svg/icons/mistral.svg?raw";
import cohere from "@lobehub/icons-static-svg/icons/cohere.svg?raw";
import nvidia from "@lobehub/icons-static-svg/icons/nvidia.svg?raw";
import microsoft from "@lobehub/icons-static-svg/icons/microsoft.svg?raw";
import perplexity from "@lobehub/icons-static-svg/icons/perplexity.svg?raw";
import minimax from "@lobehub/icons-static-svg/icons/minimax.svg?raw";
import ibm from "@lobehub/icons-static-svg/icons/ibm.svg?raw";
import aws from "@lobehub/icons-static-svg/icons/aws.svg?raw";
import { cn } from "@/lib/utils";

/** Monochrome marks (they follow the text colour), keyed by the company names `lib/models` and `lib/modelCatalog` produce. */
const MARKS: Record<string, string> = {
  Anthropic: anthropic, OpenAI: openai, Google: gemini, Meta: meta, xAI: xai, "Moonshot AI": kimi, Xiaomi: xiaomi,
  Alibaba: qwen, "Z.ai": zai, DeepSeek: deepseek, Mistral: mistral, Cohere: cohere, NVIDIA: nvidia, Microsoft: microsoft,
  Perplexity: perplexity, MiniMax: minimax, IBM: ibm, Amazon: aws,
};

/** Whether a company has a bundled mark, for callers that would rather show nothing than the generic bot glyph. */
export const hasCompanyLogo = (company: string) => company in MARKS;

/** A company's mark, or a generic bot glyph when none is bundled. Decorative: callers label the company in text. */
export function CompanyLogo({ company, className }: { company: string; className?: string }) {
  const mark = MARKS[company];
  return mark
    // Vetted bundled SVGs only; never render server-supplied markup here.
    ? <span aria-hidden="true" className={cn("inline-flex size-4 shrink-0 items-center justify-center [&>svg]:size-full", className)} dangerouslySetInnerHTML={{ __html: mark }} />
    : <BotIcon aria-hidden="true" className={cn("size-4 shrink-0", className)} />;
}
