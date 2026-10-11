import { useLayoutEffect } from "react";
import { useSettings } from "@/stores/settings";
import { BUILTIN_TOKEN_KEYS, getBuiltinTheme } from "@/themes/builtin";

/** Read by the inline script in index.html to paint the first frame in the user's theme. */
export const BOOT_THEME_KEY = "orole.boot-theme";

/**
 * Applies theme, density and custom tokens to <html>, the way Shipyard's applyTheme does.
 * Waits for the real settings: applying the defaults first would flash Midnight over the theme
 * index.html restored from the boot cache. A failed load keeps whatever is already applied.
 */
export function useApplyTheme() {
  const { theme, density, customTokens } = useSettings((s) => s.appearance);
  const ready = useSettings((s) => s.loaded && !s.error);
  useLayoutEffect(() => {
    if (!ready) return;
    const el = document.documentElement;
    for (const k of BUILTIN_TOKEN_KEYS) el.style.removeProperty(k);
    const builtin = getBuiltinTheme(theme);
    const tokens: Record<string, string> = { ...builtin.tokens };
    if (theme === "custom") for (const [k, v] of Object.entries(customTokens)) tokens[`--${k}`] = v;
    for (const [k, v] of Object.entries(tokens)) el.style.setProperty(k, v);
    const dark = builtin.dark;
    el.classList.toggle("dark", dark);
    el.style.colorScheme = dark ? "dark" : "light";
    el.dataset.density = density;
    // Midnight-only tokens are inline; obsidian/light live in index.css, so cache the resolved
    // background/foreground pair too in case the cached token set is empty.
    try {
      const cs = getComputedStyle(el);
      for (const k of ["--background", "--foreground", "--muted-foreground"]) {
        if (!(k in tokens)) tokens[k] = cs.getPropertyValue(k).trim();
      }
      localStorage.setItem(BOOT_THEME_KEY, JSON.stringify({ dark, tokens }));
    } catch { /* storage unavailable: next start just boots in the default colours */ }
  }, [ready, theme, density, customTokens]);
}
