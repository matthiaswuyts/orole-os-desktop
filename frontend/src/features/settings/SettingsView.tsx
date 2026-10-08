import { useState, type ReactNode } from "react";
import { Accessibility, Bell, Bot, Cpu, HardDrive, Image, Palette, PlugZap, Server, ShieldCheck, SquareTerminal, Stethoscope, Workflow, Download } from "lucide-react";
import { useSettings, type ThemeId } from "@/stores/settings";
import { useDaemon } from "@/stores/daemon";
import { SCENES } from "@/components/ascii/AsciiBackground";
import { Switch } from "@/components/ui/switch";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";
import { AutostartPanel } from "@/features/settings/Autostart";
import { CUSTOM_TOKEN_KEYS, validateCustomTokens } from "@/themes/custom";
import { NotImplemented } from "@/features/shared/States";
import { fmtUptime } from "@/components/widgets/builtin";
import { GitPullRequest } from "lucide-react";
import { GitHubSettings } from "@/features/github/GitHubSettings";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { AutomationSettings } from "@/features/automations/AutomationSettings";
import { MCPSettings } from "@/features/mcp/MCPSettings";
import { Puzzle } from "lucide-react";
import { PluginSettings } from "@/features/plugins";
import { SoundSettings } from "@/features/settings/SoundSettings";
import { Volume2 } from "lucide-react";
import { UpdateSettings } from "@/features/settings/UpdateSettings";
import { useUI } from "@/stores/ui";
import { WorkspaceSettings } from "@/features/settings/WorkspaceSettings";
import { PanelsTopLeft } from "lucide-react";

type Section = { id: string; label: string; icon: typeof Palette; built: boolean };
const SECTIONS: Section[] = [
  { id: "appearance", label: "Appearance", icon: Palette, built: true },
  { id: "workspaces", label: "Workspaces", icon: PanelsTopLeft, built: true },
  { id: "ascii", label: "ASCII scenes", icon: Image, built: true },
  { id: "accessibility", label: "Accessibility", icon: Accessibility, built: true },
  { id: "terminal", label: "Terminal", icon: SquareTerminal, built: true },
  { id: "machines", label: "Machines", icon: Server, built: false },
  { id: "agents", label: "Agents", icon: Bot, built: false },
  { id: "github", label: "GitHub", icon: GitPullRequest, built: true },
  { id: "mcp", label: "MCP servers", icon: PlugZap, built: true },
  { id: "plugins", label: "Plugins", icon: Puzzle, built: true },
  { id: "notifications", label: "Notifications", icon: Bell, built: true },
  { id: "sound", label: "Sounds", icon: Volume2, built: true },
  { id: "automations", label: "Automations", icon: Workflow, built: true },
  { id: "security", label: "Security", icon: ShieldCheck, built: false },
  { id: "storage", label: "Storage", icon: HardDrive, built: false },
  { id: "updates", label: "Updates", icon: Download, built: true },
  { id: "diagnostics", label: "Diagnostics", icon: Stethoscope, built: true },
];

export function SettingsSection({ title, description, children }: { title: string; description?: string; children: ReactNode }) {
  return (
    <section className="mb-5">
      <h2 className="text-sm font-medium">{title}</h2>
      {description && <p className="mb-2 text-muted-foreground">{description}</p>}
      <div className="divide-y rounded-lg border bg-card">{children}</div>
    </section>
  );
}

export function Field({ label, hint, children, htmlFor }: { label: string; hint?: string; children: ReactNode; htmlFor?: string }) {
  return (
    <div className="flex items-center justify-between gap-6 px-3 py-2.5">
      <div className="min-w-0">
        <label htmlFor={htmlFor} className="block">{label}</label>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  );
}

const THEMES: { id: ThemeId; label: string; sw: string[] }[] = [
  { id: "midnight", label: "Orole Midnight", sw: ["#0b1020", "#1b2a50", "#8fb0ff", "#f0b45a"] },
  { id: "obsidian", label: "Orole Obsidian", sw: ["#0c0c0e", "#1e1e22", "#6fe0b0", "#8fb0ff"] },
  { id: "light", label: "Orole Light", sw: ["#f3f5fa", "#ffffff", "#34509a", "#a8741c"] },
];

export function ThemePicker() {
  const { theme } = useSettings((s) => s.appearance);
  const set = useSettings((s) => s.set);
  return (
    <div role="radiogroup" aria-label="Theme" className="flex gap-2 p-3">
      {THEMES.map((t) => (
        <button key={t.id} role="radio" aria-checked={theme === t.id} onClick={() => set("appearance", { theme: t.id })}
          className={cn("flex w-36 flex-col gap-1.5 rounded-lg border p-2 text-left hover:bg-accent", theme === t.id ? "border-primary" : "border-border")}>
          <span className="flex h-9 overflow-hidden rounded">{t.sw.map((c) => <span key={c} className="flex-1" style={{ background: c }} />)}</span>
          <span>{t.label}</span>
        </button>
      ))}
    </div>
  );
}

function CustomTheme() {
  const { customTokens, theme } = useSettings((s) => s.appearance);
  const set = useSettings((s) => s.set);
  const [text, setText] = useState(JSON.stringify(Object.keys(customTokens).length ? customTokens : { primary: "#8fb0ff", "accent-secondary": "#f0b45a" }, null, 2));
  const [err, setErr] = useState<string | null>(null);
  return (
    <div className="space-y-2 p-3">
      <p className="text-muted-foreground">Custom theme: JSON of validated colour tokens over Midnight. Allowed: <span className="font-mono">{CUSTOM_TOKEN_KEYS.join(", ")}</span>.</p>
      <Textarea aria-label="Custom theme tokens" className="selectable h-28 font-mono" value={text} onChange={(e) => setText(e.target.value)} />
      {err && <p role="alert" className="text-destructive-foreground">{err}</p>}
      <div className="flex gap-2">
        <Button size="xs" onClick={() => {
          try {
            const r = validateCustomTokens(JSON.parse(text));
            if (!r.ok) return setErr(r.error);
            setErr(null);
            set("appearance", { theme: "custom", customTokens: r.tokens });
          } catch { setErr("Not valid JSON."); }
        }}>Apply custom theme</Button>
        {theme === "custom" && <Button size="xs" variant="outline" onClick={() => set("appearance", { theme: "midnight", customTokens: {} })}>Remove</Button>}
      </div>
    </div>
  );
}

function Appearance() {
  const { density } = useSettings((s) => s.appearance);
  const set = useSettings((s) => s.set);
  return (
    <>
      <SettingsSection title="Theme"><ThemePicker /><CustomTheme /></SettingsSection>
      <SettingsSection title="Density" description="Compact is the default; comfortable adds spacing.">
        <Field label="Compact density"><Switch checked={density === "compact"} onCheckedChange={(c) => set("appearance", { density: c ? "compact" : "comfortable" })} aria-label="Compact density" /></Field>
      </SettingsSection>
    </>
  );
}

function Ascii() {
  const a = useSettings((s) => s.ascii);
  const set = useSettings((s) => s.set);
  const patch = (p: Partial<typeof a>) => set("ascii", p);
  const num = (v: number | readonly number[]) => (Array.isArray(v) ? (v[0] as number) : (v as number));
  return (
    <>
      <SettingsSection title="Scene" description="Animated backgrounds from ascii.rest, rendered once behind the whole workspace.">
        <Field label="Show background"><Switch checked={a.enabled} onCheckedChange={(c) => patch({ enabled: c })} aria-label="Show background" /></Field>
        <div role="radiogroup" aria-label="Scene" className="grid grid-cols-3 gap-1.5 p-3">
          {SCENES.map((s) => (
            <button key={s.id} role="radio" aria-checked={a.scene === s.id} onClick={() => patch({ scene: s.id })}
              className={cn("rounded-md border px-2 py-1.5 text-left hover:bg-accent", a.scene === s.id ? "border-primary bg-accent" : "border-border")}>{s.label}</button>
          ))}
        </div>
      </SettingsSection>
      <SettingsSection title="Motion">
        <Field label="Pause animation" hint="Shows a still frame."><Switch checked={a.paused} onCheckedChange={(c) => patch({ paused: c })} aria-label="Pause animation" /></Field>
        <Field label="Static only" hint="Never animate, regardless of other settings."><Switch checked={a.staticOnly} onCheckedChange={(c) => patch({ staticOnly: c })} aria-label="Static only" /></Field>
        <Field label={`Frame rate: ${a.fps} fps`} htmlFor="ascii-fps"><Slider id="ascii-fps" aria-label="Frame rate" className="w-48" min={1} max={30} step={1} value={[a.fps]} onValueChange={(v) => patch({ fps: num(v) })} /></Field>
        <Field label={`Intensity: ${Math.round(a.intensity * 100)}%`} hint="Scales frame rate and brightness together."><Slider aria-label="Intensity" className="w-48" min={0} max={1} step={0.05} value={[a.intensity]} onValueChange={(v) => patch({ intensity: num(v) })} /></Field>
        <Field label={`Opacity: ${Math.round(a.opacity * 100)}%`}><Slider aria-label="Opacity" className="w-48" min={0.1} max={1} step={0.05} value={[a.opacity]} onValueChange={(v) => patch({ opacity: num(v) })} /></Field>
        <Field label="Pause when window is unfocused" hint="Saves CPU and GPU in the background."><Switch checked={a.pauseWhenUnfocused} onCheckedChange={(c) => patch({ pauseWhenUnfocused: c })} aria-label="Pause when unfocused" /></Field>
      </SettingsSection>
    </>
  );
}

function A11y() {
  const a = useSettings((s) => s.accessibility);
  const asc = useSettings((s) => s.ascii);
  const set = useSettings((s) => s.set);
  return (
    <SettingsSection title="Motion" description="Windows “Show animations” and prefers-reduced-motion are honoured by default.">
      <Field label="Reduce motion">
        <select aria-label="Reduce motion" value={a.reduceMotion} onChange={(e) => set("accessibility", { reduceMotion: e.target.value as typeof a.reduceMotion })} className="rounded-md border bg-transparent px-2 py-1">
          <option value="system">Follow system</option><option value="on">Always reduce</option><option value="off">Never reduce</option>
        </select>
      </Field>
      <Field label="Respect system setting for background"><Switch checked={asc.respectReducedMotion} onCheckedChange={(c) => set("ascii", { respectReducedMotion: c })} aria-label="Respect system reduced motion" /></Field>
    </SettingsSection>
  );
}

function TerminalSettingsSection() {
  const t = useSettings((s) => s.terminal);
  const set = useSettings((s) => s.set);
  return (
    <SettingsSection title="Terminal">
      <Field label={`Font size: ${t.fontSize}px`}><Slider aria-label="Terminal font size" className="w-48" min={9} max={22} step={1} value={[t.fontSize]} onValueChange={(v) => set("terminal", { fontSize: Array.isArray(v) ? (v[0] as number) : (v as number) })} /></Field>
    </SettingsSection>
  );
}

function Diagnostics() {
  const { link, health, error, refresh } = useDaemon();
  const rows: [string, string][] = health ? [
    ["Daemon", `v${health.version} · pid ${health.pid}`], ["Uptime", fmtUptime(health.uptimeSec)], ["Platform", `${health.platform} · ${health.goVersion}`],
    ["Memory", `${health.memAllocMB.toFixed(1)} MB`], ["Goroutines", String(health.goroutines)], ["Schema", `v${health.dbVersion}`], ["IPC protocol", `v${health.protocolVersion}`],
  ] : [];
  return (
    <SettingsSection title="Diagnostics" description="Daemon logs are in %LOCALAPPDATA%\Orole\logs\daemon.log.">
      <Field label="Connection"><span className="font-mono">{link}</span></Field>
      {error && <p role="alert" className="px-3 py-2 text-destructive-foreground">{error}</p>}
      {rows.map(([k, v]) => <Field key={k} label={k}><span className="font-mono">{v}</span></Field>)}
      <div className="p-3"><Button size="xs" variant="outline" onClick={() => void refresh()}><Cpu />Refresh</Button></div>
    </SettingsSection>
  );
}

export function SettingsView() {
  const requested = useUI((state) => state.settingsSection);
  const sec = SECTIONS.find((section) => section.id === requested) ?? SECTIONS[0]!;
  const id = sec.id;
  const setId = useUI((state) => state.openSettings);
  return (
    <div className="flex h-full min-h-0 gap-4 p-4">
      <nav aria-label="Settings sections" className="w-44 shrink-0 border-r pr-2">
        {SECTIONS.map((s) => (
          <button key={s.id} aria-current={id === s.id ? "page" : undefined} onClick={() => setId(s.id)}
            className={cn("flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-muted-foreground hover:bg-accent hover:text-foreground", id === s.id && "bg-accent text-foreground")}>
            <s.icon className="size-3.5" />{s.label}
          </button>
        ))}
      </nav>
      <div className="min-h-0 flex-1 overflow-auto pr-1">
        <h1 className="mb-3 text-base font-medium">{sec.label}</h1>
        {id === "appearance" && <Appearance />}
        {id === "workspaces" && <WorkspaceSettings />}
        {id === "ascii" && <Ascii />}
        {id === "accessibility" && <A11y />}
        {id === "terminal" && <TerminalSettingsSection />}
        {id === "github" && <GitHubSettings />}
        {id === "mcp" && <MCPSettings />}
        {id === "plugins" && <PluginSettings />}
        {id === "notifications" && <NotificationSettings />}
        {id === "sound" && <SoundSettings />}
        {id === "automations" && <AutomationSettings />}
        {id === "updates" && <UpdateSettings />}
        {id === "diagnostics" && <><Diagnostics /><AutostartPanel /></>}
        {!sec.built && <NotImplemented app={`${sec.label} settings`} />}
      </div>
    </div>
  );
}
