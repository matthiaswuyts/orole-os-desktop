import { create } from "zustand";

/** The composer's per-bot state kept while another bot is open. */
export interface Draft { text: string; research: boolean }

interface State {
  drafts: Record<string, Draft>;
  setDraft: (botId: string, patch: Partial<Draft>) => void;
  dropDraft: (botId: string) => void;
}

/**
 * Drafts change on every keystroke. They live apart from the conversation store so typing never wakes the selectors
 * that watch messages, and a streaming reply never wakes the composer's draft readers.
 */
export const useChatDrafts = create<State>((set) => ({
  drafts: {},
  setDraft: (botId, change) => set((state) => {
    const current = state.drafts[botId] ?? { text: "", research: false };
    const next = { ...current, ...change };
    return next.text === current.text && next.research === current.research && state.drafts[botId] ? state : { drafts: { ...state.drafts, [botId]: next } };
  }),
  dropDraft: (botId) => set((state) => {
    if (!(botId in state.drafts)) return state;
    const { [botId]: _removed, ...rest } = state.drafts;
    return { drafts: rest };
  }),
}));
