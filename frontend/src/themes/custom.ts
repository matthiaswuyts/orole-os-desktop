/** Custom theme tokens: only whitelisted semantic tokens, only safe colour values. */
export const CUSTOM_TOKEN_KEYS = [
  "background", "foreground", "primary", "primary-foreground", "card", "sidebar",
  "success", "warning", "destructive", "border", "muted-foreground",
] as const;
export type CustomTokenKey = (typeof CUSTOM_TOKEN_KEYS)[number];

const COLOR = /^(#[0-9a-fA-F]{3,8}|(oklch|rgb|rgba|hsl|hsla)\(\s*[-0-9a-zA-Z.%/,\s]+\))$/;

export function validateCustomTokens(input: unknown): { ok: true; tokens: Record<string, string> } | { ok: false; error: string } {
  if (typeof input !== "object" || input === null || Array.isArray(input)) return { ok: false, error: "Theme must be a JSON object of token: color." };
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(input)) {
    if (!(CUSTOM_TOKEN_KEYS as readonly string[]).includes(k)) return { ok: false, error: `Unknown token “${k}”.` };
    if (typeof v !== "string" || !COLOR.test(v.trim())) return { ok: false, error: `Token “${k}” needs a hex, rgb(), hsl() or oklch() colour.` };
    out[k] = v.trim();
  }
  return { ok: true, tokens: out };
}
