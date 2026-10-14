import { MicIcon, PhoneCallIcon, PhoneIcon, Trash2Icon } from "lucide-react";
import { useCallback, useEffect, useId, useState } from "react";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { errText } from "@/features/agents/format";
import { LIMITS, validate, type FormState } from "@/features/chat/builder/botForm";
import { ControlRow, Group, SwitchRow } from "@/features/chat/panel/rows";
import type { AgentsSetting } from "@/features/settings/AgentSafetySettings";
import { inDesktop } from "@/lib/backend";
import { Jarvis, Settings, type Bot, type CallRecord, type JarvisRule, type TTSModel, type Voice } from "@/mygo";
import { useUI } from "@/stores/ui";
import { CallCard, callPart } from "./CallCard";
import { describeRule } from "./describe";
import { jarvisGate } from "./gate";
import { phoneReady, useJarvis } from "./store";

const RECENT_CALLS = 5;
const DEFAULT_ITEM = { value: "", label: "Default" };

function Note({ children, alert }: { children: React.ReactNode; alert?: boolean }) {
  return <p role={alert ? "alert" : undefined} className="selectable break-words px-1 text-muted-foreground text-xs">{children}</p>;
}

/** A compact select for a row: the first item ("Default") means "use the global choice". */
function RowSelect({ id, value, items, onChange }: { id: string; value: string; items: { value: string; label: string }[]; onChange: (value: string) => void }) {
  const shown = items.some((item) => item.value === value) ? items : [...items, { value, label: value }];
  return <Select value={value} items={shown} onValueChange={(next: string | null) => onChange(next ?? "")}>
    <SelectTrigger id={id} size="sm" className="w-auto min-w-24 max-w-52 border-0 bg-transparent shadow-none"><SelectValue /></SelectTrigger>
    <SelectPopup alignItemWithTrigger={false} className="max-h-80">
      {shown.map((item) => <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>)}
    </SelectPopup>
  </Select>;
}

/**
 * A bot's phone calls: Jarvis mode (the bot may phone the owner and take their calls), when to call, the voice, the
 * call rules the bot has set up for itself, and recent calls. Needs phone setup first and, to switch on, a bot with
 * full permissions: calls have no approval prompt. The gate shown here only mirrors the daemon's; the daemon checks again on save.
 */
export function JarvisPage({ bot, form, patch, onOpenPermissions }: { bot: Bot; form: FormState; patch: (p: Partial<FormState>) => void; onOpenPermissions?: () => void }) {
  const status = useJarvis((s) => s.status);
  const load = useJarvis((s) => s.load);
  // Setup done once (the number is connected): the page stays usable while the relay or a key is down, so Jarvis can
  // always be switched off and rules managed. Only a test call needs everything working right now.
  const setUp = !!status?.settings.agentId;
  const ready = phoneReady(status);
  const hasElevenLabs = !!status?.hasElevenLabs;
  const ids = { policy: useId(), voice: useId(), model: useId() };

  const [agents, setAgents] = useState<AgentsSetting | null>(null);
  const [rules, setRules] = useState<JarvisRule[] | null>(null);
  const [calls, setCalls] = useState<CallRecord[]>([]);
  const [voices, setVoices] = useState<Voice[]>([]);
  const [models, setModels] = useState<TTSModel[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);

  useEffect(() => { if (!status) void load(); }, [status, load]);

  useEffect(() => {
    if (!inDesktop) return;
    let live = true;
    Settings.all().then((all) => { if (live) setAgents((all.agents as AgentsSetting | undefined) ?? {}); }, () => { if (live) setAgents({}); });
    return () => { live = false; };
  }, []);

  const refresh = useCallback(async () => {
    if (!inDesktop) return;
    try {
      const [nextRules, nextCalls] = await Promise.all([Jarvis.rules(bot.id), Jarvis.recentCalls(bot.id)]);
      setRules(nextRules);
      setCalls(nextCalls.slice(0, RECENT_CALLS));
    } catch (failure) {
      setError(errText(failure));
    }
  }, [bot.id]);

  useEffect(() => { if (setUp) void refresh(); }, [setUp, refresh]);

  useEffect(() => {
    if (!inDesktop || !setUp || !hasElevenLabs) return;
    let live = true;
    // The lists come from ElevenLabs; without them the selects keep "Default" and any stored choice.
    Promise.all([Jarvis.voices(), Jarvis.models()]).then(([v, m]) => { if (live) { setVoices(v); setModels(m); } }, () => {});
    return () => { live = false; };
  }, [setUp, hasElevenLabs]);

  if (!setUp) {
    return <div className="flex flex-col items-start gap-2 px-1">
      <Note>Phone calls aren't set up yet.</Note>
      <Button type="button" size="sm" onClick={() => useUI.getState().openSettings("phone")}>Set up phone calls</Button>
    </div>;
  }
  if (agents === null) return <Note>Loading…</Note>;

  const reason = jarvisGate(form, agents);
  // Switching off always works; only switching on needs full permissions.
  const locked = !form.jarvisEnabled && reason !== "";
  const policyProblem = validate(form).jarvisPolicy;

  async function deleteRule(rule: JarvisRule) {
    setError(null);
    try {
      await Jarvis.deleteRule(rule.id);
      setRules((current) => current?.filter((entry) => entry.id !== rule.id) ?? null);
    } catch (failure) {
      setError(errText(failure));
    }
  }

  async function testCall() {
    setTesting(true);
    setError(null);
    setTestResult(null);
    try {
      setTestResult(await Jarvis.testCall(bot.id));
      void refresh();
    } catch (failure) {
      setError(errText(failure));
    } finally {
      setTesting(false);
    }
  }

  const voiceItems = [DEFAULT_ITEM, ...voices.map((voice) => ({ value: voice.id, label: voice.name }))];
  const modelItems = [DEFAULT_ITEM, ...models.map((model) => ({ value: model.id, label: `${model.name} · ${model.latency}` }))];

  return <>
    <Group title="Jarvis">
      <SwitchRow icon={PhoneIcon} label="Jarvis mode" hint={`Lets ${bot.name} phone you and take your calls.`} checked={form.jarvisEnabled} disabled={locked} onChange={(jarvisEnabled) => patch({ jarvisEnabled })} />
    </Group>
    {!ready && <div className="flex flex-col items-start gap-1.5 px-1">
      <Note>Phone calls are unavailable right now: {status?.relayConnected ? "a setup step needs attention" : "the relay is not connected"}. Rules and this switch still work.</Note>
      <Button type="button" variant="outline" size="xs" onClick={() => useUI.getState().openSettings("phone")}>Open phone settings</Button>
    </div>}
    {locked && <div className="flex flex-col items-start gap-1.5 px-1">
      <Note>{reason}</Note>
      {onOpenPermissions && <Button type="button" variant="outline" size="xs" onClick={onOpenPermissions}>Open permissions</Button>}
    </div>}

    <Group title="Policy" aside={`${Array.from(form.jarvisPolicy).length}/${LIMITS.jarvisPolicy}`}>
      <div className="p-2">
        <Textarea id={ids.policy} aria-label="When to call me" aria-invalid={!!policyProblem || undefined} rows={3} value={form.jarvisPolicy}
          placeholder="Only call for failures or when you need a decision. Never after 22:00." onChange={(e) => patch({ jarvisPolicy: e.target.value })} />
      </div>
    </Group>
    {policyProblem && <Note alert>{policyProblem}</Note>}

    <Group title="Voice">
      <ControlRow icon={MicIcon} label="Voice" htmlFor={ids.voice}>
        <RowSelect id={ids.voice} value={form.jarvisVoice} items={voiceItems} onChange={(jarvisVoice) => patch({ jarvisVoice })} />
      </ControlRow>
      <ControlRow icon={MicIcon} label="Voice model" htmlFor={ids.model}>
        <RowSelect id={ids.model} value={form.jarvisModel} items={modelItems} onChange={(jarvisModel) => patch({ jarvisModel })} />
      </ControlRow>
    </Group>

    <Group title="Rules">
      {rules === null ? <p className="px-3 py-2 text-muted-foreground text-xs">Loading…</p>
        : rules.length === 0 ? <p className="px-3 py-2 text-muted-foreground text-xs">No rules yet. Tell {bot.name} in chat, e.g. "call me when the deploy finishes".</p>
          : rules.map((rule) => {
            const text = describeRule(rule);
            return <div key={rule.id} className="flex min-h-10 items-center gap-2 px-3 py-1.5 text-sm">
              <span className="min-w-0 flex-1 break-words">{text}</span>
              <Tooltip>
                <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label={`Delete rule: ${text}`} onClick={() => void deleteRule(rule)} />}>
                  <Trash2Icon aria-hidden />
                </TooltipTrigger>
                <TooltipPopup>Delete</TooltipPopup>
              </Tooltip>
            </div>;
          })}
    </Group>

    <Group title="Recent calls">
      {calls.length === 0 ? <p className="px-3 py-2 text-muted-foreground text-xs">No calls yet.</p>
        : <div className="flex flex-col gap-1.5 p-2">{calls.map((call) => <CallCard key={call.id} part={callPart(call)} />)}</div>}
      <div className="flex items-center gap-2 px-3 py-2">
        <Button type="button" variant="outline" size="sm" disabled={!form.jarvisEnabled || !ready || testing} onClick={() => void testCall()}><PhoneCallIcon aria-hidden />Test call</Button>
        {testResult && <span className="selectable min-w-0 break-words text-muted-foreground text-xs">{testResult}</span>}
      </div>
    </Group>
    {error && <Note alert>{error}</Note>}
  </>;
}
