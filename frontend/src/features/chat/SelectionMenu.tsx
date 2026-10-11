import { LightbulbIcon, MinimizeIcon, SparklesIcon, type LucideIcon } from "lucide-react";
import { useEffect, useState, type RefObject } from "react";
import { Button } from "@/components/ui/button";
import { useChatMessages } from "@/stores/chatMessages";
import { withQuote } from "./quote";

const ACTIONS: { label: string; ask: string; icon: LucideIcon }[] = [
  { label: "Shorten", ask: "Shorten this selected text.", icon: MinimizeIcon },
  { label: "Improve", ask: "Improve this selected text.", icon: SparklesIcon },
  { label: "Explain", ask: "Explain this selected text.", icon: LightbulbIcon },
];

interface Selected { text: string; x: number; y: number }

/** The selected text when it lies wholly inside one bot bubble under `root`; otherwise null. */
export function selectedReplyText(root: HTMLElement | null): Selected | null {
  const sel = window.getSelection();
  if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const range = sel.getRangeAt(0);
  const bubble = (node: Node | null) => (node instanceof Element ? node : node?.parentElement)?.closest("[data-slot=chat-bubble]") ?? null;
  const start = bubble(range.startContainer);
  if (!start || start !== bubble(range.endContainer) || !root.contains(start)) return null;
  const text = sel.toString().trim();
  if (!text) return null;
  const rect = range.getBoundingClientRect();
  return { text, x: rect.left + rect.width / 2, y: rect.bottom };
}

/**
 * A small menu below text selected in a bot's reply: Shorten, Improve or Explain. Choosing one sends the bot a message
 * that quotes the selection. Pressing the buttons keeps the selection until the click lands.
 */
export function SelectionMenu({ botId, root }: { botId: string; root: RefObject<HTMLElement | null> }) {
  const [selected, setSelected] = useState<Selected | null>(null);

  useEffect(() => {
    const update = () => setSelected(selectedReplyText(root.current));
    const hide = () => setSelected(null);
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") hide(); };
    document.addEventListener("selectionchange", update);
    document.addEventListener("keydown", onKey);
    const el = root.current;
    el?.addEventListener("scroll", hide, true);
    return () => {
      document.removeEventListener("selectionchange", update);
      document.removeEventListener("keydown", onKey);
      el?.removeEventListener("scroll", hide, true);
    };
  }, [root]);

  if (!selected) return null;
  const run = (ask: string) => {
    void useChatMessages.getState().send(botId, { text: withQuote(selected.text, ask) });
    window.getSelection()?.removeAllRanges();
    setSelected(null);
  };
  const width = 264;
  const left = Math.max(8, Math.min(window.innerWidth - width - 8, selected.x - width / 2));
  return (
    <div role="toolbar" aria-label="Ask about the selected text" data-slot="selection-menu" style={{ position: "fixed", left, top: selected.y + 8, width }}
      className="z-50 flex items-center gap-1 rounded-xl border bg-popover p-1 text-popover-foreground shadow-lg/10"
      onMouseDown={(event) => event.preventDefault()}>
      {ACTIONS.map(({ label, ask, icon: Icon }) => (
        <Button key={label} type="button" variant="ghost" size="sm" className="flex-1" onClick={() => run(ask)}><Icon aria-hidden />{label}</Button>
      ))}
    </div>
  );
}

