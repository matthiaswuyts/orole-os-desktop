import type { Model } from "@/mygo";

/** Model families that are not chat models: embeddings, image/video/audio generation, speech, moderation, rerankers. */
const NON_CHAT = /embed|dall-?e|gpt-image|\bimagen|image-(?:gen|preview)|-image\b|stable-diffusion|\bsdxl|flux|midjourney|\bsora\b|\bveo\b|whisper|\btts\b|-tts|transcribe|diariz|text-to-speech|speech|realtime|audio|moderation|rerank|guard\b|\bocr\b/i;

/** True for models a bot can hold a conversation with. */
export function isChatModel(model: Pick<Model, "id" | "name">): boolean {
  return !NON_CHAT.test(model.id) && !NON_CHAT.test(model.name);
}

/** Known model-family prefixes, in match order, mapped to the company that makes them. */
const FAMILIES: [RegExp, string][] = [
  [/^(?:gpt|chatgpt|o\d|codex|davinci|babbage)(?:[-.\d]|$)/i, "OpenAI"],
  [/^claude/i, "Anthropic"],
  [/^(?:gemini|gemma|palm|learnlm)/i, "Google"],
  [/^(?:llama|meta-llama|codellama)/i, "Meta"],
  [/^(?:mistral|mixtral|codestral|ministral|pixtral|magistral|devstral|open-mi)/i, "Mistral"],
  [/^(?:qwen|qwq|qvq)/i, "Alibaba"],
  [/^deepseek/i, "DeepSeek"],
  [/^grok/i, "xAI"],
  [/^(?:command|c4ai|aya)/i, "Cohere"],
  [/^(?:phi|wizardlm)/i, "Microsoft"],
  [/^(?:nova|titan)/i, "Amazon"],
  [/^(?:glm|chatglm)/i, "Z.ai"],
  [/^(?:kimi|moonshot)/i, "Moonshot AI"],
  [/^mimo/i, "Xiaomi"],
  [/^(?:sonar|pplx)/i, "Perplexity"],
  [/^minimax/i, "MiniMax"],
  [/^(?:ernie)/i, "Baidu"],
  [/^(?:yi-)/i, "01.AI"],
  [/^(?:nemotron)/i, "NVIDIA"],
  [/^(?:granite)/i, "IBM"],
  [/^(?:jamba)/i, "AI21"],
  [/^(?:hermes)/i, "Nous Research"],
];

const VENDOR_NAMES: Record<string, string> = {
  openai: "OpenAI", anthropic: "Anthropic", google: "Google", "meta-llama": "Meta", meta: "Meta", mistralai: "Mistral", mistral: "Mistral",
  qwen: "Alibaba", alibaba: "Alibaba", deepseek: "DeepSeek", "deepseek-ai": "DeepSeek", "x-ai": "xAI", xai: "xAI", cohere: "Cohere",
  microsoft: "Microsoft", amazon: "Amazon", "z-ai": "Z.ai", zhipu: "Z.ai", moonshotai: "Moonshot AI", perplexity: "Perplexity",
  xiaomi: "Xiaomi", minimax: "MiniMax", nvidia: "NVIDIA", ibm: "IBM", "ai21": "AI21", nousresearch: "Nous Research", baidu: "Baidu",
};

/**
 * The AI company behind a model. Aggregators (OpenRouter and similar) namespace ids as `vendor/model`, so that prefix
 * wins; otherwise the model family is recognised from the id. Returns "Other" when neither says.
 */
export function companyOf(model: Pick<Model, "id" | "name">): string {
  const slash = model.id.indexOf("/");
  const bare = model.id.slice(slash + 1).replace(/^models\//, "");
  if (slash > 0) {
    const vendor = model.id.slice(0, slash).toLowerCase();
    if (vendor !== "models") return VENDOR_NAMES[vendor] ?? vendor.replace(/[-_]+/g, " ").replace(/\b\w/g, (c) => c.toUpperCase());
  }
  for (const [pattern, company] of FAMILIES) if (pattern.test(bare)) return company;
  return "Other";
}
