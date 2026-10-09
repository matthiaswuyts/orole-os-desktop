import { useEffect } from "react";
import type { Bot, Caps } from "@/mygo";
import { useLLM } from "@/stores/llm";

/** The model a bot talks to: its own, or the default chat model, with what the provider reports about it. */
export interface BotModel {
  /** Display name; empty when no model is configured anywhere. */
  label: string;
  /** True when the bot has no model of its own. */
  isDefault: boolean;
  /** Provider-reported capabilities, or null while unknown (not loaded, or not in the catalog). */
  caps: Caps | null;
}

/** Splits "provider/model"; the model id may itself contain slashes. */
function split(value: string): { provider: string; model: string } {
  const at = value.indexOf("/");
  return at < 0 ? { provider: "", model: "" } : { provider: value.slice(0, at), model: value.slice(at + 1) };
}

export function useBotModel(bot: Pick<Bot, "provider" | "model">): BotModel {
  const defaults = useLLM((s) => s.defaults);
  const own = bot.provider && bot.model;
  const { provider, model } = own ? { provider: bot.provider, model: bot.model } : split(defaults?.chatModel ?? "");
  const catalog = useLLM((s) => (provider ? s.models[provider] : undefined));
  useEffect(() => {
    if (provider) void useLLM.getState().loadModels(provider);
  }, [provider]);
  const found = catalog?.models.find((m) => m.id === model);
  return { label: found?.name || model, isDefault: !own, caps: found?.caps ?? null };
}
