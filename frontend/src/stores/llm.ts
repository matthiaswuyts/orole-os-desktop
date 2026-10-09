import { useEffect } from "react";
import { create } from "zustand";
import { LLM, events, type Defaults, type ManifestRecord, type Model, type ProviderManifest, type ProviderStatus } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";

/** A failed action carries a message the UI can show verbatim; a successful one carries its result. */
export type Outcome<T = void> = { ok: true; value: T } | { ok: false; error: string; /** the daemon's error code (conflict, denied, canceled, bad_request, …) when it sent one */ code?: string };

/** What a provider offers, fetched lazily and kept until the daemon says the provider changed. */
export interface ModelCatalog {
  models: Model[];
  loading: boolean;
  error: string | null;
}

/** The kind of action in flight for a provider id. */
export type Busy = "connect" | "test" | "disconnect";

/** The kind of action in flight for a manifest id. */
export type ManifestBusy = "save" | "trust" | "revoke" | "remove";

interface State {
  providers: ProviderStatus[];
  defaults: Defaults | null;
  models: Record<string, ModelCatalog>;
  /** Stored provider manifests (never contains API keys), loaded independently of the provider list. */
  manifests: ManifestRecord[];
  manifestsReady: boolean;
  manifestsError: string | null;
  manifestBusy: Record<string, ManifestBusy>;
  busy: Record<string, Busy>;
  ready: boolean;
  loading: boolean;
  error: string | null;
  load: () => Promise<void>;
  loadModels: (id: string, force?: boolean) => Promise<void>;
  loadManifests: () => Promise<void>;
  /** Empty `expectedDigest` creates (disabled); the current digest replaces and revokes trust if the manifest changed. */
  saveManifest: (manifest: ProviderManifest, expectedDigest: string) => Promise<Outcome<ManifestRecord>>;
  /** Enabling waits for the daemon's separate policy approval; denial or cancellation is an error outcome. */
  trustManifest: (id: string, digest: string, enabled: boolean) => Promise<Outcome<ManifestRecord>>;
  removeManifest: (id: string, digest: string) => Promise<Outcome>;
  connect: (id: string, apiKey: string) => Promise<Outcome<ProviderStatus>>;
  disconnect: (id: string) => Promise<Outcome>;
  test: (id: string) => Promise<Outcome<ProviderStatus>>;
  setDefaults: (defaults: Defaults) => Promise<Outcome<Defaults>>;
  /** Resolves with the authorization URL; the PKCE verifier stays in the daemon. */
  openRouterAuthStart: () => Promise<Outcome<string>>;
  openRouterAuthFinish: (code: string) => Promise<Outcome<ProviderStatus>>;
  startSync: () => () => void;
}

let sequence = 0;
let manifestSequence = 0;
let subscribers = 0;
let stop: (() => void) | undefined;
let modelCounter = 0;
// Latest model request per provider; a response only lands while its token is still current.
const modelRequests = new Map<string, number>();

const OFFLINE = "The daemon is offline. Reconnect to manage AI providers.";
const CODE = /^(bad_request|not_found|conflict|canceled|timeout|internal|denied): /;
const message = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause)).replace(CODE, "");
const codeOf = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause)).match(CODE)?.[1];
/** Defensive: a secret must not reach the UI even if an error text were to quote it. */
const redact = (text: string, secret: string) => secret.length >= 4 ? text.split(secret).join("…") : text;

function withManifest(records: ManifestRecord[], record: ManifestRecord): ManifestRecord[] {
  const next = records.some((r) => r.manifest.id === record.manifest.id)
    ? records.map((r) => r.manifest.id === record.manifest.id ? record : r)
    : [...records, record];
  return next.sort((a, b) => a.manifest.id.localeCompare(b.manifest.id));
}

async function actManifest<T>(id: string, kind: ManifestBusy, run: () => Promise<T>): Promise<Outcome<T>> {
  useLLM.setState((state) => ({ manifestBusy: { ...state.manifestBusy, [id]: kind } }));
  try {
    return { ok: true, value: await run() };
  } catch (cause) {
    return { ok: false, error: message(cause), code: codeOf(cause) };
  } finally {
    useLLM.setState((state) => {
      const manifestBusy = { ...state.manifestBusy };
      delete manifestBusy[id];
      return { manifestBusy };
    });
  }
}

function withStatus(providers: ProviderStatus[], status: ProviderStatus): ProviderStatus[] {
  return providers.some((p) => p.id === status.id)
    ? providers.map((p) => p.id === status.id ? status : p)
    : [...providers, status];
}

function dropModels(ids: readonly string[] | "all") {
  if (ids === "all") { modelRequests.clear(); useLLM.setState({ models: {} }); return; }
  for (const id of ids) modelRequests.delete(id);
  useLLM.setState((state) => {
    const models = { ...state.models };
    for (const id of ids) delete models[id];
    return { models };
  });
}

async function act<T>(id: string, kind: Busy | null, run: () => Promise<T>, secret = ""): Promise<Outcome<T>> {
  if (kind) useLLM.setState((state) => ({ busy: { ...state.busy, [id]: kind } }));
  try {
    return { ok: true, value: await run() };
  } catch (cause) {
    return { ok: false, error: redact(message(cause), secret) };
  } finally {
    if (kind) useLLM.setState((state) => {
      const busy = { ...state.busy };
      delete busy[id];
      return { busy };
    });
  }
}

/** Only the daemon owns provider state and credentials. This is a disposable, event-synchronized snapshot. */
export const useLLM = create<State>((set, get) => ({
  providers: [], defaults: null, models: {}, busy: {}, ready: false, loading: false, error: null,
  manifests: [], manifestsReady: false, manifestsError: null, manifestBusy: {},

  async load() {
    if (!inDesktop) return;
    const request = ++sequence;
    set({ loading: true, error: null });
    try {
      const [providers, defaults] = await Promise.all([LLM.providers(), LLM.defaults()]);
      if (request === sequence) set({ providers: providers ?? [], defaults, ready: true, loading: false, error: null });
    } catch (cause) {
      if (request === sequence) set({ ready: false, loading: false, error: message(cause) });
    }
  },

  async loadManifests() {
    if (!inDesktop) return;
    const request = ++manifestSequence;
    try {
      const manifests = await LLM.manifests();
      if (request === manifestSequence) set({ manifests: manifests ?? [], manifestsReady: true, manifestsError: null });
    } catch (cause) {
      if (request === manifestSequence) set({ manifestsReady: false, manifestsError: message(cause) });
    }
  },

  async saveManifest(manifest, expectedDigest) {
    const outcome = await actManifest(manifest.id, "save", () => LLM.saveManifest({ manifest, expectedDigest }));
    if (outcome.ok) {
      // Newer than any list request still in flight.
      ++manifestSequence;
      set((state) => ({ manifests: withManifest(state.manifests, outcome.value), manifestsReady: true, manifestsError: null }));
      dropModels([manifest.id]);
      void get().load();
    }
    return outcome;
  },

  async trustManifest(id, digest, enabled) {
    const outcome = await actManifest(id, enabled ? "trust" : "revoke", () => LLM.trustManifest({ id, digest, enabled }));
    if (outcome.ok) {
      ++manifestSequence;
      set((state) => ({ manifests: withManifest(state.manifests, outcome.value), manifestsReady: true, manifestsError: null }));
      dropModels([id]);
      void get().load();
    }
    return outcome;
  },

  async removeManifest(id, digest) {
    const outcome = await actManifest(id, "remove", () => LLM.removeManifest({ id, digest }));
    if (outcome.ok) {
      ++manifestSequence;
      set((state) => ({ manifests: state.manifests.filter((r) => r.manifest.id !== id) }));
      dropModels([id]);
      void get().load();
    }
    return outcome;
  },

  async loadModels(id, force = false) {
    if (!inDesktop) return;
    const current = get().models[id];
    // A failed fetch is terminal until forced, otherwise an effect keyed on the cache would retry forever.
    if (current && !force) return;
    const token = ++modelCounter;
    modelRequests.set(id, token);
    set((state) => ({ models: { ...state.models, [id]: { models: current?.models ?? [], loading: true, error: null } } }));
    try {
      const models = (await LLM.models(id)) ?? [];
      if (modelRequests.get(id) === token) set((state) => ({ models: { ...state.models, [id]: { models, loading: false, error: null } } }));
    } catch (cause) {
      if (modelRequests.get(id) === token) set((state) => ({ models: { ...state.models, [id]: { models: [], loading: false, error: message(cause) } } }));
    }
  },

  async connect(id, apiKey) {
    const outcome = await act(id, "connect", () => LLM.connect({ id, apiKey }), apiKey);
    if (outcome.ok) { dropModels([id]); set((state) => ({ providers: withStatus(state.providers, outcome.value) })); }
    return outcome;
  },

  async disconnect(id) {
    const outcome = await act(id, "disconnect", () => LLM.disconnect(id));
    if (outcome.ok) {
      dropModels([id]);
      set((state) => ({ providers: state.providers.map((p) => p.id === id ? { id: p.id, name: p.name, kind: p.kind, connected: false } : p) }));
    }
    return outcome;
  },

  async test(id) {
    const outcome = await act(id, "test", () => LLM.test(id));
    if (outcome.ok) set((state) => ({ providers: withStatus(state.providers, outcome.value) }));
    return outcome;
  },

  async setDefaults(defaults) {
    const outcome = await act("", null, () => LLM.setDefaults(defaults));
    if (outcome.ok) set({ defaults: outcome.value });
    return outcome;
  },

  async openRouterAuthStart() {
    const outcome = await act("openrouter", null, () => LLM.openRouterAuthStart());
    return outcome.ok ? { ok: true, value: outcome.value.url } : outcome;
  },

  async openRouterAuthFinish(code) {
    const outcome = await act("openrouter", "connect", () => LLM.openRouterAuthFinish(code), code);
    if (outcome.ok) { dropModels([outcome.value.id]); set((state) => ({ providers: withStatus(state.providers, outcome.value) })); }
    return outcome;
  },

  startSync() {
    if (!inDesktop) return () => {};
    if (++subscribers === 1) {
      const refresh = () => { void get().load(); void get().loadManifests(); };
      // Subscribe first; each notification invalidates older in-flight responses.
      const offChanged = events.llmChanged.on(({ id }) => {
        // A change that names a provider (connect, disconnect) makes its catalogue and account suspect;
        // a change without one is a defaults update.
        if (id) dropModels([id]);
        refresh();
      });
      const offDaemon = useDaemon.subscribe((state, previous) => {
        if (state.link === "connected" && previous.link !== "connected") refresh();
        if (state.link === "offline" && previous.link !== "offline") {
          ++sequence;
          ++manifestSequence;
          dropModels("all");
          set({ ready: false, loading: false, error: OFFLINE, manifestsReady: false, manifestsError: OFFLINE });
        }
      });
      window.addEventListener("focus", refresh);
      refresh();
      stop = () => { offChanged(); offDaemon(); window.removeEventListener("focus", refresh); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) {
        ++sequence;
        ++manifestSequence;
        dropModels("all");
        stop?.();
        stop = undefined;
        set({ ready: false, loading: false, manifestsReady: false });
      }
    };
  },
}));

/**
 * Whether any provider is connected: `null` until the daemon has answered (or when it cannot,
 * e.g. in the browser preview), so callers can tell "unknown" from "none". Keeps the snapshot synced
 * while the calling component is mounted.
 */
export function useHasProvider(): boolean | null {
  useEffect(() => useLLM.getState().startSync(), []);
  return useLLM((state) => state.ready ? state.providers.some((p) => p.connected) : null);
}
