import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { lazy, Suspense, useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import { useChat, type ChatPane } from "@/stores/chat";
import { DocumentPane } from "./DocumentPane";

// Split out like the builder: the task feed and composer load only when a task is opened beside the conversation. ChatView
// warms the chunk on idle so the first open never waits on the network.
const TaskPane = lazy(() => import("./TaskPane").then((m) => ({ default: m.TaskPane })));

/** The `@3xl` container step (48rem): from there the pane sits beside the conversation, below it lies over it. */
const WIDE_REM = 48;
const OPEN_MS = 220;
const paneKey = (pane: ChatPane) => pane.kind === "task" ? `task:${pane.id}` : `document:${pane.messageId}:${pane.partIndex}`;

/** The chat container's width, so the pane knows whether it splits the view or overlays it and how wide to grow. */
function useContainerWidth(container: RefObject<HTMLElement | null>): number {
  const [width, setWidth] = useState(() => container.current?.clientWidth ?? 0);
  useLayoutEffect(() => {
    const node = container.current;
    if (!node) return;
    setWidth(node.clientWidth);
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(([entry]) => { if (entry) setWidth(entry.contentRect.width); });
    observer.observe(node);
    return () => observer.disconnect();
  }, [container]);
  return width;
}

/**
 * What sits beside the conversation: a code task or a document. One host that stays mounted, so opening slides or widens
 * it with an exit on close, and switching between panes crossfades the content instead of remounting the shell. The heavy
 * task body waits for the open animation (`ready`); focus moves to the pane's heading once it has settled and returns
 * to the opener on close.
 */
export function SidePane({ botId, container }: { botId: string; container: RefObject<HTMLDivElement | null> }) {
  const pane = useChat((s) => s.pane);
  const reduced = useReducedMotion();
  const containerWidth = useContainerWidth(container);
  const rem = typeof document === "undefined" ? 16 : parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
  const wide = containerWidth >= WIDE_REM * rem;
  const width = Math.max(24 * rem, Math.round(containerWidth * 0.45));
  const [ready, setReady] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const open = pane !== null;
  // The opener is a row in the thread; it stays put, so focus moves to the pane once it is in place and back on close.
  useEffect(() => {
    if (open) { opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; return; }
    setReady(false);
    opener.current?.focus();
    opener.current = null;
  }, [open]);
  // The animation's own completion is the signal; the timer covers an environment that never animates.
  useEffect(() => {
    if (!open || ready) return;
    const timer = window.setTimeout(() => setReady(true), reduced ? 0 : OPEN_MS + 40);
    return () => window.clearTimeout(timer);
  }, [open, ready, reduced]);
  useEffect(() => { if (ready) root.current?.querySelector<HTMLElement>("h2[tabindex]")?.focus({ preventScroll: true }); }, [ready]);
  const close = () => useChat.getState().closePane();
  const transition = reduced ? { duration: 0 } : { duration: OPEN_MS / 1000, ease: "easeOut" as const };
  return <AnimatePresence initial={false}>
    {pane && <motion.div key="side-pane" ref={root} data-slot="side-pane" data-layout={wide ? "split" : "overlay"}
      className={wide ? "relative z-10 shrink-0 overflow-hidden border-l bg-background" : "absolute inset-y-0 right-0 z-20 w-full max-w-[34rem] border-l bg-background shadow-xl"}
      initial={wide ? { width: 0 } : { x: "100%" }}
      animate={wide ? { width } : { x: 0 }}
      exit={wide ? { width: 0 } : { x: "100%" }}
      transition={transition}
      onAnimationComplete={() => { if (useChat.getState().pane) setReady(true); }}>
      <div className="h-full" style={wide ? { width } : undefined}>
        <AnimatePresence mode="wait" initial={false}>
          <motion.div key={paneKey(pane)} className="h-full" initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} transition={reduced ? { duration: 0 } : { duration: 0.12 }}>
            {pane.kind === "task"
              ? <Suspense fallback={null}><TaskPane taskId={pane.id} ready={ready} onClose={close} /></Suspense>
              : <DocumentPane botId={botId} messageId={pane.messageId} partIndex={pane.partIndex} onClose={close} />}
          </motion.div>
        </AnimatePresence>
      </div>
    </motion.div>}
  </AnimatePresence>;
}
