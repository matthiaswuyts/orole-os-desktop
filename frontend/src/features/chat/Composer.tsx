import { ComposerPrimitive, useAui, useAuiState } from "@assistant-ui/react";
import { ArrowUpIcon, FlaskConicalIcon, PaperclipIcon, PlusIcon, SquareIcon, XIcon } from "lucide-react";
import { useEffect } from "react";
import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Menu, MenuCheckboxItem, MenuGroup, MenuGroupLabel, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { cn } from "@/lib/utils";
import type { Bot } from "@/mygo";
import { useChat } from "@/stores/chat";
import { useBotRunning, useChatMessages } from "@/stores/chatMessages";
import { chipOf, useCanSend } from "./composerState";
import { ModelChip } from "./ConversationHeader";
import { toolNoun } from "./parts/format";
import { MAX_ATTACHMENTS } from "./runtime/attachments";

/** Opens the system file picker and hands each file to the composer, which stages it on the daemon. */
function pickFiles(add: (file: File) => Promise<void>) {
  const input = document.createElement("input");
  input.type = "file";
  input.multiple = true;
  input.onchange = () => { for (const file of Array.from(input.files ?? [])) void add(file).catch(() => {}); };
  input.click();
}

export function Composer({ bot }: { bot: Bot }) {
  const aui = useAui();
  const running = useBotRunning(bot.id);
  const attachments = useAuiState((s) => s.composer.attachments);
  const canSend = useCanSend();
  const hasResearch = bot.config.tools.some((tool) => tool.id === "research");
  const research = useChatMessages((s) => s.drafts[bot.id]?.research ?? false) && hasResearch;
  const failed = attachments.some((attachment) => attachment.status.type === "incomplete");

  // The composer lives and dies with the bot's conversation; its text is kept per bot in the store.
  useEffect(() => {
    const text = useChatMessages.getState().drafts[bot.id]?.text;
    if (text) aui.composer.setText(text);
  }, [aui, bot.id]);

  const send = () => { if (canSend) aui.composer.send(); };
  const setResearch = (on: boolean) => useChatMessages.getState().setDraft(bot.id, { research: on });

  return <ComposerPrimitive.Root data-slot="composer" aria-label="Message composer" className="mx-auto flex w-full max-w-3xl shrink-0 flex-col gap-2 px-4 pb-4">
    <ComposerPrimitive.AttachmentDropzone disabled={research} className={cn("flex flex-col gap-2 rounded-2xl border bg-card p-2 shadow-xs/5 transition-shadow focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/24 data-[dragging=true]:border-ring")}>
      {attachments.length > 0 && <ComposerAttachments aria-label="Attachments">
        {attachments.map((attachment, index) => <ComposerAttachmentChip key={attachment.id} attachment={chipOf(attachment)} onRemove={() => void aui.composer.attachment({ index }).remove()} />)}
      </ComposerAttachments>}
      <ComposerPrimitive.Input
        aria-label={`Message ${bot.name}`}
        placeholder={research ? "What should I research?" : `Message ${bot.name}`}
        submitMode="none"
        rows={1}
        maxRows={10}
        className="max-h-60 min-h-9 w-full resize-none bg-transparent px-2 py-1.5 text-sm outline-none placeholder:text-muted-foreground"
        onChange={(event) => useChatMessages.getState().setDraft(bot.id, { text: event.target.value })}
        onKeyDown={(event) => {
          if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
          event.preventDefault();
          send();
        }}
      />
      <div className="flex min-w-0 items-center gap-1.5">
        <Menu>
          <MenuTrigger render={<Button type="button" variant="ghost" size="icon-sm" aria-label="Add files, research and tools" />}><PlusIcon aria-hidden /></MenuTrigger>
          <MenuPopup align="start" side="top" className="min-w-56">
            <MenuItem disabled={research || attachments.length >= MAX_ATTACHMENTS} onClick={() => pickFiles((file) => aui.composer.addAttachment(file))}><PaperclipIcon />Attach files</MenuItem>
            {hasResearch && <MenuCheckboxItem checked={research} disabled={attachments.length > 0} onCheckedChange={setResearch}>Research mode</MenuCheckboxItem>}
            <MenuSeparator />
            <MenuGroup>
              <MenuGroupLabel>Enabled tools</MenuGroupLabel>
              {bot.config.tools.length === 0 && <MenuItem disabled>No tools enabled</MenuItem>}
              {bot.config.tools.map((tool) => <MenuItem key={tool.id} disabled>{toolNoun(tool.id)}</MenuItem>)}
            </MenuGroup>
            <MenuSeparator />
            <MenuItem onClick={() => useChat.getState().openBuilder(bot.id)}>Edit bot</MenuItem>
          </MenuPopup>
        </Menu>
        {research && <Badge variant="info" size="lg" render={<button type="button" aria-label="Research mode on. Turn off" onClick={() => setResearch(false)} />}>
          <FlaskConicalIcon aria-hidden />Research<XIcon aria-hidden />
        </Badge>}
        <span className="min-w-0 flex-1" />
        <ModelChip bot={bot} />
        {running && <Button type="button" variant="outline" size="icon-sm" aria-label="Stop" onClick={() => void useChatMessages.getState().cancel(bot.id)}><SquareIcon aria-hidden className="fill-current" /></Button>}
        <Button type="button" size="icon-sm" aria-label={running ? "Queue message" : "Send message"} disabled={!canSend} onClick={send}><ArrowUpIcon aria-hidden /></Button>
      </div>
    </ComposerPrimitive.AttachmentDropzone>
    {failed && <p role="status" className="px-1 text-xs text-muted-foreground">Remove files that could not be attached to send your message.</p>}
  </ComposerPrimitive.Root>;
}
