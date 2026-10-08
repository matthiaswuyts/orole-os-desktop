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

/** Sticky message box under the feed. Enter sends, Shift+Enter adds a line. */
export function Composer({ session }: { session: Session }) {
  const [text, setText] = useState("");
  const action = useAction();
  const state = composerState(session);

  if (!state.canSend) return <div className="bg-background px-5 py-3"><div className="mx-auto max-w-4xl"><InputGroup><InputGroupTextarea aria-label="Message the agent" disabled rows={2} placeholder="Messages are unavailable" /></InputGroup><p role="status" className="mt-2 text-xs text-muted-foreground">{state.reason}</p></div></div>;

  const send = () => {
    const prompt = text.trim();
    if (!prompt || action.busy) return;
    void action.run(async () => {
      useAgents.getState().upsert(await Agents.prompt(session.id, prompt));
      setText("");
    });
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); send(); }
  };

  return <form className="bg-background px-5 py-3" onSubmit={(e) => { e.preventDefault(); send(); }}>
    <div className="mx-auto flex max-w-4xl flex-col gap-2">
      {state.note && <p className="text-xs text-muted-foreground">{state.note}</p>}
      <InputGroup>
        <InputGroupTextarea aria-label="Message the agent" placeholder={state.placeholder} value={text} disabled={action.busy} onChange={(e) => setText(e.target.value)} onKeyDown={onKeyDown} className="max-h-48" rows={3} />
        <InputGroupAddon align="block-end">
          <InputGroupText className="ml-auto hidden sm:flex">Shift+Enter for new line</InputGroupText>
          <Tooltip>
            <TooltipTrigger render={<Button type="submit" aria-label={state.label} className="ml-auto rounded-full sm:ml-0" size="icon-sm" loading={action.busy} disabled={action.busy || !text.trim()}><ArrowUpIcon /></Button>} />
            <TooltipPopup>{state.label}</TooltipPopup>
          </Tooltip>
        </InputGroupAddon>
      </InputGroup>
      <ErrorNote error={action.error} />
    </div>
  </form>;
}
