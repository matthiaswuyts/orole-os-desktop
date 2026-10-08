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

const preferenceColumns = "grid grid-cols-[repeat(3,minmax(0,1fr))_4rem] items-center gap-x-3 gap-y-2 sm:grid-cols-[minmax(0,1fr)_4rem_4rem_4rem_4rem]";

function PreferenceHeadings({ label }: { label: string }) {
  return <div aria-hidden="true" className={`${preferenceColumns} hidden text-xs text-muted-foreground sm:grid`}>
    <span>{label}</span><span className="text-center">Allow</span><span className="text-center">Windows</span><span className="text-center">Sound</span>
  </div>;
}

function PreferenceRow({ label, pref, disabled, inherited, onChange, onReset }: {
  label: string; pref: SourcePref; disabled: boolean; inherited?: boolean;
  onChange: (pref: SourcePref) => void; onReset?: () => void;
}) {
  return (
    <Fieldset disabled={disabled} className={`${preferenceColumns} border-b py-3 last:border-b-0`}>
      <div className="col-span-4 min-w-0 sm:col-span-1">
        <FieldsetLegend className="break-words text-sm font-medium">{label}</FieldsetLegend>
        {inherited !== undefined && <p className="text-xs text-muted-foreground">{inherited ? "Default" : "Custom"}</p>}
      </div>
      <Field className="items-center"><FieldLabel className="flex-col gap-1"><span aria-hidden="true" className="text-xs sm:sr-only">Allow</span><span className="sr-only">{label}: enable notifications</span><Switch checked={pref.enabled} disabled={disabled} onCheckedChange={(enabled) => onChange({ ...pref, enabled })} /></FieldLabel></Field>
      <Field className="items-center" disabled={!pref.enabled || disabled}><FieldLabel className="flex-col gap-1"><span aria-hidden="true" className="text-xs sm:sr-only">Windows</span><span className="sr-only">{label}: Windows notifications</span><Switch checked={pref.native} disabled={!pref.enabled || disabled} onCheckedChange={(native) => onChange({ ...pref, native })} /></FieldLabel></Field>
      <Field className="items-center" disabled={!pref.enabled || disabled}><FieldLabel className="flex-col gap-1"><span aria-hidden="true" className="text-xs sm:sr-only">Sound</span><span className="sr-only">{label}: notification sound</span><Switch checked={pref.sound} disabled={!pref.enabled || disabled} onCheckedChange={(sound) => onChange({ ...pref, sound })} /></FieldLabel></Field>
      {onReset && !inherited && <Button size="xs" variant="ghost" disabled={disabled} aria-label={`${label}: use defaults`} onClick={onReset}>Reset</Button>}
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
      <p className="text-sm text-muted-foreground">Choose which alerts you receive. Changes save automatically.</p>
      {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Reconnect to load or save notification preferences.</AlertDescription></Alert>}
      {n.prefsError && <Alert variant="error"><AlertTitle>Preferences could not be loaded or saved</AlertTitle><AlertDescription>{n.prefsError}</AlertDescription></Alert>}
      {!offline && n.error && <Alert variant="warning"><AlertTitle>Could not refresh notification sources</AlertTitle><AlertDescription>{n.error}</AlertDescription></Alert>}
      {n.nativeError && <Alert variant="warning"><AlertTitle>Windows notifications unavailable</AlertTitle><AlertDescription>{n.nativeError} Notifications still appear in the center when enabled.</AlertDescription></Alert>}
      {n.nativeStatus && <p role="status" className="text-xs text-muted-foreground">{n.nativeStatus}</p>}
      {(n.prefsError || n.error || offline) && <Button size="sm" variant="outline" className="self-start" disabled={n.prefsLoading || n.prefsSaving} onClick={() => { void useDaemon.getState().refresh(); void n.loadPrefs(); void n.load(); }}>Retry connection</Button>}
      {n.prefsLoading && <div role="status" aria-label="Loading notification preferences"><Skeleton className="h-20 w-full" /></div>}
      {n.prefs && <>
        <section aria-label="Default notification preferences">
          <PreferenceHeadings label="Default preferences" />
          <PreferenceRow label="Default behavior" pref={n.prefs.default} disabled={offline || n.prefsSaving || n.prefsLoading} onChange={(pref) => void n.setSourcePref(null, pref)} />
        </section>
        <section aria-label="Source notification preferences">
          <h2 className="mb-2 text-sm font-medium sm:sr-only">Sources</h2>
          <PreferenceHeadings label="Sources" />
          {sources.map((source) => <PreferenceRow key={source} label={source} pref={n.prefs!.sources[source] ?? n.prefs!.default} inherited={!Object.hasOwn(n.prefs!.sources, source)} disabled={offline || n.prefsSaving || n.prefsLoading} onChange={(pref) => void n.setSourcePref(source, pref)} onReset={() => void n.setSourcePref(source, null)} />)}
          {n.loading && <p role="status" className="py-3 text-muted-foreground">Loading sources…</p>}
        </section>
      </>}
      <section aria-label="Test notification" className="flex flex-col items-start gap-2 border-t pt-4">
        <div className="flex flex-wrap items-center gap-3">
          <Button size="sm" variant="outline" disabled={offline || !n.prefs || n.prefsSaving || n.prefsLoading || n.testing} onClick={() => void n.test()}>{n.testing ? "Sending test…" : "Test notification"}</Button>
          <span className="text-xs text-muted-foreground">Uses system.test preferences.</span>
        </div>
        <p role="status" className="text-xs text-muted-foreground">{n.testing ? "Creating test notification…" : n.testStatus}</p>
        {n.testError && <Alert variant="error"><AlertTitle>Test notification failed</AlertTitle><AlertDescription>{n.testError}</AlertDescription></Alert>}
      </section>
      <details className="text-xs">
        <summary className="min-h-6 cursor-pointer rounded-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring">How alerts work</summary>
        <div className="mt-2 flex max-w-prose flex-col gap-2 text-muted-foreground">
          <p>New sources appear after their first alert and use the defaults until you customize them. Reset restores a source to the defaults.</p>
          <p>Allow adds new alerts to the notification center. Turning it off keeps existing history. Windows also shows a desktop notification; enable it for system.test to test delivery.</p>
          <p>Windows controls desktop notification volume, permissions, and Do not disturb / Focus Assist. In-app sounds use Interface sound volume. Sound changes apply only to new alerts; reading, dismissing, and loading history stay silent.</p>
        </div>
      </details>
      <span role="status" className="text-xs text-muted-foreground">{n.prefsSaving ? "Saving preferences…" : ""}</span>
    </div>
  );
}
