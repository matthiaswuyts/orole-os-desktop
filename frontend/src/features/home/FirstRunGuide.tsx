import { useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { SCENES } from "@/components/ascii/scenes";
import { Settings } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useSettings } from "@/stores/settings";
import type { ThemeId } from "@/stores/settings";
import { BUILTIN_THEMES } from "@/themes/builtin";
import { useUI } from "@/stores/ui";
import type { ViewId } from "@/stores/ui";

const go = (view: ViewId) => () => useUI.getState().setView(view);
const steps: { title: string; description: string; open?: () => void; action?: string }[] = [
  { title: "Welcome to Orole OS", description: "Set up your workspace, then connect only what you want. No Orole account is required." },
  { title: "Make this workspace yours", description: "Choose a theme and scene. You can change motion, density and contrast in Settings." },
  { title: "Check your local daemon", description: "The daemon keeps supported tasks running when the desktop closes. Terminal processes end when the daemon exits or Windows restarts." },
  { title: "Connect GitHub", description: "Use your existing GitHub CLI sign-in so tasks can push branches and open pull requests.", open: () => useUI.getState().openSettings("integrations", "github"), action: "Open GitHub setup" },
  { title: "Add skills", description: "Configure skills once; every harness and model can use the ones you enable.", open: go("skills"), action: "Open Skills" },
  { title: "Add MCP servers", description: "Configure MCP servers once; every harness receives them through Orole's gateway, and Orole decides what each tool call may do.", open: go("mcp"), action: "Open MCP" },
  { title: "Start a task", description: "Add a project, then start a task on it from the sidebar. A task runs an installed harness on its own branch; a worktree separates edits but is not a security sandbox.", open: go("agents"), action: "Open Tasks" },
  { title: "Your workspace is ready to explore", description: "Customize the widgets below or start a task from a project in the sidebar. Skipped steps remain available from navigation; finishing this guide does not mark anything connected." },
];
type Progress = { step: number; dismissed: boolean };
const initial: Progress = { step: 0, dismissed: false };
const key = "onboarding";

export function FirstRunGuide() {
  const [progress, setProgress] = useState<Progress | null>(inDesktop ? null : initial);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const link = useDaemon((s) => s.link);
  const daemonError = useDaemon((s) => s.error);
  const appearance = useSettings((s) => s.appearance);
  const ascii = useSettings((s) => s.ascii);
  const settingError = useSettings((s) => s.error);
  const set = useSettings((s) => s.set);
  useEffect(() => {
    if (!inDesktop || link !== "connected") return;
    let live = true;
    Settings.all().then((all) => {
      if (!live) return;
      const value = all[key] as Partial<Progress> | undefined;
      setProgress({ step: Number.isInteger(value?.step) && value!.step! >= 0 && value!.step! < steps.length ? value!.step! : 0, dismissed: value?.dismissed === true });
      setError("");
    }, (e: unknown) => { if (live) setError(String(e)); });
    return () => { live = false; };
  }, [link]);
  async function save(next: Progress, open?: () => void) {
    setSaving(true);
    try {
      if (inDesktop) await Settings.set(key, next);
      setProgress(next);
      setError("");
      open?.();
    } catch (e) { setError(String(e)); }
    finally { setSaving(false); }
  }
  if (!progress) return error ? <p role="alert">Setup progress could not load: {error}</p> : null;
  if (progress.dismissed) return <div className="mb-3 flex justify-end"><Button size="xs" variant="ghost" onClick={() => void save({ step: 0, dismissed: false })} disabled={saving}>Reopen setup guide</Button>{error && <p role="alert">{error}</p>}</div>;
  const current = steps[progress.step];
  const next = Math.min(progress.step + 1, steps.length - 1);
  return <section aria-labelledby="first-run-title" className="mb-5 rounded-lg border bg-card p-4">
    <div className="flex flex-wrap items-start justify-between gap-3">
      <div className="flex max-w-prose flex-col gap-2">
        <h2 id="first-run-title" className="font-medium">{current.title}</h2>
        <p className="text-muted-foreground">{current.description}</p>
      </div>
      <Button size="xs" variant="ghost" disabled={saving} onClick={() => void save({ ...progress, dismissed: true })}>I'll explore on my own</Button>
    </div>
    {progress.step === 1 && <div className="my-4 flex flex-wrap gap-4">
      <label className="flex flex-col gap-1">Theme<select className="rounded-md border bg-background px-2 py-1" value={appearance.theme} onChange={(e) => set("appearance", { theme: e.target.value as ThemeId })}>
        {BUILTIN_THEMES.map((theme) => <option key={theme.id} value={theme.id}>{theme.label}</option>)}{appearance.theme === "custom" && <option value="custom">Custom theme</option>}
      </select></label>
      <label className="flex flex-col gap-1">Background scene<select className="rounded-md border bg-background px-2 py-1" value={ascii.scene} onChange={(e) => set("ascii", { scene: e.target.value })}>{SCENES.map((scene) => <option key={scene.id} value={scene.id}>{scene.label}</option>)}</select></label>
      <label className="flex items-center gap-2"><input type="checkbox" checked={ascii.staticOnly} onChange={(e) => set("ascii", { staticOnly: e.target.checked })} />Use a still background</label>
    </div>}
    {progress.step === 2 && <div className="my-3 flex flex-wrap items-center gap-3"><p role="status">{link === "connected" ? "Local daemon connected." : link === "preview" ? "Browser preview: open the desktop app to connect to the daemon." : `Daemon ${link}. ${daemonError ?? ""}`}</p><Button size="xs" variant="outline" disabled={!inDesktop} onClick={() => void useDaemon.getState().refresh()}>Check again</Button></div>}
    {(error || settingError) && <p role="alert" className="my-2 text-destructive-foreground">{error || settingError}</p>}
    <div className="mt-4 flex flex-wrap items-center gap-2">
      <span className="mr-auto text-xs text-muted-foreground">Step {progress.step + 1} of {steps.length}{!inDesktop && " · preview progress is not saved"}</span>
      {progress.step > 0 && <Button size="xs" variant="ghost" disabled={saving} onClick={() => void save({ step: progress.step - 1, dismissed: false })}>Back</Button>}
      {current.open && <Button size="xs" variant="outline" disabled={saving} onClick={() => void save({ step: next, dismissed: false }, current.open)}>{current.action}</Button>}
      <Button size="xs" disabled={saving} onClick={() => void save({ step: next, dismissed: progress.step === steps.length - 1 })}>{progress.step === steps.length - 1 ? "Open dashboard" : current.open ? "Skip for now" : "Continue"}</Button>
    </div>
  </section>;
}
