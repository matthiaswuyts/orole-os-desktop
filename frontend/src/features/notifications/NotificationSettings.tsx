import { useEffect } from "react";
import type { SourcePref } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useNotifications } from "@/stores/notifications";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldLabel } from "@/components/ui/field";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";

function PreferenceRow({ label, pref, disabled, inherited, onChange, onReset }: {
  label: string; pref: SourcePref; disabled: boolean; inherited?: boolean;
  onChange: (pref: SourcePref) => void; onReset?: () => void;
}) {
  return (
    <Fieldset disabled={disabled} className="flex flex-wrap items-center gap-x-6 gap-y-3 border-b py-3 last:border-b-0">
      <div className="min-w-36 flex-1"><FieldsetLegend className="break-words">{label}</FieldsetLegend>{inherited !== undefined && <p className="text-xs text-muted-foreground">{inherited ? "Uses defaults" : "Custom preferences"}</p>}</div>
      <Field className="flex-row items-center"><FieldLabel>Enabled<Switch checked={pref.enabled} onCheckedChange={(enabled) => onChange({ ...pref, enabled })} aria-label={`${label}: enable notifications`} /></FieldLabel></Field>
      <Field className="flex-row items-center" disabled={!pref.enabled}><FieldLabel>Windows<Switch checked={pref.native} disabled={!pref.enabled || disabled} onCheckedChange={(native) => onChange({ ...pref, native })} aria-label={`${label}: Windows notifications`} /></FieldLabel></Field>
      <Field className="flex-row items-center" disabled={!pref.enabled}><FieldLabel>Sound<Switch checked={pref.sound} disabled={!pref.enabled || disabled} onCheckedChange={(sound) => onChange({ ...pref, sound })} aria-label={`${label}: notification sound`} /></FieldLabel></Field>
      {onReset && <Button size="xs" variant="ghost" disabled={disabled || inherited} onClick={onReset}>Use defaults</Button>}
    </Fieldset>
  );
}

export function NotificationSettings() {
  const n = useNotifications();
  const link = useDaemon((state) => state.link);
  useEffect(() => n.startSync(), [n.startSync]);
  const offline = link === "offline";
  const sources = Array.from(new Set(["system.test", ...n.sources.map((source) => source.source), ...Object.keys(n.prefs?.sources ?? {})])).sort();
  if (!inDesktop) return <Empty><EmptyHeader><EmptyTitle>Open the desktop app</EmptyTitle><EmptyDescription>Notification preferences are saved by the Orole daemon, not in the browser preview.</EmptyDescription></EmptyHeader></Empty>;
  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <p className="text-muted-foreground">Choose which sources can add to your notification center and show Windows notifications. Changes save immediately. Disabling a source prevents new notifications; existing history is kept.</p>
      <p className="text-xs text-muted-foreground">Native notification volume follows Windows, including notification permissions and Do not disturb / Focus Assist. In-app notifications use the separate web-kit UI sound volume. Sound changes affect only new notifications; reading, dismissing, and loading history stay silent.</p>
      {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Reconnect to load or save notification preferences.</AlertDescription></Alert>}
      {n.prefsError && <Alert variant="error"><AlertTitle>Preferences could not be loaded or saved</AlertTitle><AlertDescription>{n.prefsError}</AlertDescription></Alert>}
      {!offline && n.error && <Alert variant="warning"><AlertTitle>Could not refresh notification sources</AlertTitle><AlertDescription>{n.error}</AlertDescription></Alert>}
      {n.nativeError && <Alert variant="warning"><AlertTitle>Windows notifications unavailable</AlertTitle><AlertDescription>{n.nativeError} Notifications still appear in the center when enabled.</AlertDescription></Alert>}
      {n.nativeStatus && <p role="status" className="text-xs text-muted-foreground">{n.nativeStatus}</p>}
      {(n.prefsError || n.error || offline) && <Button size="sm" variant="outline" className="self-start" disabled={n.prefsLoading || n.prefsSaving} onClick={() => { void useDaemon.getState().refresh(); void n.loadPrefs(); void n.load(); }}>Retry connection</Button>}
      {n.prefsLoading && <div role="status" aria-label="Loading notification preferences"><Skeleton className="h-20 w-full" /></div>}
      {n.prefs && <>
        <section aria-label="Default notification preferences">
          <PreferenceRow label="Defaults for new sources" pref={n.prefs.default} disabled={offline || n.prefsSaving || n.prefsLoading} onChange={(pref) => void n.setSourcePref(null, pref)} />
          <p className="text-xs text-muted-foreground">Sources without custom preferences follow these defaults. Windows delivery also depends on system notification permissions.</p>
        </section>
        <section aria-label="Source notification preferences">
          <h2 className="text-sm font-medium">Sources</h2>
          {sources.map((source) => <PreferenceRow key={source} label={source} pref={n.prefs!.sources[source] ?? n.prefs!.default} inherited={!Object.hasOwn(n.prefs!.sources, source)} disabled={offline || n.prefsSaving || n.prefsLoading} onChange={(pref) => void n.setSourcePref(source, pref)} onReset={() => void n.setSourcePref(source, null)} />)}
          {n.loading && <p role="status" className="py-3 text-muted-foreground">Loading sources…</p>}
          <p className="py-3 text-xs text-muted-foreground">Other sources appear after sending their first notification. Your defaults apply until then.</p>
        </section>
      </>}
      <section aria-label="Test notification" className="flex flex-col items-start gap-2">
        <Button size="sm" variant="outline" disabled={offline || !n.prefs || n.prefsSaving || n.prefsLoading || n.testing} onClick={() => void n.test()}>{n.testing ? "Sending test…" : "Test notification"}</Button>
        <p className="text-xs text-muted-foreground">Creates a real system.test notification using its saved preferences above. Enable Windows for this source to test a native notification.</p>
        <p role="status" className="text-xs text-muted-foreground">{n.testing ? "Creating test notification…" : n.testStatus}</p>
        {n.testError && <Alert variant="error"><AlertTitle>Test notification failed</AlertTitle><AlertDescription>{n.testError}</AlertDescription></Alert>}
      </section>
      <span role="status" className="text-xs text-muted-foreground">{n.prefsSaving ? "Saving preferences…" : ""}</span>
    </div>
  );
}
