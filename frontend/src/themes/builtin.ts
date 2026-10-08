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
export const MIDNIGHT_KEYS = Object.keys(MIDNIGHT);
