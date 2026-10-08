export interface ProviderPreference { model: string; reasoning: string }
export type ProviderPreferences = Record<string, ProviderPreference>;
const key = "orole.task.providerPreferences";

/** UI defaults only; model capabilities and permissions still come from Go. */
export function loadProviderPreferences(): ProviderPreferences {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(key) ?? "{}");
    if (!value || typeof value !== "object" || Array.isArray(value)) return {};
    return Object.fromEntries(Object.entries(value).filter((entry): entry is [string, ProviderPreference] => {
      const preference: unknown = entry[1];
      return Boolean(preference && typeof preference === "object" && "model" in preference && typeof preference.model === "string" && "reasoning" in preference && typeof preference.reasoning === "string");
    }));
  } catch { return {}; }
}

export function saveProviderPreferences(preferences: ProviderPreferences) {
  localStorage.setItem(key, JSON.stringify(preferences));
}
