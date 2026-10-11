import { useRef, useState, type KeyboardEvent } from "react";
import { ArrowUpIcon, FolderIcon, GitBranchIcon } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupTextarea } from "@/components/ui/input-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ErrorNote } from "@/features/agents/shared";
import { useAction } from "@/features/agents/format";
import { useAgents } from "@/stores/agents";
import { composerState } from "./composerState";
import { ComposerContext } from "./ComposerContext";
import { ComposerVoice } from "./ComposerVoice";
import { MessageQueue } from "./MessageQueue";
import { PermissionModeSelect } from "./PermissionModeSelect";
import type { PermissionMode } from "./permissionModes";
import { useTaskMessage } from "./useTaskMessage";
import { TaskAttachmentButton, TaskAttachmentPreviews, TaskModelPicker } from "./TaskMessageControls";

/** Follow-up box for a Code task: one surface, the draft on top, one toolbar underneath. Enter sends; Ctrl+Enter inserts an explicit line. */
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
  const inactive = action.busy || voiceActive;
  const modeControl = session.followup;
  const where = session.worktreeOwned ? "Worktree" : session.machine && session.machine !== "local" ? session.machine : "Local checkout";

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
  // The daemon decides whether the mode can change and when it takes effect; this only relays the choice.
  const changeMode = async (mode: PermissionMode, confirmYolo: boolean) => {
    useAgents.getState().upsert(await Agents.setPermissionMode(session.id, mode, confirmYolo));
  };

  return <div className="bg-background px-5 pt-2 pb-3">
    <div className="mx-auto flex max-w-4xl flex-col gap-1.5">
      <MessageQueue session={session} />
      {!state.canSend ? <>
        <InputGroup className="rounded-3xl [&_textarea]:min-h-0! [&_textarea]:py-3!"><InputGroupTextarea aria-label="Message the agent" disabled rows={1} placeholder="Messages are unavailable" /></InputGroup>
        <p role="status" className="text-xs text-muted-foreground">{state.reason}</p>
      </> : <form className="flex flex-col gap-1.5" onSubmit={(e) => { e.preventDefault(); send(); }}>
        {state.note && <p className="px-2 text-xs text-muted-foreground">{state.note}</p>}
        <InputGroup className="rounded-3xl bg-card! has-[input:disabled,textarea:disabled]:opacity-100">
          {message.attachments.files.length > 0 && <InputGroupAddon align="block-start" className="pb-0"><TaskAttachmentPreviews message={message} disabled={inactive} /></InputGroupAddon>}
          <InputGroupTextarea ref={input} aria-label="Message the agent" aria-description="Enter to send. Ctrl+Enter to add a line." placeholder="Do anything…" value={text} disabled={inactive} onChange={(e) => setText(e.target.value)} onKeyDown={onKeyDown} style={{ height: `${Math.min(8, text.split("\n").length) * 1.5 + 1.5}rem`, fieldSizing: "fixed" }} className="w-full [&_textarea]:min-h-0! [&_textarea]:px-4 [&_textarea]:pt-3 [&_textarea]:pb-1 [&_textarea]:leading-6" rows={1} />
          <InputGroupAddon align="block-end" className="gap-1 px-2 pb-2">
            <TaskAttachmentButton message={message} disabled={inactive} />
            <PermissionModeSelect value={session.permissionMode} onChange={changeMode} support={message.harness?.permissionModes}
              disabled={inactive || !modeControl?.permissionMode}
              disabledReason={modeControl?.permissionMode ? "" : modeControl?.permissionModeReason || "This task cannot change permission mode."} />
            <div className="ml-auto flex min-w-0 shrink-0 items-center gap-1">
              <TaskModelPicker message={message} disabled={inactive} />
              <ComposerVoice compact quiet disabled={action.busy} onActiveChange={setVoiceActive} onTranscript={(value) => setText((current) => current ? `${current}${/\s$/.test(current) ? "" : " "}${value}` : value)} />
              <ComposerContext usage={session.usage} />
              <Tooltip>
                <TooltipTrigger render={<Button type="submit" aria-label={state.label} className="rounded-full" size="icon-sm" loading={action.busy} disabled={inactive || !message.attachments.ready || (!text.trim() && !message.attachments.ids.length)}><ArrowUpIcon /></Button>} />
                <TooltipPopup>{state.label}</TooltipPopup>
              </Tooltip>
            </div>
          </InputGroupAddon>
        </InputGroup>
        {(session.dir || session.branch) && <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-0.5 px-3 text-xs text-muted-foreground">
          {session.dir && <span className="flex min-w-0 items-center gap-1.5" title={session.dir}><FolderIcon aria-hidden className="size-3.5 shrink-0" /><span className="max-w-48 truncate">{where}</span></span>}
          {session.branch && <span className="flex min-w-0 items-center gap-1.5" title={session.branch}><GitBranchIcon aria-hidden className="size-3.5 shrink-0" /><span className="max-w-48 truncate">{session.branch}</span></span>}
        </div>}
        <ErrorNote error={action.error} />
      </form>}
    </div>
  </div>;
}
