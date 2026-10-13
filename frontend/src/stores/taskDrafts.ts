import { create } from "zustand";

interface State {
  /** Unsent follow-up text per task session; a session with no draft has no entry. */
  drafts: Record<string, string>;
  setDraft: (sessionId: string, text: string | ((current: string) => string)) => void;
}

/**
 * Task composer drafts, kept in memory so leaving a task (another view, another task) and coming back keeps what
 * was typed. Not persisted: a draft can hold anything the user pasted, and it ends with the window.
 */
export const useTaskDrafts = create<State>((set) => ({
  drafts: {},
  setDraft: (sessionId, change) => set((state) => {
    const current = state.drafts[sessionId] ?? "";
    const next = typeof change === "function" ? change(current) : change;
    if (next === current) return state;
    if (next) return { drafts: { ...state.drafts, [sessionId]: next } };
    const { [sessionId]: _removed, ...rest } = state.drafts;
    return { drafts: rest };
  }),
}));
