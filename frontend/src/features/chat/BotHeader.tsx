import { MonitorIcon, PanelRightIcon } from "lucide-react";
import { BotAvatar } from "@/components/shell/BotAvatar";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { computerView } from "@/features/computer/computerState";
import { useComputer } from "@/features/computer/useComputer";
import type { Bot } from "@/mygo";
import { useChat, useSelectedBot } from "@/stores/chat";

/** The title bar's content in Chat: the selected bot's avatar and name, nothing else. */
export function BotTitle({ bot }: { bot: Bot }) {
  return <div data-slot="bot-title" className="flex min-w-0 items-center gap-2">
    <BotAvatar name={bot.name} avatar={bot.avatar} className="size-5" />
    <h1 className="truncate font-medium text-sm">{bot.name}</h1>
  </div>;
}

/** Why the Computer button does nothing while the computer is not running (the pane would only say so). */
const COMPUTER_IDLE: Record<string, string> = {
  off: "The computer is off. It starts when the bot uses it.",
  starting: "The computer is starting…",
  stopping: "The computer is shutting down…",
  failed: "The computer failed to start. It starts again the next time the bot uses it.",
};

/**
 * Opens the bot's computer beside the conversation. While the computer is off, starting, stopping or failed there is
 * nothing to show, so the button stays inert and says why instead of opening a pane that only repeats it. Setup (this
 * PC cannot run one yet) and a running computer still open the pane, and an open pane can always be closed.
 */
function ComputerButton({ botId }: { botId: string }) {
  const computerOpen = useChat((s) => s.pane?.kind === "computer");
  const { availability, status, handoff } = useComputer(botId);
  const view = computerView(availability, status, handoff);
  const idle = computerOpen ? undefined : COMPUTER_IDLE[view.kind];
  return <Tooltip>
    <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-xs" aria-label={computerOpen ? "Hide computer" : "Show computer"} aria-pressed={computerOpen}
      aria-disabled={idle ? true : undefined} className={idle ? "opacity-50" : undefined}
      onClick={() => { if (computerOpen) useChat.getState().closePane(); else if (!idle) useChat.getState().openComputerPane(botId); }} />}>
      <MonitorIcon aria-hidden />
    </TooltipTrigger>
    <TooltipPopup>{idle ?? "Computer"}</TooltipPopup>
  </Tooltip>;
}

/** The title bar's actions in Chat: the bot's computer (for bots that have one) and its settings. */
export function BotActions() {
  const open = useChat((s) => s.settingsOpen);
  const bot = useSelectedBot();
  return <>
    {bot?.config.computer && <ComputerButton botId={bot.id} />}
    <Button type="button" variant="ghost" size="icon-xs" aria-label={open ? "Hide bot settings" : "Show bot settings"} aria-pressed={open} onClick={() => useChat.getState().setSettingsOpen(!open)}><PanelRightIcon aria-hidden /></Button>
  </>;
}
