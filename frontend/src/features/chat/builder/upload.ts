import { Chat, type KnowledgeFile } from "@/mygo";

/** Bytes per chunk before base64 (the daemon's AttachmentChunkMax); frames are capped at 4 MiB. */
export const CHUNK_BYTES = 1 << 20;
/** The daemon's per-file cap (MaxAttachmentBytes). It validates again; this only spares a pointless transfer. */
export const MAX_FILE_BYTES = 20 << 20;

function toBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

/**
 * Stages `file` in 1 MiB base64 chunks, commits it and attaches it to the bot as knowledge. Every daemon
 * error is thrown as-is; a staged upload that does not end up attached is discarded.
 */
export async function uploadKnowledge(botId: string, file: File, onProgress?: (sent: number) => void): Promise<KnowledgeFile> {
  if (file.size > MAX_FILE_BYTES) throw new Error(`${file.name} is larger than ${MAX_FILE_BYTES >> 20} MiB, the most a file may be.`);
  const { id } = await Chat.beginAttachment({ name: file.name, size: file.size });
  try {
    for (let offset = 0, seq = 0; offset < file.size; offset += CHUNK_BYTES, seq++) {
      const chunk = new Uint8Array(await file.slice(offset, offset + CHUNK_BYTES).arrayBuffer());
      await Chat.attachmentChunk({ id, seq, dataBase64: toBase64(chunk) });
      onProgress?.(Math.min(offset + CHUNK_BYTES, file.size));
    }
    const attachment = await Chat.commitAttachment(id);
    return await Chat.addKnowledge({ botId, attachmentId: attachment.id });
  } catch (cause) {
    // Best effort: an orphaned upload expires on its own, and the original failure is what matters.
    await Chat.discardAttachment(id).catch(() => undefined);
    throw cause;
  }
}
