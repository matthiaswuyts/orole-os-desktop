import { lazy, Suspense, useEffect } from "react";
import { PlugZap, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { NeedsDesktop } from "@/features/shared/States";
import { inDesktop } from "@/lib/backend";
import { useChat, useSelectedBot } from "@/stores/chat";
import { useHasProvider } from "@/stores/llm";
import { useUI } from "@/stores/ui";
import { Conversation } from "./Conversation";
import { BotPanel } from "./BotPanel";

const NewBot = lazy(() => import("./builder/NewBot").then((m) => ({ default: m.NewBot })));

function CenteredCard({ icon, title, children, action }: { icon?: React.ReactNode; title: string; children?: React.ReactNode; action?: React.ReactNode }) {
  return <div className="flex h-full items-center justify-center p-6">
    <div role="status" className="flex max-w-md flex-col items-center gap-2 rounded-lg border bg-card p-8 text-center">
      {icon && <div aria-hidden className="text-muted-foreground [&_svg]:size-7">{icon}</div>}
      <h2 className="text-sm font-medium">{title}</h2>
      {children && <div className="text-sm text-muted-foreground">{children}</div>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  </div>;
}

/** The chat view: setup states, the bot builder when it is open, otherwise the selected bot's conversation. */
export function ChatView() {
  const hasProvider = useHasProvider();
  const builder = useChat((s) => s.builder);
  const ready = useChat((s) => s.ready);
  const loading = useChat((s) => s.loading);
  const error = useChat((s) => s.error);
  const botCount = useChat((s) => s.bots.length);
  const bot = useSelectedBot();
  const settingsOpen = useChat((s) => s.settingsOpen);

  useEffect(() => useChat.getState().startSync(), []);

  if (!inDesktop) return <NeedsDesktop what="Chat" />;
  if (builder.open) {
    return <Suspense fallback={<p role="status" className="p-6 text-sm text-muted-foreground">Loading the bot builder…</p>}>
      <NewBot />
    </Suspense>;
  }
  if (hasProvider === false) {
    return <CenteredCard icon={<PlugZap />} title="Connect an AI provider to start chatting"
      action={<Button type="button" variant="outline" onClick={() => useUI.getState().openSettings("ai")}>Open AI settings</Button>}>
      <p>Bots talk to a model through a provider you connect, using your own API key. Nothing is sent anywhere until you do.</p>
    </CenteredCard>;
  }
  if (!ready) {
    return error
      ? <CenteredCard title="Could not load your bots" action={<Button type="button" variant="outline" onClick={() => void useChat.getState().load()}>Retry</Button>}><p className="selectable">{error}</p></CenteredCard>
      : <p role="status" className="p-6 text-sm text-muted-foreground">{loading ? "Loading bots…" : "Loading…"}</p>;
  }
  if (!bot) {
    return botCount === 0
      ? <CenteredCard title="No bots yet" action={<Button type="button" onClick={() => useChat.getState().openBuilder("new")}><Plus aria-hidden />Create your first bot</Button>}>
        <p>A bot has its own instructions, model, tools and one long-running conversation.</p>
      </CenteredCard>
      : <CenteredCard title="Pick a bot"><p>Choose a bot from the list to open its conversation.</p></CenteredCard>;
  }
  return <div className="flex h-full min-w-0">
    <div className="min-w-0 flex-1"><Conversation key={bot.id} bot={bot} /></div>
    {settingsOpen && <BotPanel bot={bot} />}
  </div>;
}
