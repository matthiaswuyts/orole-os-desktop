import type { ITheme } from "@xterm/xterm";

// Orole Midnight colours come from Shipyard's "Midnight" theme (app/src/themes/builtin.ts, MIT).
// Orole Obsidian is Shipyard's default dark token set and Orole Light its light set, both defined
// in index.css; they need no inline overrides.
export const MIDNIGHT: Record<string, string> = {
  "--background": "#0f1420", "--foreground": "#d7deef", "--sidebar": "#0b0f19", "--sidebar-foreground": "#93a0bd",
  "--muted": "#161c2b", "--muted-foreground": "#7c8aaa", "--border": "#1e2639", "--input": "#1e2639",
  "--accent": "#1a2236", "--accent-foreground": "#e7ecf8", "--primary": "#8fb3ff", "--primary-foreground": "#0b0f19",
  "--secondary": "#161c2b", "--secondary-foreground": "#d7deef", "--card": "#121828", "--card-foreground": "#d7deef",
  "--popover": "#141b2c", "--popover-foreground": "#d7deef", "--ring": "#566792",
  "--success": "#4ccf8b", "--warning": "#f2b65a", "--destructive": "#ff6b7a", "--info": "#2b7fff",
  "--success-foreground": "#00d492", "--warning-foreground": "#ffb900", "--destructive-foreground": "#ff6467", "--info-foreground": "#51a2ff",
  "--sidebar-accent": "#1a2236", "--sidebar-accent-foreground": "#e7ecf8", "--sidebar-border": "#1e2639",
  "--sidebar-primary": "#8fb3ff", "--sidebar-primary-foreground": "#0b0f19", "--sidebar-ring": "#566792",
};
interface ThemeDefinition {
  id: string;
  label: string;
  dark: boolean;
  sw: readonly string[];
  tokens: Record<string, string>;
  terminal?: ITheme;
}

interface Palette {
  base: string;
  mantle: string;
  surface0: string;
  surface1: string;
  text: string;
  subtext1: string;
  blue: string;
  green: string;
  yellow: string;
  red: string;
  mauve: string;
  teal: string;
  peach: string;
}

function paletteTokens(p: Palette, primary = p.blue, onPrimary = p.base): Record<string, string> {
  return {
    "--background": p.base, "--foreground": p.text, "--sidebar": p.mantle, "--sidebar-foreground": p.subtext1,
    "--muted": p.surface0, "--muted-foreground": p.subtext1, "--border": p.surface1, "--input": p.surface1,
    "--accent": p.surface0, "--accent-foreground": p.text, "--primary": primary, "--primary-foreground": onPrimary,
    "--secondary": p.surface0, "--secondary-foreground": p.text, "--card": p.mantle, "--card-foreground": p.text,
    "--popover": p.mantle, "--popover-foreground": p.text, "--ring": primary,
    "--success": p.green, "--warning": p.yellow, "--destructive": p.red, "--info": p.blue,
    "--success-foreground": p.green, "--warning-foreground": p.yellow, "--destructive-foreground": p.red, "--info-foreground": p.blue,
    "--sidebar-accent": p.surface0, "--sidebar-accent-foreground": p.text, "--sidebar-border": p.surface1,
    "--sidebar-primary": primary, "--sidebar-primary-foreground": onPrimary, "--sidebar-ring": primary,
    "--chart-1": p.blue, "--chart-2": p.green, "--chart-3": p.yellow, "--chart-4": p.mauve, "--chart-5": p.peach,
    "--code": p.mantle, "--code-foreground": p.text, "--code-highlight": p.surface0,
  };
}

function catppuccin(p: Palette) {
  return {
    tokens: paletteTokens(p),
    sw: [p.base, p.surface0, p.blue, p.mauve],
    terminal: {
      black: p.surface0, brightBlack: p.surface1, red: p.red, brightRed: p.red,
      green: p.green, brightGreen: p.green, yellow: p.yellow, brightYellow: p.yellow,
      blue: p.blue, brightBlue: p.blue, magenta: p.mauve, brightMagenta: p.mauve,
      cyan: p.teal, brightCyan: p.teal, white: p.subtext1, brightWhite: p.text,
    },
  };
}

// Canonical sRGB palettes: https://ethanschoonover.com/solarized/
// https://github.com/catppuccin/palette/blob/main/palette.json (v1.8.0).
const SOLARIZED_DARK: Palette = {
  base: "#002b36", mantle: "#073642", surface0: "#073642", surface1: "#586e75",
  text: "#93a1a1", subtext1: "#93a1a1", blue: "#268bd2", green: "#859900",
  yellow: "#b58900", red: "#dc322f", mauve: "#6c71c4", teal: "#2aa198", peach: "#cb4b16",
};
const SOLARIZED_LIGHT: Palette = {
  ...SOLARIZED_DARK, base: "#fdf6e3", mantle: "#eee8d5", surface0: "#eee8d5",
  surface1: "#93a1a1", text: "#586e75", subtext1: "#586e75",
};
const SOLARIZED_TERMINAL: ITheme = {
  black: "#073642", red: "#dc322f", green: "#859900", yellow: "#b58900",
  blue: "#268bd2", magenta: "#d33682", cyan: "#2aa198", white: "#eee8d5",
  brightBlack: "#002b36", brightRed: "#cb4b16", brightGreen: "#586e75", brightYellow: "#657b83",
  brightBlue: "#839496", brightMagenta: "#6c71c4", brightCyan: "#93a1a1", brightWhite: "#fdf6e3",
};

/** Shared by settings, onboarding and the command palette; IDs are persisted unchanged. */
export const BUILTIN_THEMES = [
  { id: "midnight", label: "Orole Midnight", dark: true, tokens: MIDNIGHT, sw: ["#0b1020", "#1b2a50", "#8fb0ff", "#f0b45a"] },
  { id: "obsidian", label: "Orole Obsidian", dark: true, tokens: {}, sw: ["#0c0c0e", "#1e1e22", "#6fe0b0", "#8fb0ff"] },
  { id: "light", label: "Orole Light", dark: false, tokens: {}, sw: ["#f3f5fa", "#ffffff", "#34509a", "#a8741c"] },
  {
    id: "solarized-dark", label: "Solarized Dark", dark: true,
    tokens: paletteTokens(SOLARIZED_DARK, "#2aa198", "#002b36"), terminal: SOLARIZED_TERMINAL,
    sw: ["#002b36", "#073642", "#268bd2", "#b58900"],
  },
  {
    id: "solarized-light", label: "Solarized Light", dark: false,
    tokens: paletteTokens(SOLARIZED_LIGHT, "#586e75", "#fdf6e3"), terminal: SOLARIZED_TERMINAL,
    sw: ["#fdf6e3", "#eee8d5", "#268bd2", "#b58900"],
  },
  {
    id: "catppuccin-latte", label: "Catppuccin Latte", dark: false,
    ...catppuccin({
      base: "#eff1f5", mantle: "#e6e9ef", surface0: "#ccd0da", surface1: "#bcc0cc",
      text: "#4c4f69", subtext1: "#5c5f77", blue: "#1e66f5", green: "#40a02b",
      yellow: "#df8e1d", red: "#d20f39", mauve: "#8839ef", teal: "#179299", peach: "#fe640b",
    }),
  },
  {
    id: "catppuccin-frappe", label: "Catppuccin Frappé", dark: true,
    ...catppuccin({
      base: "#303446", mantle: "#292c3c", surface0: "#414559", surface1: "#51576d",
      text: "#c6d0f5", subtext1: "#b5bfe2", blue: "#8caaee", green: "#a6d189",
      yellow: "#e5c890", red: "#e78284", mauve: "#ca9ee6", teal: "#81c8be", peach: "#ef9f76",
    }),
  },
  {
    id: "catppuccin-macchiato", label: "Catppuccin Macchiato", dark: true,
    ...catppuccin({
      base: "#24273a", mantle: "#1e2030", surface0: "#363a4f", surface1: "#494d64",
      text: "#cad3f5", subtext1: "#b8c0e0", blue: "#8aadf4", green: "#a6da95",
      yellow: "#eed49f", red: "#ed8796", mauve: "#c6a0f6", teal: "#8bd5ca", peach: "#f5a97f",
    }),
  },
  {
    id: "catppuccin-mocha", label: "Catppuccin Mocha", dark: true,
    ...catppuccin({
      base: "#1e1e2e", mantle: "#181825", surface0: "#313244", surface1: "#45475a",
      text: "#cdd6f4", subtext1: "#bac2de", blue: "#89b4fa", green: "#a6e3a1",
      yellow: "#f9e2af", red: "#f38ba8", mauve: "#cba6f7", teal: "#94e2d5", peach: "#fab387",
    }),
  },
] as const satisfies readonly ThemeDefinition[];

export type BuiltinThemeId = (typeof BUILTIN_THEMES)[number]["id"];

/** Custom themes still layer over Midnight; unknown persisted IDs retain the default. */
export function getBuiltinTheme(id: string): ThemeDefinition {
  return BUILTIN_THEMES.find((theme) => theme.id === id) ?? BUILTIN_THEMES[0];
}

export const BUILTIN_TOKEN_KEYS = [...new Set(BUILTIN_THEMES.flatMap((theme) => Object.keys(theme.tokens)))];
