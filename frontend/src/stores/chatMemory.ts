import { create } from "zustand";
import { Chat, events, type MemoryEntry, type MemorySaveRequest, type MemoryScope } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import type { Outcome } from "@/stores/llm";

/** Entries the daemon returns per scope at most (its own cap). */
const ENTRY_LIMIT = 500;

export interface Listing<T> {
  items: T[];
  loaded: boolean;
  loading: boolean;
  error: string | null;
}

/** What the entry list of a scope is showing: an optional full-text query and whether invalidated history is included. */
export interface EntryFilter {
  query: string;
  history: boolean;
}

export interface EntryListing extends Listing<MemoryEntry> {
  filter: EntryFilter;
}

interface State {
  /** Scopes a bot can see, by bot id. */
  scopes: Record<string, Listing<MemoryScope>>;
  /** Entries by scope id (pending ones included). */
  entries: Record<string, EntryListing>;
  loadScopes: (botId: string) => Promise<void>;
  loadEntries: (scope: string, filter: EntryFilter) => Promise<void>;
  saveEntry: (request: MemorySaveRequest) => Promise<Outcome<MemoryEntry>>;
  deleteEntry: (id: string, scope: string) => Promise<Outcome>;
  resolve: (id: string, approve: boolean, scope: string) => Promise<Outcome>;
  createScope: (name: string, botId: string) => Promise<Outcome<MemoryScope>>;
  deleteScope: (id: string, botId: string) => Promise<Outcome>;
  startSync: () => () => void;
}

const EMPTY_FILTER: EntryFilter = { query: "", history: false };
const blank = <T,>(): Listing<T> => ({ items: [], loaded: false, loading: false, error: null });

/** Daemon errors carry a machine prefix ("bad_request: ..."); the user needs the sentence. */
const message = (cause: unknown) => (cause instanceof Error ? cause.message : String(cause)).replace(/^(bad_request|not_found|conflict|canceled|timeout|internal|denied): /, "");

let token = 0;
/** Latest request per key; a response only lands while its token is still current. */
const latest = new Map<string, number>();
let subscribers = 0;
let stop: (() => void) | undefined;

async function attempt<T>(run: () => Promise<T>): Promise<Outcome<T>> {
  try {
    return { ok: true, value: await run() };
  } catch (cause) {
    return { ok: false, error: message(cause) };
  }
}

/** The daemon owns memory. This is a disposable, event-synchronized snapshot for the bot builder. */
export const useChatMemory = create<State>((set, get) => ({
  scopes: {}, entries: {},

  async loadScopes(botId) {
    if (!inDesktop) return;
    const key = `scopes:${botId}`;
    const request = ++token;
    latest.set(key, request);
    set((state) => ({ scopes: { ...state.scopes, [botId]: { ...(state.scopes[botId] ?? blank()), loading: true, error: null } } }));
    try {
      const items = (await Chat.memoryScopes(botId)) ?? [];
      if (latest.get(key) === request) set((state) => ({ scopes: { ...state.scopes, [botId]: { items, loaded: true, loading: false, error: null } } }));
    } catch (cause) {
      if (latest.get(key) === request) set((state) => ({ scopes: { ...state.scopes, [botId]: { ...(state.scopes[botId] ?? blank()), loading: false, error: message(cause) } } }));
    }
  },

  async loadEntries(scope, filter) {
    if (!inDesktop) return;
    const key = `entries:${scope}`;
    const request = ++token;
    latest.set(key, request);
    set((state) => ({ entries: { ...state.entries, [scope]: { ...(state.entries[scope] ?? { ...blank<MemoryEntry>(), filter }), filter, loading: true, error: null } } }));
    try {
      const items = (await Chat.memoryEntries({ scope, kind: "", includePending: true, includeInvalidated: filter.history, query: filter.query.trim(), limit: ENTRY_LIMIT })) ?? [];
      if (latest.get(key) === request) set((state) => ({ entries: { ...state.entries, [scope]: { items, loaded: true, loading: false, error: null, filter } } }));
    } catch (cause) {
      if (latest.get(key) === request) set((state) => ({ entries: { ...state.entries, [scope]: { ...(state.entries[scope] ?? { ...blank<MemoryEntry>(), filter }), loading: false, error: message(cause), filter } } }));
    }
  },

  async saveEntry(request) {
    const outcome = await attempt(() => Chat.saveMemoryEntry(request));
    if (outcome.ok) refreshScope(request.scope);
    return outcome;
  },

  async deleteEntry(id, scope) {
    const outcome = await attempt(() => Chat.deleteMemoryEntry(id));
    if (outcome.ok) refreshScope(scope);
    return outcome;
  },

  async resolve(id, approve, scope) {
    const outcome = await attempt(() => Chat.resolveMemory({ id, approve }));
    if (outcome.ok) refreshScope(scope);
    return outcome;
  },

  async createScope(name, botId) {
    const outcome = await attempt(() => Chat.createMemoryScope(name));
    if (outcome.ok) void get().loadScopes(botId);
    return outcome;
  },

  async deleteScope(id, botId) {
    const outcome = await attempt(() => Chat.deleteMemoryScope(id));
    if (outcome.ok) {
      latest.delete(`entries:${id}`);
      set((state) => {
        const entries = { ...state.entries };
        delete entries[id];
        return { entries };
      });
      void get().loadScopes(botId);
    }
    return outcome;
  },

  startSync() {
    if (!inDesktop) return () => {};
    if (++subscribers === 1) {
      // Subscribe first so a change during the first request is not lost; tokens drop the older response.
      const offMemory = events.chatMemory.on(({ scope }) => refreshScope(scope));
      const offDaemon = useDaemon.subscribe((state, previous) => {
        if (state.link === "connected" && previous.link !== "connected") refreshScope("");
      });
      stop = () => { offMemory(); offDaemon(); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) { stop?.(); stop = undefined; }
    };
  },
}));

/** Reloads what is on screen for a scope ("" = everything loaded) and the usage meters of every loaded bot. */
function refreshScope(scope: string) {
  const { entries, scopes, loadEntries, loadScopes } = useChatMemory.getState();
  for (const [id, listing] of Object.entries(entries)) {
    if (!scope || id === scope) void loadEntries(id, listing.filter ?? EMPTY_FILTER);
  }
  for (const botId of Object.keys(scopes)) void loadScopes(botId);
}

/** Entries waiting for the user's approval. */
export const pendingOf = (entries: readonly MemoryEntry[]): MemoryEntry[] => entries.filter((e) => e.pending && !e.invalidatedAt);
