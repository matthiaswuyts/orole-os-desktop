import { create } from "zustand";
import { Chat, events, type Bot, type Message, type MessagePreview } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";

const SELECTED_KEY = "orole.chat.selected";
const PREVIEW_MAX = 160;

interface BuilderState {
  open: boolean;
  /** The bot being edited; null while creating a new one. */
  botId: string | null;
}

export interface ChatState {
  /** Newest conversation first, then by name. */
  bots: Bot[];
  selectedBotId: string | null;
  running: Record<string, boolean>;
  builder: BuilderState;
  /** False in the browser preview, where there is no daemon. */
  available: boolean;
  ready: boolean;
  loading: boolean;
  error: string | null;
  load: () => Promise<void>;
  startSync: () => () => void;
  select: (botId: string | null) => void;
  /** Selects the bot and shows the chat view (palette and rail entry point). */
  open: (botId: string) => void;
  openBuilder: (botId: string | "new") => void;
  closeBuilder: () => void;
  /** The slim settings panel beside the conversation (name, description, instructions, model). */
  settingsOpen: boolean;
  setSettingsOpen: (open: boolean) => void;
}

const readSelected = (): string | null => {
  try { return localStorage.getItem(SELECTED_KEY) || null; } catch { return null; }
};
const writeSelected = (id: string | null) => {
  try {
    if (id) localStorage.setItem(SELECTED_KEY, id); else localStorage.removeItem(SELECTED_KEY);
  } catch {
    // Selection then lasts for the session only.
  }
};

/** Latest activity first; bots nobody has talked to follow, alphabetically. */
export function sortBots(bots: Bot[]): Bot[] {
  return bots.toSorted((a, b) => (b.lastMessage?.at ?? 0) - (a.lastMessage?.at ?? 0) || a.name.localeCompare(b.name));
}

/** One-line text for the rail from a message snapshot, or null when it has nothing worth showing yet (e.g. a tool-only step). */
export function previewOf(message: Message): MessagePreview | null {
  const parts = message.parts ?? [];
  let text = parts.filter((p) => p.type === "text").map((p) => p.text ?? "").join(" ");
  if (!text.trim()) text = parts.find((p) => p.type === "report")?.text ?? "";
  if (!text.trim() && parts.some((p) => p.type === "attachment")) text = "Attachment";
  if (!text.trim() && message.status === "error") text = message.error ?? "";
  text = text.replace(/\s+/g, " ").trim();
  if (!text) return null;
  return { role: message.role, text: text.length > PREVIEW_MAX ? `${text.slice(0, PREVIEW_MAX - 1)}…` : text, at: message.createdAt };
}

let sequence = 0;
let subscribers = 0;
let stop: (() => void) | undefined;
/** Bots whose live state arrived by event while a list request was in flight; the (older) response must not overwrite them. */
const touched = new Set<string>();

/** The daemon owns bots and conversations. This is a disposable, event-synchronized snapshot for the rail. */
export const useChat = create<ChatState>((set, get) => ({
  bots: [], selectedBotId: readSelected(), running: {}, builder: { open: false, botId: null }, settingsOpen: false,
  available: inDesktop, ready: false, loading: false, error: null,
  async load() {
    if (!get().available) return;
    const request = ++sequence;
    touched.clear();
    set({ loading: true, error: null });
    try {
      const fetched = await Chat.bots() ?? [];
      if (request !== sequence) return;
      set((state) => {
        const current = new Map(state.bots.map((b) => [b.id, b]));
        const bots = fetched.map((b) => {
          if (!touched.has(b.id)) return b;
          const live = current.get(b.id);
          return live ? { ...b, running: state.running[b.id] ?? b.running, lastMessage: live.lastMessage } : b;
        });
        const running = Object.fromEntries(bots.map((b) => [b.id, touched.has(b.id) ? state.running[b.id] ?? b.running : b.running]));
        const selectedBotId = state.selectedBotId && bots.some((b) => b.id === state.selectedBotId) ? state.selectedBotId : null;
        if (selectedBotId !== state.selectedBotId) writeSelected(null);
        return { bots: sortBots(bots), running, selectedBotId, ready: true, loading: false, error: null };
      });
    } catch (cause) {
      if (request === sequence) set({ ready: false, loading: false, error: cause instanceof Error ? cause.message : String(cause) });
    }
  },
  startSync() {
    if (!get().available) return () => {};
    if (++subscribers === 1) {
      const refresh = () => { void get().load(); };
      // Subscribe first so nothing that happens during the first list is lost.
      const offRun = events.chatRun.on(({ botId, running }) => {
        touched.add(botId);
        // Kept even for a bot the first list has not delivered yet; the list merge consults it.
        set((state) => ({ running: { ...state.running, [botId]: running } }));
      });
      const offMessage = events.chatMessage.on((message) => {
        touched.add(message.botId);
        const preview = previewOf(message);
        if (!preview) return;
        set((state) => {
          const bot = state.bots.find((b) => b.id === message.botId);
          if (!bot || (bot.lastMessage && bot.lastMessage.at > preview.at)) return state;
          return { bots: sortBots(state.bots.map((b) => b === bot ? { ...b, lastMessage: preview } : b)) };
        });
      });
      const offBot = events.chatBot.on(({ id, action }) => {
        if (action === "archived") {
          set((state) => {
            if (state.selectedBotId === id) writeSelected(null);
            const running = { ...state.running };
            delete running[id];
            return {
              bots: state.bots.filter((b) => b.id !== id), running,
              selectedBotId: state.selectedBotId === id ? null : state.selectedBotId,
              builder: state.builder.botId === id ? { open: false, botId: null } : state.builder,
            };
          });
        } else if (action === "cleared") {
          set((state) => ({ bots: sortBots(state.bots.map((b) => b.id === id ? { ...b, lastMessage: undefined } : b)) }));
        }
        refresh();
      });
      const offDaemon = useDaemon.subscribe((state, previous) => {
        if (state.link === "connected" && previous.link !== "connected") refresh();
        if (state.link === "offline" && previous.link !== "offline") {
          ++sequence;
          set({ ready: false, loading: false, error: "The daemon is offline. Reconnect to chat with your bots." });
        }
      });
      window.addEventListener("focus", refresh);
      refresh();
      stop = () => { offRun(); offMessage(); offBot(); offDaemon(); window.removeEventListener("focus", refresh); };
    }
    let stopped = false;
    return () => {
      if (stopped) return;
      stopped = true;
      if (--subscribers === 0) {
        ++sequence;
        stop?.();
        stop = undefined;
        set({ ready: false, loading: false });
      }
    };
  },
  select(botId) {
    writeSelected(botId);
    set({ selectedBotId: botId });
  },
  open(botId) {
    get().select(botId);
    useUI.getState().setView("chat");
  },
  openBuilder: (botId) => set({ builder: { open: true, botId: botId === "new" ? null : botId } }),
  closeBuilder: () => set({ builder: { open: false, botId: null } }),
  setSettingsOpen: (settingsOpen) => set({ settingsOpen }),
}));

/** The bot whose conversation is open, if it still exists. */
export function useSelectedBot(): Bot | undefined {
  return useChat((state) => state.bots.find((b) => b.id === state.selectedBotId));
}
