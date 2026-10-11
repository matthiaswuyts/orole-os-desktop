import { useExternalStoreRuntime, type AppendMessage, type AssistantRuntime } from "@assistant-ui/react";
import { useCallback, useEffect, useMemo } from "react";
import type { Bot } from "@/mygo";
import { selectConversation, useChatMessages } from "@/stores/chatMessages";
import { useChat } from "@/stores/chat";
import { useChatDrafts } from "@/stores/chatDrafts";
import { mentionsOf, targetsFor } from "../mentions";
import { ATTACHMENT_PART, createAttachmentAdapter } from "./attachments";
import { convertMessage } from "./convert";
import { useBotModel } from "./useBotModel";

/** What a composer submission carries to the daemon. */
export function sendInputOf(message: Pick<AppendMessage, "content" | "attachments">): { text: string; attachmentIds: string[] } {
  const text = message.content.flatMap((part) => (part.type === "text" ? [part.text] : [])).join("\n\n");
  const attachmentIds = (message.attachments ?? []).flatMap((attachment) =>
    attachment.content.flatMap((part) => {
      const data: unknown = part.type === "data" && part.name === ATTACHMENT_PART ? part.data : undefined;
      const id = typeof data === "object" && data !== null && "id" in data ? data.id : undefined;
      return typeof id === "string" ? [id] : [];
    }));
  return { text, attachmentIds };
}

/**
 * assistant-ui's view of one bot's conversation. The daemon owns everything: sending stores the message there and
 * the reply arrives by event; there are no branches, edits or thread list because the daemon cannot honour them.
 */
export function useChatRuntime(bot: Pick<Bot, "id" | "provider" | "model">, running: boolean): AssistantRuntime {
  const botId = bot.id;
  const conversation = useChatMessages(selectConversation(botId));
  const { caps } = useBotModel(bot);
  const attachments = useMemo(() => createAttachmentAdapter(), []);
  useEffect(() => attachments.setSupport(caps), [attachments, caps]);

  const onNew = useCallback(async (message: AppendMessage) => {
    const { send } = useChatMessages.getState();
    const { drafts, setDraft } = useChatDrafts.getState();
    const { text, attachmentIds } = sendInputOf(message);
    const mode = drafts[botId]?.research ? "research" : "";
    const { bots, skills } = mentionsOf(text, targetsFor(useChat.getState().bots, botId));
    // The composer has already cleared itself; the draft follows so a bot switch does not bring the text back.
    setDraft(botId, { text: "", research: false });
    await send(botId, { text, attachmentIds, mode, mentions: mode ? [] : bots, skills: mode ? [] : skills });
  }, [botId]);
  const onCancel = useCallback(() => useChatMessages.getState().cancel(botId), [botId]);
  const onReload = useCallback(async () => {
    const last = useChatMessages.getState().byBot[botId]?.messages.findLast((m) => m.role !== "user");
    if (last) await useChatMessages.getState().regenerate(botId, last.id);
  }, [botId]);
  const onLoadEarlier = useCallback(() => useChatMessages.getState().loadOlder(botId), [botId]);

  return useExternalStoreRuntime({
    isRunning: running,
    messages: conversation.messages,
    convertMessage,
    hasEarlier: conversation.hasMore,
    onLoadEarlier,
    onNew,
    onCancel,
    onReload,
    adapters: { attachments },
  });
}
