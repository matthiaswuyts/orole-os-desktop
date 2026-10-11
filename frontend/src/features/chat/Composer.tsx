import { ComposerPrimitive, useAui, useAuiState } from "@assistant-ui/react";
import { ArrowUpIcon, FlaskConicalIcon, PaperclipIcon, PlusIcon, SquareIcon, XIcon } from "lucide-react";
import { useEffect, useRef, useState, type KeyboardEvent } from "react";
import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Menu, MenuCheckboxItem, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
import { ComposerVoice } from "@/features/tasks/ComposerVoice";
import { cn } from "@/lib/utils";
import type { Bot } from "@/mygo";
import { useBotRunning, useChatMessages } from "@/stores/chatMessages";
import { chipOf, useCanSend } from "./composerState";
import { MentionHighlight } from "./MentionHighlight";
import { MentionTrigger } from "./MentionTrigger";
import { mentionDeletion, useMentionTargets } from "./mentions";
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
  const text = useAuiState((s) => s.composer.text);
  const canSend = useCanSend();
  const [dictating, setDictating] = useState(false);
  const hasResearch = bot.config.tools.some((tool) => tool.id === "research");
  const research = useChatMessages((s) => s.drafts[bot.id]?.research ?? false) && hasResearch;
  const failed = attachments.some((attachment) => attachment.status.type === "incomplete");
  const hasContent = text.trim() !== "" || attachments.length > 0;
  const targets = useMentionTargets(bot);
  const mirror = useRef<HTMLDivElement>(null);

  // The composer lives and dies with the bot's conversation; its text is kept per bot in the store.
  useEffect(() => {
    const text = useChatMessages.getState().drafts[bot.id]?.text;
    if (text) aui.composer.setText(text);
  }, [aui, bot.id]);

  // Text put in the draft from elsewhere (Add to chat on a selected passage) shows up in the box.
  const draftText = useChatMessages((s) => s.drafts[bot.id]?.text ?? "");
  useEffect(() => {
    if (draftText !== aui.composer.getState().text) aui.composer.setText(draftText);
  }, [aui, draftText]);

  const send = () => { if (canSend) aui.composer.send(); };
  const setResearch = (on: boolean) => useChatMessages.getState().setDraft(bot.id, { research: on });
  const addDictation = (value: string) => {
    const current = aui.composer.getState().text;
    const next = current ? `${current}${/\s$/.test(current) ? "" : " "}${value}` : value;
    aui.composer.setText(next);
    useChatMessages.getState().setDraft(bot.id, { text: next });
  };
  /** Backspace/Delete next to a mention removes all of it, as one unit; returns whether it did. */
  const removeMention = (event: KeyboardEvent<HTMLTextAreaElement>): boolean => {
    const el = event.currentTarget;
    if (event.ctrlKey || event.metaKey || event.altKey || event.shiftKey || event.nativeEvent.isComposing || el.selectionStart !== el.selectionEnd) return false;
    const cut = mentionDeletion(el.value, targets, el.selectionStart, event.key === "Delete");
    if (!cut) return false;
    event.preventDefault();
    const next = el.value.slice(0, cut.from) + el.value.slice(cut.to);
    aui.composer.setText(next);
    useChatMessages.getState().setDraft(bot.id, { text: next });
    requestAnimationFrame(() => el.setSelectionRange(cut.from, cut.from));
    return true;
  };

  return <ComposerPrimitive.TriggerPopoverRoot><ComposerPrimitive.Root data-slot="composer" aria-label="Message composer" className="relative mx-auto flex w-full max-w-3xl shrink-0 flex-col gap-2 px-4 pb-4">
    <ComposerPrimitive.AttachmentDropzone disabled={research} className={cn("flex flex-col gap-1.5 rounded-[1.5rem] border bg-card p-1.5 shadow-xs/5 transition-shadow focus-within:border-ring focus-within:ring-[3px] focus-within:ring-ring/24 data-[dragging=true]:border-ring")}>
      {attachments.length > 0 && <ComposerAttachments aria-label="Attachments" className="px-2 pt-1">
        {attachments.map((attachment, index) => <ComposerAttachmentChip key={attachment.id} attachment={chipOf(attachment)} onRemove={() => void aui.composer.attachment({ index }).remove()} />)}
      </ComposerAttachments>}
      <div className="flex min-w-0 items-end gap-2">
        <Menu>
          <MenuTrigger render={<Button type="button" variant="secondary" size="icon-lg" className="rounded-full" aria-label="Add files and research" />}><PlusIcon aria-hidden /></MenuTrigger>
          <MenuPopup align="start" side="top" className="min-w-56">
            <MenuItem disabled={research || attachments.length >= MAX_ATTACHMENTS} onClick={() => pickFiles((file) => aui.composer.addAttachment(file))}><PaperclipIcon />Attach files</MenuItem>
            {hasResearch && <MenuCheckboxItem checked={research} disabled={attachments.length > 0} onCheckedChange={setResearch}>Research mode</MenuCheckboxItem>}
          </MenuPopup>
        </Menu>
        {research && <Badge variant="info" size="lg" render={<button type="button" aria-label="Research mode on. Turn off" onClick={() => setResearch(false)} />}>
          <FlaskConicalIcon aria-hidden />Research<XIcon aria-hidden />
        </Badge>}
        <div className="relative min-w-0 flex-1">
          <MentionHighlight text={text} targets={targets} mirrorRef={mirror} />
          <ComposerPrimitive.Input
            aria-label={`Message ${bot.name}`}
            placeholder={research ? "What should I research?" : running ? `Queue a message for ${bot.name}` : `Message ${bot.name}`}
            submitMode="none"
            rows={1}
            maxRows={8}
            className="relative block max-h-48 min-h-9 w-full resize-none overflow-y-auto bg-transparent px-1 py-2 text-sm text-transparent caret-foreground outline-none selection:bg-primary/25 selection:text-transparent placeholder:text-muted-foreground"
            onChange={(event) => useChatMessages.getState().setDraft(bot.id, { text: event.target.value })}
            onScroll={(event) => { if (mirror.current) mirror.current.scrollTop = event.currentTarget.scrollTop; }}
            onKeyDown={(event) => {
              if ((event.key === "Backspace" || event.key === "Delete") && removeMention(event)) return;
              if (event.key !== "Enter" || event.shiftKey || event.nativeEvent.isComposing) return;
              // Enter picks the highlighted mention while the picker is open.
              if (event.currentTarget.getAttribute("aria-expanded") === "true") return;
              event.preventDefault();
              send();
            }}
          />
        </div>
        {running && <Button type="button" size="icon-lg" className="rounded-full" aria-label="Stop" variant={hasContent ? "secondary" : "default"} onClick={() => void useChatMessages.getState().cancel(bot.id)}><SquareIcon aria-hidden className="fill-current" /></Button>}
        {!running && (!hasContent || dictating) && <ComposerVoice compact disabled={false} onActiveChange={setDictating} onTranscript={addDictation} />}
        {hasContent && !dictating && <Button type="button" size="icon-lg" className="rounded-full" aria-label={running ? "Queue message" : "Send message"} disabled={!canSend} onClick={send}><ArrowUpIcon aria-hidden /></Button>}
      </div>
    </ComposerPrimitive.AttachmentDropzone>
    {failed && <p role="status" className="px-1 text-xs text-muted-foreground">Remove files that could not be attached to send your message.</p>}
    <MentionTrigger targets={targets} />
  </ComposerPrimitive.Root></ComposerPrimitive.TriggerPopoverRoot>;
}
