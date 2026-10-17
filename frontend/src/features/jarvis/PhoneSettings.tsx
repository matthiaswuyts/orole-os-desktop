import { useEffect, useId, useState, type ReactNode } from "react";
import { CopyIcon, XIcon } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { Jarvis, type PhoneSettings as PhoneConfig, type StatusView, type TTSModel } from "@/mygo";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Stepper } from "@/components/ui/stepper";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { errText } from "@/features/agents/format";
import { Failure, Loading } from "@/features/mcp/shared";
import { SettingsSection } from "@/features/settings/SettingsSection";
import { inDesktop } from "@/lib/backend";
import { useJarvis } from "./store";
import { LanguagePicker, LanguageVoicePicker, ModelPicker } from "./VoicePicker";

/** One labelled control row. */
function Row({ label, htmlFor, hint, children }: { label: string; htmlFor?: string; hint?: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2 px-3 py-2.5">
      <div className="min-w-0 flex-1">
        <label htmlFor={htmlFor} className="block">{label}</label>
        {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
      </div>
      <div className="flex shrink-0 items-center gap-1.5">{children}</div>
    </div>
  );
}

function ErrorLine({ message }: { message: string | null | undefined }) {
  if (!message) return null;
  return <p role="alert" className="selectable break-words px-3 py-2 text-xs text-destructive-foreground">{message}</p>;
}

/** Runs one backend call with a busy flag and an error line. */
function useAction() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function run<T>(work: () => Promise<T>): Promise<T | undefined> {
    setBusy(true);
    setError(null);
    try {
      return await work();
    } catch (failure) {
      setError(errText(failure));
      return undefined;
    } finally {
      setBusy(false);
    }
  }
  return { busy, error, run };
}

function IconAction({ label, tip, onClick, children }: { label: string; tip: string; onClick: () => void; children: ReactNode }) {
  return (
    <Tooltip>
      <TooltipTrigger render={<Button size="icon-sm" variant="ghost" aria-label={label} onClick={onClick} />}>{children}</TooltipTrigger>
      <TooltipPopup>{tip}</TooltipPopup>
    </Tooltip>
  );
}

function SecretBlock({ name, value, copyLabel }: { name: string; value: string; copyLabel: string }) {
  return (
    <div className="flex items-center gap-2 px-3 py-2">
      <div className="min-w-0 flex-1">
        <div className="text-xs text-muted-foreground">{name}</div>
        <code className="selectable block break-all font-mono text-xs">{value}</code>
      </div>
      <IconAction label={copyLabel} tip="Copy" onClick={() => { void navigator.clipboard?.writeText(value).catch(() => undefined); }}>
        <CopyIcon aria-hidden />
      </IconAction>
    </div>
  );
}

function TwilioSection({ status }: { status: StatusView }) {
  const sidId = useId();
  const tokenId = useId();
  const [twilioSid, setSid] = useState("");
  const [twilioToken, setToken] = useState("");
  const action = useAction();
  async function save() {
    const next = await action.run(() => Jarvis.setSecrets({ twilioSid, twilioToken }));
    if (!next) return;
    setSid("");
    setToken("");
    useJarvis.getState().apply(next);
  }
  return (
    <SettingsSection title="Twilio" description={status.hasTwilio ? "Credentials are stored. Enter new ones to replace them." : "Your Twilio account SID and auth token. They are stored in Windows Credential Manager."}>
      <Row label="Account SID" htmlFor={sidId}>
        <Input id={sidId} size="sm" className="w-64" autoComplete="off" value={twilioSid} onChange={(event) => setSid(event.target.value)} />
      </Row>
      <Row label="Auth token" htmlFor={tokenId}>
        <Input id={tokenId} type="password" size="sm" className="w-64" autoComplete="off" value={twilioToken} onChange={(event) => setToken(event.target.value)} />
      </Row>
      <div className="flex items-center justify-end gap-2 px-3 py-2">
        <Button size="sm" loading={action.busy} disabled={!twilioSid && !twilioToken} onClick={() => void save()}>Save Twilio</Button>
      </div>
      <ErrorLine message={action.error} />
    </SettingsSection>
  );
}

function ElevenLabsSection({ status }: { status: StatusView }) {
  const keyId = useId();
  const [elevenLabsKey, setKey] = useState("");
  const action = useAction();
  async function save() {
    const next = await action.run(() => Jarvis.setSecrets({ elevenLabsKey }));
    if (!next) return;
    setKey("");
    useJarvis.getState().apply(next);
  }
  return (
    <SettingsSection title="ElevenLabs" description={status.hasElevenLabs ? "A key is stored. Enter a new one to replace it." : "Your ElevenLabs API key. It is stored in Windows Credential Manager."}>
      <Row label="API key" htmlFor={keyId}>
        <Input id={keyId} type="password" size="sm" className="w-64" autoComplete="off" value={elevenLabsKey} onChange={(event) => setKey(event.target.value)} />
        <Button size="sm" loading={action.busy} disabled={!elevenLabsKey} onClick={() => void save()}>Save key</Button>
      </Row>
      <ErrorLine message={action.error} />
    </SettingsSection>
  );
}

function RelaySection({ status, webhookSecret, onWebhookSecret }: { status: StatusView; webhookSecret: string; onWebhookSecret: (value: string) => void }) {
  const urlId = useId();
  const numberId = useId();
  const [relayUrl, setRelayUrl] = useState(status.settings.relayUrl ?? "");
  const [twilioNumber, setNumber] = useState(status.settings.twilioNumber ?? "");
  const [secrets, setSecrets] = useState<{ relayToken: string; llmSecret: string } | null>(null);
  const generate = useAction();
  const connect = useAction();

  async function generateSecrets() {
    const next = await generate.run(() => Jarvis.generateRelaySecrets());
    if (!next) return;
    setSecrets(next);
    void useJarvis.getState().load();
  }
  async function connectRelay() {
    const result = await connect.run(() => Jarvis.configure({ ...status.settings, relayUrl: relayUrl.trim(), twilioNumber: twilioNumber.trim() }));
    if (!result) return;
    useJarvis.getState().apply(result.status);
    if (result.webhookSecret) onWebhookSecret(result.webhookSecret);
  }

  return (
    <SettingsSection title="Relay" description="A small Cloudflare Worker (relay/ in the repo) that connects Twilio and ElevenLabs to this PC. Deploy it, give it its secrets, then connect it here.">
      <Row label="Relay URL" htmlFor={urlId} hint="https://your-worker.workers.dev">
        <Input id={urlId} size="sm" className="w-64" autoComplete="off" value={relayUrl} onChange={(event) => setRelayUrl(event.target.value)} />
      </Row>
      <Row label="Jarvis number" htmlFor={numberId} hint="Your Twilio number, like +3220000000">
        <Input id={numberId} size="sm" className="w-64" autoComplete="off" value={twilioNumber} onChange={(event) => setNumber(event.target.value)} />
      </Row>
      <div className="flex items-center justify-end gap-2 px-3 py-2">
        <Button size="sm" variant="outline" loading={generate.busy} onClick={() => void generateSecrets()}>Generate relay secrets</Button>
        <Button size="sm" loading={connect.busy} disabled={!relayUrl.trim() || !twilioNumber.trim()} onClick={() => void connectRelay()}>Connect</Button>
      </div>
      <ErrorLine message={generate.error} />
      <ErrorLine message={connect.error} />
      {secrets && (
        <div className="bg-muted/30">
          <div className="flex items-start justify-between gap-2 px-3 pt-2">
            <p className="text-xs text-muted-foreground">Shown once. Generating again replaces them, and the relay must be updated with the new values afterwards.</p>
            <IconAction label="Hide relay secrets" tip="Hide" onClick={() => setSecrets(null)}><XIcon aria-hidden /></IconAction>
          </div>
          <SecretBlock name="Relay token" value={secrets.relayToken} copyLabel="Copy relay token" />
          <SecretBlock name="LLM secret" value={secrets.llmSecret} copyLabel="Copy LLM secret" />
          <pre className="selectable overflow-x-auto px-3 py-2 font-mono text-xs">{`cd relay
bunx wrangler@4 secret put RELAY_TOKEN
bunx wrangler@4 secret put LLM_SECRET
bunx wrangler@4 deploy`}</pre>
        </div>
      )}
      {webhookSecret && (
        <div className="bg-muted/30">
          <div className="flex items-start justify-between gap-2 px-3 pt-2">
            <p className="text-xs text-muted-foreground">ElevenLabs created a webhook secret. It is shown only once: copy it now and give it to the relay.</p>
            <IconAction label="Hide webhook secret" tip="Hide" onClick={() => onWebhookSecret("")}><XIcon aria-hidden /></IconAction>
          </div>
          <SecretBlock name="Webhook secret" value={webhookSecret} copyLabel="Copy webhook secret" />
          <pre className="selectable overflow-x-auto px-3 py-2 font-mono text-xs">{`cd relay
bunx wrangler@4 secret put WEBHOOK_SECRET
bunx wrangler@4 deploy`}</pre>
        </div>
      )}
    </SettingsSection>
  );
}

function OwnerSection({ status }: { status: StatusView }) {
  const numberId = useId();
  const codeId = useId();
  const s = status.settings;
  const [number, setNumber] = useState(s.ownerNumber ?? "");
  const [code, setCode] = useState("");
  const [sent, setSent] = useState(false);
  const start = useAction();
  const confirm = useAction();
  const prerequisites = status.hasTwilio && status.hasElevenLabs && status.hasRelaySecrets && status.relayConnected && !!s.agentId;
  const awaitingCode = sent || status.verificationPending;

  async function startCall() {
    const ok = await start.run(async () => { await Jarvis.startVerification(number.trim()); return true; });
    if (ok) setSent(true);
  }
  async function verify() {
    const next = await confirm.run(() => Jarvis.confirmVerification(code.trim()));
    if (!next) return;
    setCode("");
    setSent(false);
    useJarvis.getState().apply(next);
  }

  const callButton = (
    <Button size="sm" loading={start.busy} disabled={!prerequisites || !number.trim()} onClick={() => void startCall()}>Call me with a code</Button>
  );
  return (
    <SettingsSection title="Your phone" description="Orole only calls a number you have verified. It rings you and reads out a code.">
      <Row label="Your phone number" htmlFor={numberId} hint={s.ownerVerified ? "Verified" : "Like +32470000000"}>
        <Input id={numberId} size="sm" className="w-64" autoComplete="off" value={number} onChange={(event) => setNumber(event.target.value)} />
        {prerequisites ? callButton : (
          <Tooltip>
            <TooltipTrigger render={<span tabIndex={0} />}>{callButton}</TooltipTrigger>
            <TooltipPopup>Finish the earlier setup steps first</TooltipPopup>
          </Tooltip>
        )}
      </Row>
      <ErrorLine message={start.error} />
      {awaitingCode && (
        <Row label="Code" htmlFor={codeId} hint="Enter the 6 digits you heard on the call">
          <Input id={codeId} size="sm" className="w-32" inputMode="numeric" maxLength={6} autoComplete="one-time-code" value={code} onChange={(event) => setCode(event.target.value)} />
          <Button size="sm" loading={confirm.busy} disabled={code.trim().length === 0} onClick={() => void verify()}>Verify</Button>
        </Row>
      )}
      <ErrorLine message={confirm.error} />
    </SettingsSection>
  );
}

function CallsSection({ status }: { status: StatusView }) {
  const s = status.settings;
  const ids = { start: useId(), end: useId(), daily: useId(), monthly: useId(), voice: useId(), model: useId(), language: useId() };
  const [quietStart, setQuietStart] = useState(s.quietStart ?? "");
  const [quietEnd, setQuietEnd] = useState(s.quietEnd ?? "");
  const [dailyCap, setDailyCap] = useState(s.dailyCap ? String(s.dailyCap) : "");
  const [monthlyDollars, setMonthlyDollars] = useState(s.monthlyCapCents ? String(s.monthlyCapCents / 100) : "");
  const [voiceId, setVoiceId] = useState(s.defaultVoiceId ?? "");
  const [ttsModel, setTtsModel] = useState(s.defaultTtsModel ?? "");
  const [language, setLanguage] = useState(s.callLanguage || "nl");
  const [models, setModels] = useState<TTSModel[]>([]);
  const [lookupError, setLookupError] = useState<string | null>(null);
  const action = useAction();

  useEffect(() => {
    if (!status.hasElevenLabs) return; // the list comes from ElevenLabs: nothing to ask without a key
    let active = true;
    Jarvis.models()
      .then((m) => { if (active) { setModels(m); setLookupError(null); } })
      .catch((failure: unknown) => { if (active) setLookupError(errText(failure)); });
    return () => { active = false; };
  }, [status.hasElevenLabs]);

  async function save() {
    const next: PhoneConfig = {
      ...s,
      quietStart,
      quietEnd,
      dailyCap: Math.max(0, Math.floor(Number(dailyCap) || 0)),
      monthlyCapCents: Math.max(0, Math.round((Number(monthlyDollars) || 0) * 100)),
      defaultVoiceId: voiceId,
      defaultTtsModel: ttsModel,
      callLanguage: language,
    };
    const result = await action.run(() => Jarvis.configure(next));
    if (result) useJarvis.getState().apply(result.status);
  }

  const cost = (status.monthCostMicros / 1_000_000).toFixed(2);
  return (
    <SettingsSection title="Calls" description="Limits and the voice Orole speaks with. Empty limits use the defaults (10 calls a day, $20 a month).">
      <Row label="Quiet hours start" htmlFor={ids.start} hint="No calls between start and end">
        <Input id={ids.start} type="time" size="sm" className="w-32" value={quietStart} onChange={(event) => setQuietStart(event.target.value)} />
      </Row>
      <Row label="Quiet hours end" htmlFor={ids.end}>
        <Input id={ids.end} type="time" size="sm" className="w-32" value={quietEnd} onChange={(event) => setQuietEnd(event.target.value)} />
      </Row>
      <Row label="Calls per day" htmlFor={ids.daily}>
        <Input id={ids.daily} type="number" min={0} size="sm" className="w-32" value={dailyCap} onChange={(event) => setDailyCap(event.target.value)} />
      </Row>
      <Row label="Monthly cost cap ($)" htmlFor={ids.monthly}>
        <Input id={ids.monthly} type="number" min={0} step="0.01" size="sm" className="w-32" value={monthlyDollars} onChange={(event) => setMonthlyDollars(event.target.value)} />
      </Row>
      <Row label="Call language" htmlFor={ids.language} hint="What you and your bots speak on a call">
        <LanguagePicker id={ids.language} value={language} onChange={setLanguage} />
      </Row>
      <Row label="Default voice" htmlFor={ids.voice}>
        <LanguageVoicePicker id={ids.voice} value={voiceId} language={language} enabled={status.hasElevenLabs} defaultId="" onChange={setVoiceId} />
      </Row>
      <Row label="Default voice model" htmlFor={ids.model}>
        <ModelPicker id={ids.model} value={ttsModel} models={models} defaultId="" onChange={setTtsModel} />
      </Row>
      <div className="flex items-center justify-between gap-2 px-3 py-2">
        <span className="text-xs text-muted-foreground">Calls today: {status.callsToday} · This month: ${cost}</span>
        <Button size="sm" loading={action.busy} onClick={() => void save()}>Save</Button>
      </div>
      <ErrorLine message={action.error} />
      <ErrorLine message={lookupError && `Could not load voice models: ${lookupError}`} />
    </SettingsSection>
  );
}

const STEP_IDS = ["twilio", "elevenlabs", "relay", "phone", "calls"] as const;
type StepId = (typeof STEP_IDS)[number];

/** Which setup steps are finished. The last, Calls, is only limits and a voice: it has defaults, so it is never "done". */
function stepDone(status: StatusView): Record<StepId, boolean> {
  return {
    twilio: status.hasTwilio,
    elevenlabs: status.hasElevenLabs,
    relay: status.hasRelaySecrets && status.relayConnected && !!status.settings.agentId,
    phone: !!status.settings.ownerVerified,
    calls: false,
  };
}

const STEP_LABELS: Record<StepId, string> = { twilio: "Twilio", elevenlabs: "ElevenLabs", relay: "Relay", phone: "Your phone", calls: "Calls" };

/** Settings > Phone: everything needed before Orole can place and take phone calls, one step at a time. */
export function PhoneSettings() {
  const status = useJarvis((s) => s.status);
  const error = useJarvis((s) => s.error);
  // Shown once after Connect; deliberately kept out of the store.
  const [webhookSecret, setWebhookSecret] = useState("");
  // The wizard opens on the first unfinished step and then stays put until the user moves: finishing a step must
  // not jump away from what it just showed (the relay secrets and the webhook secret are shown once).
  const [current, setCurrent] = useState<number | null>(null);
  const [direction, setDirection] = useState(1);
  const reduced = useReducedMotion();
  useEffect(() => {
    const stop = useJarvis.getState().startSync();
    void useJarvis.getState().load();
    return stop;
  }, []);

  if (!inDesktop) return <p role="status" className="rounded-lg border bg-card p-3 text-muted-foreground">Phone settings are available in the Orole OS desktop app.</p>;
  if (!status) return error ? <Failure error={error} retry={() => void useJarvis.getState().load()} /> : <Loading>Loading phone settings…</Loading>;

  const done = stepDone(status);
  const steps = STEP_IDS.map((id) => ({ id, label: STEP_LABELS[id], done: done[id] }));
  const firstOpen = steps.findIndex((step) => !step.done);
  if (current === null) setCurrent(firstOpen === -1 ? STEP_IDS.length - 1 : firstOpen);
  const index = current ?? 0;
  const id = STEP_IDS[index];
  function go(next: number) {
    setDirection(next >= index ? 1 : -1);
    setCurrent(next);
  }
  const last = index === STEP_IDS.length - 1;

  return (
    <div className="flex min-w-0 flex-col">
      {error && <div className="mb-3"><Failure error={error} retry={() => void useJarvis.getState().load()} /></div>}
      <Stepper steps={steps} current={index} onSelect={go} className="mb-4" />
      <ErrorLine message={status.lastError} />
      <motion.div
        key={id}
        initial={reduced ? false : { opacity: 0, x: direction * 24 }}
        animate={{ opacity: 1, x: 0 }}
        transition={{ duration: reduced ? 0 : 0.2, ease: "easeOut" }}
      >
        {id === "twilio" && <TwilioSection status={status} />}
        {id === "elevenlabs" && <ElevenLabsSection status={status} />}
        {id === "relay" && <RelaySection status={status} webhookSecret={webhookSecret} onWebhookSecret={setWebhookSecret} />}
        {id === "phone" && <OwnerSection status={status} />}
        {id === "calls" && <CallsSection status={status} />}
      </motion.div>
      <div className="mb-5 flex items-center justify-between gap-2">
        <Button size="sm" variant="ghost" disabled={index === 0} onClick={() => go(index - 1)}>Back</Button>
        {!last && <Button size="sm" variant={done[id] ? "default" : "outline"} onClick={() => go(index + 1)}>{done[id] ? "Next" : "Skip for now"}</Button>}
      </div>
      <p className="max-w-prose text-xs text-muted-foreground">
        Calls need Orole running on this PC and the relay online. Phone conversations pass through ElevenLabs and your relay.
        The ElevenLabs post-call and inbound webhooks are workspace-wide settings in your ElevenLabs account.
      </p>
    </div>
  );
}
