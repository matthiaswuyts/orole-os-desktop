import { create } from "zustand";
import { Files, type FileRead, type FilesInfo, type FileWriteResult } from "@/mygo";

export interface EditorTab {
  id: string;
  provider: string;
  path: string;
  name: string;
  text: string;
  savedText: string;
  etag: string;
  bom: boolean;
  lineEnding: "\n" | "\r\n";
  readOnly: boolean;
  reason?: string;
  loading: boolean;
  saving: boolean;
  error?: string;
  conflict?: FileWriteResult;
}
export const fileId = (provider: string, path: string) => `${provider}:${path}`;
export const isDirty = (tab: EditorTab) => tab.text !== tab.savedText;
export const fileError = (error: unknown) => error instanceof Error ? error.message : String(error);
let nextTabId = 0;
export const fileName = (path: string) => path.split(/[\\/]/).filter(Boolean).at(-1) ?? path;
export const parentPath = (path: string) => {
  const index = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  if (index < 0) return path;
  if (index === 0) return path.slice(0, 1);
  if (index === 2 && /^[a-z]:/i.test(path)) return path.slice(0, 3);
  return path.slice(0, index);
};
export const joinPath = (path: string, name: string) => `${path.replace(/[\\/]$/, "")}${path.includes("\\") ? "\\" : "/"}${name}`;
export const containsPath = (root: string, path: string) => {
  const normalize = (s: string) => s.replace(/\\/g, "/").replace(/\/$/, "");
  const r = normalize(root), p = normalize(path);
  return p === r || p.startsWith(`${r}/`);
};

/** Keep encoding metadata separately: CodeMirror's document uses LF internally. */
export function decodeFile(read: FileRead): Pick<EditorTab, "text" | "savedText" | "etag" | "bom" | "lineEnding" | "readOnly" | "reason"> {
  const bytes = Uint8Array.from(atob(read.data), (c) => c.charCodeAt(0));
  const bom = bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf;
  let text = "", reason: string | undefined;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    if (text.includes("\0")) reason = "Binary files cannot be edited. Open this file in its native application.";
  } catch {
    reason = "This file is not valid UTF-8. Open it in an editor that supports its encoding.";
  }
  if (read.truncated || read.offset !== 0) reason = "Only part of this file was read. Partial files cannot be saved.";
  if (reason) text = "";
  const lineEnding = text.includes("\r\n") ? "\r\n" : "\n";
  text = text.replace(/\r\n/g, "\n");
  return { text, savedText: text, etag: read.info.etag ?? "", bom, lineEnding, readOnly: !!reason || !!read.info.readOnly, reason: reason ?? (read.info.readOnly ? "This file is read-only." : undefined) };
}
export function encodeFile(tab: Pick<EditorTab, "text" | "bom" | "lineEnding">): string {
  const bytes = new TextEncoder().encode((tab.bom ? "\uFEFF" : "") + tab.text.replace(/\n/g, tab.lineEnding));
  let binary = "";
  for (let i = 0; i < bytes.length; i += 8192) binary += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(binary);
}

interface FilesState {
  tabs: EditorTab[];
  activeId: string | null;
  provider: string;
  directory: string;
  setLocation: (provider: string, directory: string) => void;
  openFile: (provider: string, path: string) => Promise<void>;
  activate: (id: string) => void;
  edit: (id: string, text: string) => void;
  save: (id: string, force?: boolean) => Promise<boolean>;
  reload: (id: string) => Promise<boolean>;
  close: (id: string, discard?: boolean) => boolean;
  dismissConflict: (id: string) => void;
  renamed: (provider: string, from: string, info: FilesInfo) => void;
  deleted: (provider: string, path: string) => void;
}

export const useFiles = create<FilesState>((set, get) => {
  const update = (id: string, change: Partial<EditorTab>) => set((s) => ({ tabs: s.tabs.map((tab) => tab.id === id ? { ...tab, ...change } : tab) }));
  return {
    tabs: [], activeId: null, provider: "local", directory: "",
    setLocation: (provider, directory) => set({ provider, directory }),
    activate: (activeId) => set({ activeId }),
    openFile: async (provider, path) => {
      set({ provider, directory: parentPath(path) });
      const existing = get().tabs.find((tab) => tab.provider === provider && tab.path === path);
      if (existing) { set({ activeId: existing.id }); return; }
      const id = `${fileId(provider, path)}#${++nextTabId}`;
      const tab: EditorTab = { id, provider, path, name: fileName(path), text: "", savedText: "", etag: "", bom: false, lineEnding: "\n", readOnly: false, loading: true, saving: false };
      set((s) => ({ tabs: [...s.tabs, tab], activeId: id }));
      try { update(id, { ...decodeFile(await Files.read(provider, path, 0, 0)), loading: false }); }
      catch (error) { update(id, { loading: false, readOnly: true, error: fileError(error) }); }
    },
    edit: (id, text) => {
      const tab = get().tabs.find((t) => t.id === id);
      if (tab && !tab.readOnly && !tab.loading) update(id, { text });
    },
    save: async (id, force = false) => {
      const tab = get().tabs.find((t) => t.id === id);
      if (!tab || tab.readOnly || tab.loading || tab.saving) return false;
      if (!isDirty(tab) && !force) return true;
      const snapshot = tab.text;
      update(id, { saving: true, error: undefined });
      try {
        const result = await Files.write(tab.provider, tab.path, encodeFile(tab), tab.etag, false, force);
        if (result.conflict) { update(id, { conflict: result, saving: false }); return false; }
        if (!result.info) throw new Error("The file service did not confirm the saved version.");
        update(id, { savedText: snapshot, etag: result.info.etag ?? "", conflict: undefined, saving: false });
        return true;
      } catch (error) { update(id, { saving: false, error: fileError(error) }); return false; }
    },
    reload: async (id) => {
      const tab = get().tabs.find((t) => t.id === id);
      if (!tab || tab.saving || tab.loading) return false;
      update(id, { loading: true, error: undefined });
      try {
        update(id, { ...decodeFile(await Files.read(tab.provider, tab.path, 0, 0)), loading: false, conflict: undefined });
        return true;
      } catch (error) { update(id, { loading: false, error: fileError(error) }); return false; }
    },
    close: (id, discard = false) => {
      const tab = get().tabs.find((t) => t.id === id);
      if (tab?.saving || (tab && isDirty(tab) && !discard)) return false;
      set((s) => {
        const tabs = s.tabs.filter((t) => t.id !== id);
        return { tabs, activeId: s.activeId === id ? tabs.at(-1)?.id ?? null : s.activeId };
      });
      return true;
    },
    dismissConflict: (id) => update(id, { conflict: undefined }),
    renamed: (provider, from, info) => set((s) => ({ tabs: s.tabs.map((tab) => {
      if (tab.provider !== provider || !containsPath(from, tab.path)) return tab;
      const path = info.path + tab.path.slice(from.length);
      // IDs stay stable so in-flight editor operations keep their target.
      return { ...tab, path, name: fileName(path), etag: tab.path === from ? info.etag ?? tab.etag : tab.etag };
    }) })),
    deleted: (provider, path) => set((s) => ({ tabs: s.tabs.map((tab) => tab.provider === provider && containsPath(path, tab.path) ? { ...tab, conflict: { conflict: true, deleted: true } } : tab) })),
  };
});

/** Public entry point for palette and other views; the store survives lazy-view unmounts. */
export const openFile = (provider: string, path: string) => useFiles.getState().openFile(provider, path);
