import { useState, type KeyboardEvent } from "react";
import { ArrowUpIcon } from "lucide-react";
import { Agents, type Session } from "@/mygo";
import { useAgents } from "@/stores/agents";
import { Button } from "@/components/ui/button";
import { InputGroup, InputGroupAddon, InputGroupText, InputGroupTextarea } from "@/components/ui/input-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ErrorNote } from "@/features/agents/shared";
import { useAction } from "@/features/agents/format";
import { composerState } from "./composerState";
import { ComposerContext } from "./ComposerContext";
import { ComposerVoice } from "./ComposerVoice";
import { MessageQueue } from "./MessageQueue";

/** Sticky message box under the feed. Enter sends, Shift+Enter adds a line. */
export function Composer({ session }: { session: Session }) {
  return <SessionComposer key={session.id} session={session} />;
}

function SessionComposer({ session }: { session: Session }) {
  const [text, setText] = useState("");
  const [voiceActive, setVoiceActive] = useState(false);
  const action = useAction();
  const state = composerState(session);


  const send = () => {
    const prompt = text.trim();
    if (!prompt || action.busy || voiceActive || !state.canSend) return;
    void action.run(async () => {
      useAgents.getState().upsert(await (state.delivery === "queue" ? Agents.queuePrompt(session.id, prompt) : Agents.prompt(session.id, prompt)));
      setText((current) => current.trim() === prompt ? "" : current);
    });
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); send(); }
  };

  return <div className="bg-background px-5 py-3">
    <div className="mx-auto flex max-w-4xl flex-col gap-2">
      <MessageQueue session={session} />
      {!state.canSend ? <>
        <InputGroup><InputGroupTextarea aria-label="Message the agent" disabled rows={2} placeholder="Messages are unavailable" /></InputGroup>
        <p role="status" className="text-xs text-muted-foreground">{state.reason}</p>
      </> : <form className="flex flex-col gap-2" onSubmit={(e) => { e.preventDefault(); send(); }}>
        {state.note && <p className="text-xs text-muted-foreground">{state.note}</p>}
        <InputGroup>
          <InputGroupTextarea aria-label="Message the agent" placeholder={state.placeholder} value={text} disabled={action.busy || voiceActive} onChange={(e) => setText(e.target.value)} onKeyDown={onKeyDown} className="max-h-48" rows={3} />
          <InputGroupAddon align="block-end">
            <ComposerContext usage={session.usage} />
            <InputGroupText className="ml-auto hidden sm:flex">Shift+Enter for new line</InputGroupText>
            <Tooltip>
              <TooltipTrigger render={<Button type="submit" aria-label={state.label} className="ml-auto rounded-full sm:ml-0" size="icon-sm" loading={action.busy} disabled={action.busy || voiceActive || !text.trim()}><ArrowUpIcon /></Button>} />
              <TooltipPopup>{state.label}</TooltipPopup>
            </Tooltip>
          </InputGroupAddon>
        </InputGroup>
        <ComposerVoice disabled={action.busy} onActiveChange={setVoiceActive} onTranscript={(value) => setText((current) => current ? `${current}${/\s$/.test(current) ? "" : " "}${value}` : value)} />
        <ErrorNote error={action.error} />
      </form>}
    </div>
  </div>;
}
