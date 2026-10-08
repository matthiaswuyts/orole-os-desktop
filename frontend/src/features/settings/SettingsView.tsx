import { useEffect, useState, type ReactNode } from "react";
import { Bell, Cpu, Palette, PanelsTopLeft, PlugZap, Settings2 } from "lucide-react";
import { useSettings } from "@/stores/settings";
import { BUILTIN_THEMES } from "@/themes/builtin";
import { useDaemon } from "@/stores/daemon";
import { ScenePicker } from "@/features/settings/ScenePicker";
import { Switch } from "@/components/ui/switch";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { AutostartPanel } from "@/features/settings/Autostart";
import { CUSTOM_TOKEN_KEYS, validateCustomTokens } from "@/themes/custom";
import { fmtUptime } from "@/components/widgets/builtin";
import { GitHubSettings } from "@/features/github/GitHubSettings";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { AutomationSettings } from "@/features/automations/AutomationSettings";
import { PluginSettings } from "@/features/plugins";
import { SoundSettings } from "@/features/settings/SoundSettings";
import { UpdateSettings } from "@/features/settings/UpdateSettings";
import { useUI } from "@/stores/ui";
import { WorkspaceSettings } from "@/features/settings/WorkspaceSettings";

type Section = { id: string; label: string; icon: typeof Palette };
// Search and deep links address a section by id and, optionally, a tab or an anchor inside it (see openSettings).
const SECTIONS: Section[] = [
  { id: "customization", label: "Customization", icon: Palette },
  { id: "workspaces", label: "Workspaces", icon: PanelsTopLeft },
  { id: "integrations", label: "Integrations", icon: PlugZap },
  { id: "notifications", label: "Alerts & automations", icon: Bell },
  { id: "system", label: "System", icon: Settings2 },
];

export function SettingsSection({ title, description, children, id }: { title: string; description?: string; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="mb-5 scroll-mt-2">
      <h2 className="text-sm font-medium">{title}</h2>
      {description && <p className="mb-2 text-muted-foreground">{description}</p>}
      <div className="divide-y rounded-lg border bg-card">{children}</div>
    </section>
  );
}

/** A titled block whose content draws its own container. */
function Group({ id, title, children }: { id: string; title: string; children: ReactNode }) {
  return <section id={id} className="mb-5 scroll-mt-2"><h2 className="mb-2 text-sm font-medium">{title}</h2>{children}</section>;
}

/** Panels of one section behind tabs; the open tab lives in the UI store so deep links and search can select it. */
function Tabbed({ section, tabs }: { section: string; tabs: { id: string; label: string; panel: ReactNode }[] }) {
  const requested = useUI((s) => s.settingsTab);
  const open = useUI((s) => s.openSettings);
  const value = tabs.find((t) => t.id === requested)?.id ?? tabs[0]!.id;
  return (
    <Tabs value={value} onValueChange={(v) => open(section, String(v))}>
      <TabsList variant="underline">{tabs.map((t) => <TabsTab key={t.id} value={t.id}>{t.label}</TabsTab>)}</TabsList>
      {tabs.map((t) => <TabsPanel key={t.id} value={t.id} className="pt-3">{t.panel}</TabsPanel>)}
    </Tabs>
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

export function ThemePicker() {
  const { theme } = useSettings((s) => s.appearance);
  const set = useSettings((s) => s.set);
  return (
    <div role="radiogroup" aria-label="Theme" className="flex flex-wrap gap-2 p-3">
      {BUILTIN_THEMES.map((t) => (
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

function Customization() {
  const { density } = useSettings((s) => s.appearance);
  const a = useSettings((s) => s.ascii);
  const access = useSettings((s) => s.accessibility);
  const t = useSettings((s) => s.terminal);
  const set = useSettings((s) => s.set);
  const patch = (p: Partial<typeof a>) => set("ascii", p);
  const num = (v: number | readonly number[]) => (Array.isArray(v) ? (v[0] as number) : (v as number));
  return (
    <>
      <SettingsSection id="settings-appearance" title="Theme"><ThemePicker /><CustomTheme /></SettingsSection>
      <SettingsSection title="Density" description="Compact is the default; comfortable adds spacing.">
        <Field label="Compact density"><Switch checked={density === "compact"} onCheckedChange={(c) => set("appearance", { density: c ? "compact" : "comfortable" })} aria-label="Compact density" /></Field>
      </SettingsSection>
      <SettingsSection id="settings-ascii" title="Background" description="ASCII scenes for Home.">
        <Field label="Show background"><Switch checked={a.enabled} onCheckedChange={(c) => patch({ enabled: c })} aria-label="Show background" /></Field>
        <Field label="Random scene on startup" hint="Applies next time you open the app. Choosing a scene turns this off.">
          <Switch checked={a.randomOnStartup} onCheckedChange={(c) => patch({ randomOnStartup: c })} aria-label="Random scene on startup" />
        </Field>
        <ScenePicker />
        <Field label={`Opacity: ${Math.round(a.opacity * 100)}%`}><Slider aria-label="Opacity" className="w-48" min={0.1} max={1} step={0.05} value={[a.opacity]} onValueChange={(v) => patch({ opacity: num(v) })} /></Field>
      </SettingsSection>
      <SettingsSection id="settings-motion" title="Motion" description="Windows “Show animations” and prefers-reduced-motion are honoured by default.">
        <Field label="Reduce motion">
          <select aria-label="Reduce motion" value={access.reduceMotion} onChange={(e) => set("accessibility", { reduceMotion: e.target.value as typeof access.reduceMotion })} className="rounded-md border bg-transparent px-2 py-1">
            <option value="system">Follow system</option><option value="on">Always reduce</option><option value="off">Never reduce</option>
          </select>
        </Field>
        <Field label="Respect system setting for background"><Switch checked={a.respectReducedMotion} onCheckedChange={(c) => patch({ respectReducedMotion: c })} aria-label="Respect system reduced motion" /></Field>
        <Field label="Pause animation" hint="Shows a still frame."><Switch checked={a.paused} onCheckedChange={(c) => patch({ paused: c })} aria-label="Pause animation" /></Field>
        <Field label="Static only" hint="Never animate, regardless of other settings."><Switch checked={a.staticOnly} onCheckedChange={(c) => patch({ staticOnly: c })} aria-label="Static only" /></Field>
        <Field label={`Frame rate: ${a.fps} fps`} htmlFor="ascii-fps"><Slider id="ascii-fps" aria-label="Frame rate" className="w-48" min={1} max={30} step={1} value={[a.fps]} onValueChange={(v) => patch({ fps: num(v) })} /></Field>
        <Field label={`Intensity: ${Math.round(a.intensity * 100)}%`} hint="Scales frame rate and brightness together."><Slider aria-label="Intensity" className="w-48" min={0} max={1} step={0.05} value={[a.intensity]} onValueChange={(v) => patch({ intensity: num(v) })} /></Field>
        <Field label="Pause when window is unfocused" hint="Saves CPU and GPU in the background."><Switch checked={a.pauseWhenUnfocused} onCheckedChange={(c) => patch({ pauseWhenUnfocused: c })} aria-label="Pause when unfocused" /></Field>
      </SettingsSection>
      <Group id="settings-sound" title="Sounds"><SoundSettings /></Group>
      <SettingsSection id="settings-terminal" title="Terminal">
        <Field label={`Font size: ${t.fontSize}px`}><Slider aria-label="Terminal font size" className="w-48" min={9} max={22} step={1} value={[t.fontSize]} onValueChange={(v) => set("terminal", { fontSize: num(v) })} /></Field>
      </SettingsSection>
    </>
  );
}

function System() {
  return (
    <>
      <Group id="settings-updates" title="Updates"><UpdateSettings /></Group>
      <div id="settings-diagnostics" className="scroll-mt-2"><Diagnostics /><AutostartPanel /></div>
    </>
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
  const tab = useUI((state) => state.settingsTab);
  const sec = SECTIONS.find((section) => section.id === requested) ?? SECTIONS[0]!;
  const id = sec.id;
  const setId = useUI((state) => state.openSettings);
  useEffect(() => { if (tab) document.getElementById(`settings-${tab}`)?.scrollIntoView({ block: "start" }); }, [id, tab]);
  return (
    <div className="flex h-full min-h-0 gap-4 p-4">
      <nav aria-label="Settings sections" className="w-52 shrink-0 border-r pr-2">
        {SECTIONS.map((s) => (
          <button key={s.id} aria-current={id === s.id ? "page" : undefined} onClick={() => setId(s.id)}
            className={cn("flex h-7 w-full items-center gap-2 rounded-md px-2 text-left text-muted-foreground hover:bg-accent hover:text-foreground", id === s.id && "bg-accent text-foreground")}>
            <s.icon className="size-3.5 shrink-0" /><span className="truncate">{s.label}</span>
          </button>
        ))}
      </nav>
      <div className="min-h-0 flex-1 overflow-auto pr-1">
        <h1 className="mb-3 text-base font-medium">{sec.label}</h1>
        {id === "customization" && <Customization />}
        {id === "workspaces" && <WorkspaceSettings />}
        {id === "integrations" && <Tabbed section={id} tabs={[
          { id: "github", label: "GitHub", panel: <GitHubSettings /> },
          { id: "plugins", label: "Plugins", panel: <PluginSettings /> },
        ]} />}
        {id === "notifications" && <Tabbed section={id} tabs={[
          { id: "notifications", label: "Alerts", panel: <NotificationSettings /> },
          { id: "automations", label: "Automation limits", panel: <AutomationSettings /> },
        ]} />}
        {id === "system" && <System />}
      </div>
    </div>
  );
}
