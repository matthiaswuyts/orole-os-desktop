import { useRef, useState, type KeyboardEvent } from "react";
import { ArrowUpIcon, FolderIcon, GitBranchIcon } from "lucide-react";
import { type Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupTextarea } from "@/components/ui/input-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ErrorNote } from "@/features/agents/shared";
import { useAction } from "@/features/agents/format";
import { composerState } from "./composerState";
import { ComposerContext } from "./ComposerContext";
import { ComposerVoice } from "./ComposerVoice";
import { MessageQueue } from "./MessageQueue";
import { useTaskMessage } from "./useTaskMessage";
import { TaskAttachmentButton, TaskAttachmentPreviews, TaskModelPicker } from "./TaskMessageControls";

/** Compact follow-up box. Enter sends; Ctrl+Enter inserts an explicit line. */
export function Composer({ session }: { session: Session }) {
  return <SessionComposer key={session.id} session={session} />;
}

function SessionComposer({ session }: { session: Session }) {
  const [text, setText] = useState("");
  const [voiceActive, setVoiceActive] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const action = useAction();
  const state = composerState(session);
  const message = useTaskMessage(session);


  const send = () => {
    const prompt = text.trim();
    if ((!prompt && !message.attachments.ids.length) || action.busy || voiceActive || !state.canSend || !message.attachments.ready) return;
    void action.run(async () => {
      await message.send(prompt);
      setText((current) => current.trim() === prompt ? "" : current);
    });
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key !== "Enter" || e.nativeEvent.isComposing) return;
    e.preventDefault();
    if (e.ctrlKey) {
      const start = e.currentTarget.selectionStart;
      const end = e.currentTarget.selectionEnd;
      setText((value) => `${value.slice(0, start)}\n${value.slice(end)}`);
      requestAnimationFrame(() => input.current?.setSelectionRange(start + 1, start + 1));
    } else if (!e.shiftKey && !e.altKey && !e.metaKey) send();
  };

  return <div className="bg-background px-5 py-3">
    <div className="mx-auto flex max-w-4xl flex-col gap-2">
      <MessageQueue session={session} />
      {!state.canSend ? <>
        <InputGroup className="rounded-3xl [&_textarea]:min-h-0! [&_textarea]:py-3!"><InputGroupTextarea aria-label="Message the agent" disabled rows={1} placeholder="Messages are unavailable" /></InputGroup>
        <p role="status" className="text-xs text-muted-foreground">{state.reason}</p>
      </> : <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); send(); }}>
        {state.note && <p className="text-xs text-muted-foreground">{state.note}</p>}
        <TaskAttachmentPreviews message={message} disabled={action.busy || voiceActive} />
        <InputGroup className="min-h-12 rounded-3xl px-1 [&_textarea]:min-h-0! [&_textarea]:py-2!">
          <InputGroupTextarea ref={input} aria-label="Message the agent" aria-description="Enter to send. Ctrl+Enter to add a line." placeholder="Do anything…" value={text} disabled={action.busy || voiceActive} onChange={(e) => setText(e.target.value)} onKeyDown={onKeyDown} style={{ height: `${Math.min(6, text.split("\n").length) * 1.5 + 1}rem`, fieldSizing: "fixed" }} className="[&_textarea]:min-w-0 [&_textarea]:flex-1 [&_textarea]:leading-6" rows={1} />
          <InputGroupAddon align="inline-start" className="shrink-0 self-end pb-1.5">
            <TaskAttachmentButton message={message} disabled={action.busy || voiceActive} />
          </InputGroupAddon>
          <InputGroupAddon align="inline-end" className="shrink-0 gap-1 self-end pb-1.5">
            <TaskModelPicker message={message} disabled={action.busy || voiceActive} />
            <Tooltip>
              <TooltipTrigger render={<Button type="submit" aria-label={state.label} className="rounded-full" size="icon-sm" loading={action.busy} disabled={action.busy || voiceActive || !message.attachments.ready || (!text.trim() && !message.attachments.ids.length)}><ArrowUpIcon /></Button>} />
              <TooltipPopup>{state.label}</TooltipPopup>
            </Tooltip>
          </InputGroupAddon>
        </InputGroup>
        <div className="flex min-w-0 items-start justify-between gap-3 px-2">
          <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 py-1 text-xs text-muted-foreground">
            {session.dir && <span className="flex min-w-0 items-center gap-1.5" title={session.dir}><FolderIcon aria-hidden className="size-3.5 shrink-0" /><span className="max-w-48 truncate">{session.worktreeOwned ? "Worktree" : session.machine && session.machine !== "local" ? session.machine : "Local checkout"}</span></span>}
            {session.branch && <span className="flex min-w-0 items-center gap-1.5" title={session.branch}><GitBranchIcon aria-hidden className="size-3.5 shrink-0" /><span className="max-w-48 truncate">{session.branch}</span></span>}
          </div>
          <div className="ml-auto flex shrink-0 items-start gap-1">
            <ComposerVoice disabled={action.busy} onActiveChange={setVoiceActive} onTranscript={(value) => setText((current) => current ? `${current}${/\s$/.test(current) ? "" : " "}${value}` : value)} />
            <ComposerContext usage={session.usage} />
          </div>
        </div>
        <ErrorNote error={action.error} />
      </form>}
    </div>
  </div>;
}
