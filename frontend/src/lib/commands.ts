import type { ReactNode } from "react";

/** A palette action. Integrations and plugins contribute via registerCommands. */
export interface Command {
  id: string;
  title: string;
  category: string;
  keywords?: string[];
  icon?: ReactNode;
  run: () => void | Promise<void>;
}

/** An async result provider (mail search, repos, files…). Queries are server-side where available. */
export interface SearchProvider {
  id: string;
  label: string;
  search: (query: string, signal: AbortSignal) => Promise<Command[]>;
}

const commands = new Map<string, Command>();
const providers = new Map<string, SearchProvider>();
const listeners = new Set<() => void>();
let version = 0;
const emit = () => { version++; listeners.forEach((l) => l()); };

export function registerCommands(list: Command[]): () => void {
  list.forEach((c) => commands.set(c.id, c));
  emit();
  return () => { list.forEach((c) => commands.delete(c.id)); emit(); };
}
export function registerProvider(p: SearchProvider): () => void {
  providers.set(p.id, p);
  emit();
  return () => { providers.delete(p.id); emit(); };
}
export const getCommands = () => [...commands.values()];
export const getProviders = () => [...providers.values()];
export const subscribeRegistry = (l: () => void) => { listeners.add(l); return () => { listeners.delete(l); }; };
export const registryVersion = () => version;

/** Fuzzy subsequence score; 0 = no match. Prefix and word-start matches rank higher. */
export function fuzzyScore(query: string, text: string): number {
  if (!query) return 1;
  const q = query.toLowerCase(), t = text.toLowerCase();
  const idx = t.indexOf(q);
  if (idx === 0) return 100 + q.length;
  if (idx > 0) return 60 + q.length - idx * 0.1;
  let qi = 0, score = 0, prev = -2;
  for (let i = 0; i < t.length && qi < q.length; i++) {
    if (t[i] === q[qi]) {
      score += prev === i - 1 ? 3 : 1 + (i === 0 || t[i - 1] === " " ? 2 : 0);
      prev = i; qi++;
    }
  }
  return qi === q.length ? score : 0;
}

const RECENT_KEY = "orole.recentCommands";
export const getRecent = (): string[] => { try { return JSON.parse(localStorage.getItem(RECENT_KEY) ?? "[]") as string[]; } catch { return []; } };
export const pushRecent = (id: string) => localStorage.setItem(RECENT_KEY, JSON.stringify([id, ...getRecent().filter((x) => x !== id)].slice(0, 8)));
