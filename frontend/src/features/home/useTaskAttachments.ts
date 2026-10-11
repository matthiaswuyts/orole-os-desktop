import { useEffect, useRef, useState } from "react";
import { Agents } from "@/mygo";
import { toastError } from "@/features/projects/dialogs";
import type { ComposerAttachment } from "@/components/assistant-ui/elements/composer-attachments";

const MAX_FILES = 8;
const MAX_BYTES = 2 * 1024 * 1024;
export type TaskAttachment = ComposerAttachment & { key: string; uploadId?: string };

function fileData(file: File): Promise<string> {
  const { promise, resolve, reject } = Promise.withResolvers<string>();
  const reader = new FileReader();
  reader.onload = () => resolve(String(reader.result).split(",", 2)[1] ?? "");
  reader.onerror = () => reject(reader.error ?? new Error("Could not read this file."));
  reader.onabort = () => reject(new Error("File reading was cancelled."));
  reader.readAsDataURL(file);
  return promise;
}
function sizeLabel(bytes: number) {
  return bytes < 1024 ? `${bytes} B` : bytes < 1024 * 1024 ? `${Math.ceil(bytes / 1024)} KB` : `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
function discard(id: string) {
  // Abandoned staging files also expire in the daemon. Never turn a successful
  // launch into a failure because disposal raced a daemon disconnect.
  void Agents.discardAttachment(id).catch(() => {});
}

/** `initial`: uploads a failed launch hands back; they are already staged in the daemon. */
export function useTaskAttachments(enabled: boolean, initial: TaskAttachment[] = []) {
  const [files, setFiles] = useState<TaskAttachment[]>(initial);
  const current = useRef<TaskAttachment[]>(initial);
  const mounted = useRef(true);
  function update(next: TaskAttachment[]) { current.current = next; if (mounted.current) setFiles(next); }
  function clear() {
    const previous = current.current;
    update([]);
    for (const file of previous) if (file.uploadId) discard(file.uploadId);
  }
  /** Hands the files to a launch: they leave this composer without being discarded, the launch now owns them. */
  function release() {
    const previous = current.current;
    update([]);
    return previous;
  }
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      const previous = current.current;
      current.current = [];
      for (const file of previous) if (file.uploadId) discard(file.uploadId);
    };
  }, []);
  function remove(key: string) {
    const file = current.current.find((item) => item.key === key);
    update(current.current.filter((item) => item.key !== key));
    if (file?.uploadId) discard(file.uploadId);
  }
  async function add(incoming: File[]) {
    if (!enabled || !mounted.current) return;
    const available = MAX_FILES - current.current.length;
    if (incoming.length > available) toastError("Too many attachments", new Error(`Attach up to ${MAX_FILES} files per task.`));
    const entries = incoming.slice(0, available).map((file) => ({ file, entry: {
      key: crypto.randomUUID(), name: file.name, meta: "Uploading…", state: "uploading" as const,
      kind: file.type.startsWith("image/") ? "image" as const : /\.(zip|gz|tar|7z|rar)$/i.test(file.name) ? "archive" as const : "text" as const,
    } }));
    update([...current.current, ...entries.map(({ entry }) => entry)]);
    for (const { file, entry } of entries) {
      if (!current.current.some((item) => item.key === entry.key)) continue;
      try {
        if (file.size > MAX_BYTES) throw new Error("Files must be 2 MB or smaller.");
        const dataBase64 = await fileData(file);
        if (!current.current.some((item) => item.key === entry.key)) continue;
        const uploaded = await Agents.uploadAttachment({ name: file.name, dataBase64 });
        if (!mounted.current || !current.current.some((item) => item.key === entry.key)) { discard(uploaded.id); continue; }
        update(current.current.map((item) => item.key === entry.key ? { ...item, uploadId: uploaded.id, state: "done", meta: sizeLabel(uploaded.size) } : item));
      } catch (error) {
        if (!mounted.current || !current.current.some((item) => item.key === entry.key)) continue;
        update(current.current.map((item) => item.key === entry.key ? { ...item, state: "error", meta: "Upload failed — remove and try again" } : item));
        toastError(`Could not attach ${file.name}`, error);
      }
    }
  }
  return { files, add, remove, clear, release, ids: files.flatMap((file) => file.uploadId ? [file.uploadId] : []), ready: files.every((file) => file.state === "done"), canAdd: enabled && files.length < MAX_FILES };
}
