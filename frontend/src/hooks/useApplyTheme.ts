import { useEffect } from "react";
import { useSettings } from "@/stores/settings";
import { MIDNIGHT, MIDNIGHT_KEYS } from "@/themes/builtin";

/** Applies theme, density and custom tokens to <html>, the way Shipyard's applyTheme does. */
export function useApplyTheme() {
  const { theme, density, customTokens } = useSettings((s) => s.appearance);
  useEffect(() => {
    const el = document.documentElement;
    for (const k of MIDNIGHT_KEYS) el.style.removeProperty(k);
    const vars = theme === "midnight" || theme === "custom" ? MIDNIGHT : {};
    for (const [k, v] of Object.entries(vars)) el.style.setProperty(k, v);
    if (theme === "custom") for (const [k, v] of Object.entries(customTokens)) el.style.setProperty(`--${k}`, v);
    const dark = theme !== "light";
    el.classList.toggle("dark", dark);
    el.style.colorScheme = dark ? "dark" : "light";
    el.dataset.density = density;
  }, [theme, density, customTokens]);
}
