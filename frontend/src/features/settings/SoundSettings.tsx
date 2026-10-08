import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Slider, SliderPrimitive } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { playSound, useAudioStatus } from "@/lib/audio";
import { useSettings } from "@/stores/settings";

export function SoundSettings() {
  const sound = useSettings((state) => state.sound);
  const loaded = useSettings((state) => state.loaded);
  const saveError = useSettings((state) => state.error);
  const set = useSettings((state) => state.set);
  const audioError = useAudioStatus((state) => state.error);
  const [testStatus, setTestStatus] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  return (
    <section className="space-y-4" aria-label="Sound preferences">
      <p className="max-w-prose text-muted-foreground">Brief synthesized cues for buttons, switches, and in-app notifications. Sounds play only in the focused window, after you interact with it. No hover or typing sounds.</p>
      <div className="divide-y rounded-lg border bg-card">
        <div className="flex items-center justify-between gap-4 px-3 py-2.5">
          <label htmlFor="ui-sounds">Interface sounds</label>
          <Switch id="ui-sounds" checked={sound.uiEnabled} disabled={!loaded} onCheckedChange={(uiEnabled) => set("sound", { uiEnabled })} />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-4 px-3 py-2.5">
          <div><span>Web sound volume</span><p className="text-xs text-muted-foreground">Includes in-app notification cues. Zero mutes all web sounds.</p></div>
          <div className="flex items-center gap-3"><Slider className="w-44" min={0} max={1} step={0.05} value={[sound.volume]} disabled={!loaded} onValueChange={(value) => set("sound", { volume: Array.isArray(value) ? value[0]! : value })}><SliderPrimitive.Label className="sr-only">Web sound volume</SliderPrimitive.Label></Slider><output className="w-9 text-right tabular-nums">{Math.round(sound.volume * 100)}%</output></div>
        </div>
      </div>
      <div className="flex items-center gap-3">
        <Button variant="outline" size="sm" data-ui-sound="none" disabled={!loaded || !sound.uiEnabled || sound.volume === 0 || testing} onClick={async () => {
          setTesting(true);
          try { setTestStatus(await playSound("success") ? "Cue sent to the audio output. Audibility depends on your device volume." : "No cue played. Keep this window focused and try again."); }
          finally { setTesting(false); }
        }}>Test interface sound</Button>
        <span role="status" className="text-xs text-muted-foreground">{testStatus}</span>
      </div>
      {audioError && <p role="alert" className="text-destructive-foreground">{audioError}</p>}
      {saveError && <p role="alert" className="text-destructive-foreground">Settings could not be saved: {saveError}</p>}
      <p className="max-w-prose text-xs text-muted-foreground">Notification source sound and Windows delivery switches are in Notifications. Native notifications use Windows sounds and volume, not this slider. Windows permissions and Do not disturb / Focus Assist may suppress them.</p>
    </section>
  );
}
