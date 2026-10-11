import { LightbulbIcon, MessageSquarePlusIcon, MinimizeIcon, SparklesIcon, type LucideIcon } from "lucide-react";
import { Fragment, useEffect, useState, type RefObject } from "react";
import { Button } from "@/components/ui/button";
import { Group, GroupSeparator } from "@/components/ui/group";
import { useChatMessages } from "@/stores/chatMessages";
import { withQuote } from "./quote";

const ACTIONS: { label: string; ask: string; icon: LucideIcon }[] = [
  { label: "Shorten", ask: "Shorten this selected text.", icon: MinimizeIcon },
  { label: "Improve", ask: "Improve this selected text.", icon: SparklesIcon },
  { label: "Explain", ask: "Explain this selected text.", icon: LightbulbIcon },
];

/** How long a keyboard selection must stay still before the menu appears. */
const SETTLE_MS = 350;

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
 * A small menu below text selected in a bot's reply, fading in under the selection: Add to chat (quotes the passage into
 * the message box without sending), or Shorten, Improve and Explain, which send the bot the quoted passage with that
 * request. Pressing the buttons keeps the selection until the click lands.
 */
export function SelectionMenu({ botId, root }: { botId: string; root: RefObject<HTMLElement | null> }) {
  const [selected, setSelected] = useState<Selected | null>(null);

  useEffect(() => {
    const hide = () => setSelected(null);
    // The menu waits until the user has finished selecting: while the mouse is held it stays hidden, and after a
    // keyboard selection (or any other change) it appears once the selection has been still for a moment.
    let dragging = false;
    let releasing = false; // the mouse was just released: its own show is on its way
    let timer: ReturnType<typeof setTimeout> | undefined;
    const show = () => { clearTimeout(timer); setSelected(selectedReplyText(root.current)); };
    const onSelectionChange = () => {
      if (dragging) { hide(); return; }
      if (releasing) return;
      clearTimeout(timer);
      // Keep the menu while the same text stays selected; hide it as soon as the selection becomes another.
      const now = selectedReplyText(root.current);
      setSelected((prev) => (prev && now && prev.text === now.text ? prev : null));
      timer = setTimeout(show, SETTLE_MS);
    };
    const onMouseDown = (event: MouseEvent) => {
      // A press on the menu itself must not hide it; anywhere else starts a new selection.
      if (event.target instanceof Element && event.target.closest("[data-slot=selection-menu]")) return;
      dragging = true;
      clearTimeout(timer);
      hide();
    };
    const onMouseUp = () => {
      if (!dragging) return;
      dragging = false;
      releasing = true;
      clearTimeout(timer);
      timer = setTimeout(() => { releasing = false; show(); }, 0); // after the browser has settled the selection
    };
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") { clearTimeout(timer); hide(); } };
    document.addEventListener("selectionchange", onSelectionChange);
    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("mouseup", onMouseUp);
    document.addEventListener("keydown", onKey);
    const el = root.current;
    el?.addEventListener("scroll", hide, true);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("selectionchange", onSelectionChange);
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("mouseup", onMouseUp);
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
  const addToChat = () => {
    const store = useChatMessages.getState();
    const draft = store.drafts[botId]?.text ?? "";
    store.setDraft(botId, { text: `${draft ? `${draft.trimEnd()}\n\n` : ""}${withQuote(selected.text, "")}` });
    window.getSelection()?.removeAllRanges();
    setSelected(null);
    requestAnimationFrame(() => {
      const box = document.querySelector<HTMLTextAreaElement>("[data-slot=composer] textarea");
      box?.focus();
      box?.setSelectionRange(box.value.length, box.value.length);
    });
  };
  const width = 372;
  const left = Math.max(8, Math.min(window.innerWidth - width - 8, selected.x - width / 2));
  return (
    <div data-slot="selection-menu" style={{ position: "fixed", left, top: selected.y + 8 }} className="z-50" onMouseDown={(event) => event.preventDefault()}>
      <Group aria-label="Ask about the selected text" className="fade-in-0 slide-in-from-top-1 zoom-in-95 animate-in rounded-lg bg-popover shadow-lg/10 duration-150 ease-out motion-reduce:animate-none">
        <Button type="button" variant="outline" size="sm" onClick={addToChat}><MessageSquarePlusIcon aria-hidden className="size-3.5" />Add to chat</Button>
        {ACTIONS.map(({ label, ask, icon: Icon }) => (
          <Fragment key={label}>
            <GroupSeparator />
            <Button type="button" variant="outline" size="sm" onClick={() => run(ask)}><Icon aria-hidden className="size-3.5" />{label}</Button>
          </Fragment>
        ))}
      </Group>
    </div>
  );
}

