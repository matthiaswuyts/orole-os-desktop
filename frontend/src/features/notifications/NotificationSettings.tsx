import { useEffect } from "react";
import type { SourcePref } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useNotifications } from "@/stores/notifications";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";

/** The three stored switches collapse into one choice: where an alert shows up and whether it makes a sound. */
const LEVELS = [
  { id: "off", label: "Off", pref: { enabled: false, native: false, sound: false } },
  { id: "app", label: "In app", pref: { enabled: true, native: false, sound: false } },
  { id: "app-sound", label: "In app + sound", pref: { enabled: true, native: false, sound: true } },
  { id: "desktop", label: "Desktop popup", pref: { enabled: true, native: true, sound: false } },
  { id: "desktop-sound", label: "Desktop popup + sound", pref: { enabled: true, native: true, sound: true } },
] as const satisfies readonly { id: string; label: string; pref: SourcePref }[];

function levelOf(pref: SourcePref): (typeof LEVELS)[number]["id"] {
  if (!pref.enabled) return "off";
  if (pref.native) return pref.sound ? "desktop-sound" : "desktop";
  return pref.sound ? "app-sound" : "app";
}

const ITEMS = LEVELS.map((l) => ({ label: l.label, value: l.id }));

function Choice({ pref, disabled, onChange }: { pref: SourcePref; disabled: boolean; onChange: (pref: SourcePref) => void }) {
  return (
    <div className="flex items-center justify-between gap-4 px-3 py-2.5">
      <span id="notify-all-label" className="min-w-0">
        <span className="block">Notify me</span>
        <span className="block text-xs text-muted-foreground">Applies to every alert.</span>
      </span>
      <Select value={levelOf(pref)} items={ITEMS} disabled={disabled}
        onValueChange={(id) => { const level = LEVELS.find((l) => l.id === id); if (level) onChange(level.pref); }}>
        <SelectTrigger aria-labelledby="notify-all-label" className="w-56 shrink-0"><SelectValue /></SelectTrigger>
        <SelectPopup>
          {ITEMS.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
        </SelectPopup>
      </Select>
    </div>
  );
}

export function NotificationSettings() {
  const n = useNotifications();
  const link = useDaemon((state) => state.link);
  useEffect(() => n.startSync(), [n.startSync]);
  const offline = link === "offline";

  if (!inDesktop) return <Empty><EmptyHeader><EmptyTitle>Open the desktop app</EmptyTitle><EmptyDescription>Notification preferences are saved by the Orole daemon, not in the browser preview.</EmptyDescription></EmptyHeader></Empty>;
  const disabled = offline || n.prefsSaving || n.prefsLoading;
  return (
    <div className="flex max-w-3xl flex-col gap-4">
      <p className="text-muted-foreground">Choose how alerts reach you. Changes save automatically.</p>
      {offline && <Alert variant="warning"><AlertTitle>Daemon offline</AlertTitle><AlertDescription>Reconnect to load or save notification preferences.</AlertDescription></Alert>}
      {n.prefsError && <Alert variant="error"><AlertTitle>Preferences could not be loaded or saved</AlertTitle><AlertDescription>{n.prefsError}</AlertDescription></Alert>}
      {!offline && n.error && <Alert variant="warning"><AlertTitle>Could not refresh notification sources</AlertTitle><AlertDescription>{n.error}</AlertDescription></Alert>}
      {n.nativeError && <Alert variant="warning"><AlertTitle>Windows notifications unavailable</AlertTitle><AlertDescription>{n.nativeError} Notifications still appear in the center when enabled.</AlertDescription></Alert>}
      {(n.prefsError || n.error || offline) && <Button size="sm" variant="outline" className="self-start" disabled={n.prefsLoading || n.prefsSaving} onClick={() => { void useDaemon.getState().refresh(); void n.loadPrefs(); void n.load(); }}>Retry connection</Button>}
      {n.prefsLoading && <div role="status" aria-label="Loading notification preferences"><Skeleton className="h-20 w-full" /></div>}
      {n.prefs && <div className="rounded-lg border bg-card"><Choice pref={n.prefs.default} disabled={disabled} onChange={(pref) => void n.setAllPrefs(pref)} /></div>}
      <section aria-label="Test notification" className="flex flex-col items-start gap-2">
        <div className="flex flex-wrap items-center gap-3">
          <Button size="sm" variant="outline" disabled={offline || !n.prefs || n.prefsSaving || n.prefsLoading || n.testing} onClick={() => void n.test()}>{n.testing ? "Sending test…" : "Test notification"}</Button>
          <span className="text-xs text-muted-foreground">Sends one alert using the setting above.</span>
        </div>
        <p role="status" className="text-xs text-muted-foreground">{n.testing ? "Creating test notification…" : n.testStatus}</p>
        {n.testError && <Alert variant="error"><AlertTitle>Test notification failed</AlertTitle><AlertDescription>{n.testError}</AlertDescription></Alert>}
      </section>
      <span role="status" className="text-xs text-muted-foreground">{n.prefsSaving ? "Saving preferences…" : ""}</span>
    </div>
  );
}
