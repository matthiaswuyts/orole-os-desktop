import { useEffect, useRef, useState, type ReactNode } from "react";
import { Bell, Bot, Cpu, Palette, PlugZap, Settings2, Sparkles, Upload } from "lucide-react";
import { useSettings } from "@/stores/settings";
import { BUILTIN_THEMES } from "@/themes/builtin";
import { useDaemon } from "@/stores/daemon";
import { ScenePicker } from "@/features/settings/ScenePicker";
import { Switch } from "@/components/ui/switch";
import { Slider } from "@/components/ui/slider";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Tabs, TabsList, TabsPanel, TabsTab } from "@/components/ui/tabs";
import { cn, fmtUptime } from "@/lib/utils";
import { AutostartPanel } from "@/features/settings/Autostart";
import { validateCustomTokens } from "@/themes/custom";
import { Integrations } from "@/features/integrations/Integrations";
import { NotificationSettings } from "@/features/notifications/NotificationSettings";
import { AutomationSettings } from "@/features/automations/AutomationSettings";
import { SoundSettings } from "@/features/settings/SoundSettings";
import { AgentSafetySettings } from "@/features/settings/AgentSafetySettings";
import { UpdateSettings } from "@/features/settings/UpdateSettings";
import { useUI } from "@/stores/ui";
import { SkillsView } from "@/features/skills/SkillsView";
import { AIProviders } from "@/features/settings/AIProviders";

type Section = { id: string; label: string; icon: typeof Palette };
// Search and deep links address a section by id and, optionally, a tab or an anchor inside it (see openSettings).
const SECTIONS: Section[] = [
  { id: "customization", label: "Customization", icon: Palette },
  { id: "integrations", label: "Integrations", icon: PlugZap },
  { id: "skills", label: "Skills", icon: Sparkles },
  { id: "ai", label: "AI providers", icon: Bot },
  { id: "notifications", label: "Alerts & automations", icon: Bell },
  { id: "system", label: "System", icon: Settings2 },
];

export function SettingsSection({ title, description, children, id }: { title: string; description?: string; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="mb-5 scroll-mt-2">
      <h2 className={cn("text-sm font-medium", !description && "mb-2")}>{title}</h2>
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

interface ThemeOption { label: string; value: string; colors: readonly string[] }

function ThemeSwatches({ colors }: { colors: readonly string[] }) {
  return (
    <span aria-hidden="true" className="flex shrink-0 -space-x-1">
      {colors.map((color) => <span key={color} className="size-4 rounded-full ring-1 ring-background" style={{ backgroundColor: color }} />)}
    </span>
  );
}

const MAX_THEME_BYTES = 64 * 1024;

/** Picks a built-in theme, or the imported custom one; a custom theme comes from a JSON file of validated colour tokens over Midnight. */
export function ThemePicker() {
  const { theme, customTokens } = useSettings((s) => s.appearance);
  const set = useSettings((s) => s.set);
  const file = useRef<HTMLInputElement>(null);
  const [err, setErr] = useState<string | null>(null);
  const hasCustom = Object.keys(customTokens).length > 0;
  const options: ThemeOption[] = [
    ...BUILTIN_THEMES.map((t) => ({ label: t.label, value: t.id, colors: t.sw })),
    ...(hasCustom ? [{ label: "Custom", value: "custom", colors: [customTokens.background, customTokens.card ?? customTokens.sidebar, customTokens.primary, customTokens.foreground].filter((c): c is string => !!c) }] : []),
  ];
  const selected = options.find((o) => o.value === theme) ?? options[0]!;

  async function importTheme(f: File) {
    setErr(null);
    if (f.size > MAX_THEME_BYTES) return setErr("The theme file is larger than 64 KiB.");
    let parsed: unknown;
    try { parsed = JSON.parse(await f.text()); } catch { return setErr("The file is not valid JSON."); }
    const r = validateCustomTokens(parsed);
    if (!r.ok) return setErr(r.error);
    if (Object.keys(r.tokens).length === 0) return setErr("The theme file has no colour tokens.");
    set("appearance", { theme: "custom", customTokens: r.tokens });
  }

  return (
    <div className="flex flex-col gap-2 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={selected} items={options} itemToStringValue={(o: ThemeOption) => o.value}
          onValueChange={(o: ThemeOption | null) => { if (o) set("appearance", { theme: o.value as typeof theme }); }}>
          <SelectTrigger aria-label="Theme" className="w-60">
            <SelectValue>
              {(o: ThemeOption) => <span className="flex items-center gap-2"><ThemeSwatches colors={o.colors} /><span className="truncate">{o.label}</span></span>}
            </SelectValue>
          </SelectTrigger>
          <SelectPopup>
            {options.map((o) => (
              <SelectItem key={o.value} value={o}>
                <span className="flex items-center gap-2"><ThemeSwatches colors={o.colors} />{o.label}</span>
              </SelectItem>
            ))}
          </SelectPopup>
        </Select>
        <Button size="sm" variant="outline" onClick={() => file.current?.click()}><Upload />Import theme (JSON)</Button>
        {hasCustom && <Button size="sm" variant="ghost" onClick={() => set("appearance", { theme: theme === "custom" ? "midnight" : theme, customTokens: {} })}>Remove custom theme</Button>}
        <input ref={file} type="file" accept="application/json,.json" hidden aria-label="Theme JSON file"
          onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ""; if (f) void importTheme(f); }} />
      </div>
      {err && <p role="alert" className="text-destructive-foreground">{err}</p>}
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
      <SettingsSection id="settings-appearance" title="Theme"><ThemePicker /></SettingsSection>
      <SettingsSection title="Density" description="Compact is the default; comfortable adds spacing.">
        <Field label="Compact density"><Switch checked={density === "compact"} onCheckedChange={(c) => set("appearance", { density: c ? "compact" : "comfortable" })} aria-label="Compact density" /></Field>
      </SettingsSection>
      <SettingsSection id="settings-ascii" title="Background" description="ASCII scenes or your own image, rendered in the same pixelated style.">
        <Field label="Show background"><Switch checked={a.enabled} onCheckedChange={(c) => patch({ enabled: c })} aria-label="Show background" /></Field>
        <Field label="Random scene on startup" hint="Applies next time you open the app. Enabling this replaces your uploaded background.">
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
      <SettingsSection id="settings-task-permissions" title="Task permissions" description="Manual, Auto and Yolo are chosen per task."><AgentSafetySettings /></SettingsSection>
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
  // "mcp" is the pre-merge section id; orole://settings/mcp links and the settings index still send it.
  const sec = SECTIONS.find((section) => section.id === (requested === "mcp" ? "integrations" : requested)) ?? SECTIONS[0]!;
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
      <div className={cn("min-h-0 min-w-0 flex-1", id === "skills" ? "overflow-hidden" : "overflow-auto pr-1")}>
        {id !== "skills" && <h1 className="mb-3 text-base font-medium">{sec.label}</h1>}
        {id === "customization" && <Customization />}
        {id === "skills" && <SkillsView />}
        {id === "ai" && <AIProviders />}
        {id === "integrations" && <Integrations />}
        {id === "notifications" && <Tabbed section={id} tabs={[
          { id: "notifications", label: "Alerts", panel: <NotificationSettings /> },
          { id: "automations", label: "Automation limits", panel: <AutomationSettings /> },
        ]} />}
        {id === "system" && <System />}
      </div>
    </div>
  );
}
