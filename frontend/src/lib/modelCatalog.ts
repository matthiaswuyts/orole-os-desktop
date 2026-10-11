/**
 * Handpicked models the bot model picker offers, so users choose among the best instead of scrolling a provider's
 * entire catalogue (which also lists dated snapshots, legacy models and variants).
 *
 * Source: Artificial Analysis LLM leaderboard (https://artificialanalysis.ai/leaderboards/models), read 2026-10-10.
 * Selection rule: keep a model only if nothing else on the board beats it on intelligence, cost per task or output
 * speed without losing badly on the others. Stats come from the row at "high" reasoning effort, or the highest effort
 * listed when the board has no "high" row, so models compare at a similar working point. Refresh this list when the
 * board moves; ids are matched against provider catalogues by `matchCurated`, so a model a provider does not offer
 * simply does not appear.
 */

export type Strength = "performance" | "value" | "speed";

export interface CuratedModel {
  /** normalized model id: lowercase, `.` and `_` as `-`, no vendor prefix */
  id: string;
  name: string;
  /** one line on why it is on the list, shown under the name */
  blurb: string;
  company: string;
  /** what this model is best at; a model can be best at several things */
  strengths: Strength[];
  /** Artificial Analysis Intelligence Index, higher is better. Recorded as the basis for the pick; the picker does not show it. */
  intelligence: number;
  /** USD to run Artificial Analysis' benchmark tasks, lower is better. Recorded as the basis for the pick; the picker does not show it. */
  costPerTask: number;
  /** median output tokens per second, null when the board has no measurement */
  speed: number | null;
}

export const STRENGTH_LABEL: Record<Strength, string> = { performance: "Best performance", value: "Best value", speed: "Fastest" };

/** Ordered for display: frontier performance first, then value, then speed. */
export const CURATED: CuratedModel[] = [
  { id: "claude-opus-5-5", name: "Claude Opus 5.5", blurb: "Top intelligence of any model tested.", company: "Anthropic", strengths: ["performance"], intelligence: 54, costPerTask: 1.82, speed: 77 },
  { id: "gpt-6-astra", name: "GPT-6 Astra", blurb: "OpenAI's highest-scoring model.", company: "OpenAI", strengths: ["performance"], intelligence: 51, costPerTask: 1.73, speed: 42 },
  { id: "gemini-4-argon", name: "Gemini 4 Argon", blurb: "Google's highest-scoring model.", company: "Google", strengths: ["performance"], intelligence: 53, costPerTask: 1.99, speed: null },
  { id: "claude-sonnet-5-5", name: "Claude Sonnet 5.5", blurb: "Strong all-rounder: about half Opus's cost, faster output.", company: "Anthropic", strengths: ["performance"], intelligence: 47, costPerTask: 0.88, speed: 101 },
  { id: "muse-spark-1-3", name: "Muse Spark 1.3", blurb: "Meta's top model, with very fast output.", company: "Meta", strengths: ["performance", "speed"], intelligence: 48, costPerTask: 1.6, speed: 175 },
  { id: "grok-4-7", name: "Grok 4.7", blurb: "xAI's top model.", company: "xAI", strengths: ["performance"], intelligence: 46, costPerTask: 2.73, speed: 64 },
  { id: "kimi-k3", name: "Kimi K3", blurb: "Moonshot AI's top model.", company: "Moonshot AI", strengths: ["performance"], intelligence: 44, costPerTask: 2, speed: 41 },
  { id: "gpt-6-1-sol", name: "GPT-6.1 Sol", blurb: "Near-frontier scores at a fraction of the cost.", company: "OpenAI", strengths: ["performance", "value"], intelligence: 50, costPerTask: 0.32, speed: 50 },
  { id: "mimo-v2-6-pro", name: "MiMo-V2.6-Pro", blurb: "Mid-frontier scores at the lowest cost.", company: "Xiaomi", strengths: ["value"], intelligence: 46, costPerTask: 0.13, speed: 43 },
  { id: "qwen3-8-flash-next", name: "Qwen3.8 Flash Next", blurb: "Alibaba's fast, low-cost model.", company: "Alibaba", strengths: ["value"], intelligence: 40, costPerTask: 0.37, speed: 56 },
  { id: "glm-5-3-flash", name: "GLM-5.3 Flash", blurb: "Z.ai's low-cost model.", company: "Z.ai", strengths: ["value"], intelligence: 42, costPerTask: 0.25, speed: 53 },
  { id: "claude-haiku-5-5", name: "Claude Haiku 5.5", blurb: "Cheap and fast for everyday chat.", company: "Anthropic", strengths: ["value", "speed"], intelligence: 38, costPerTask: 0.08, speed: 174 },
  { id: "gpt-6-luna", name: "GPT-6 Luna", blurb: "Cheapest on the list, for simple tasks.", company: "OpenAI", strengths: ["value", "speed"], intelligence: 33, costPerTask: 0.03, speed: 120 },
  { id: "deepseek-v4-1-flash", name: "DeepSeek V4.1 Flash", blurb: "Very fast and very cheap for its quality.", company: "DeepSeek", strengths: ["value", "speed"], intelligence: 39, costPerTask: 0.27, speed: 217 },
  { id: "gemini-3-8-flash", name: "Gemini 3.8 Flash", blurb: "Google's fast, balanced model.", company: "Google", strengths: ["speed"], intelligence: 41, costPerTask: 1.24, speed: 125 },
  { id: "gemini-3-5-flash-lite", name: "Gemini 3.5 Flash-Lite", blurb: "Google's fastest, for simple high-volume work.", company: "Google", strengths: ["speed"], intelligence: 22, costPerTask: 0.19, speed: 365 },
];

/** Longest id first, so `gpt-6-1-sol` is never mistaken for a shorter prefix. */
const BY_SPECIFICITY = [...CURATED].sort((a, b) => b.id.length - a.id.length);

/** Suffixes that make a model a different product from its curated base (smaller, vision-only, search, ...). */
const OTHER_PRODUCT = /(^|-)(lite|mini|nano|micro|image|audio|tts|realtime|embedding|search|fast|pro|codex|vision|instruct)(-|$)/;

/** The part of a provider model id after any vendor/`models/` prefix, normalized the way `CuratedModel.id` is. */
export const normalizeModelId = (id: string): string => id.slice(id.lastIndexOf("/") + 1).toLowerCase().replace(/[._\s]+/g, "-");

/** The curated model a provider's model id is (an alias or dated snapshot of), or undefined. */
export function matchCurated(providerModelId: string): CuratedModel | undefined {
  const bare = normalizeModelId(providerModelId);
  return BY_SPECIFICITY.find((m) => bare === m.id || (bare.startsWith(`${m.id}-`) && !OTHER_PRODUCT.test(bare.slice(m.id.length + 1))));
}
