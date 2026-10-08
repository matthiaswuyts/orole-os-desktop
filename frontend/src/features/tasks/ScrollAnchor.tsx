import { useCallback, useLayoutEffect, useRef, useState } from "react";
import type { KeyboardEvent, UIEvent } from "react";
import { ArrowDown } from "lucide-react";
import { Button } from "@/components/ui/button";

const PIN_SLACK = 48;
interface Anchor { element: HTMLElement; key: string; offset: number }

/** Real-content anchoring: resizing above the reader never turns into a jump to the end. */
export function useScrollAnchor(revision: unknown, updateIds: readonly number[]) {
  const viewport = useRef<HTMLDivElement>(null);
  const content = useRef<HTMLDivElement>(null);
  const following = useRef(true);
  const anchor = useRef<Anchor | null>(null);
  const lastTop = useRef(0);
  const newest = useRef<number | null>(null);
  const [away, setAway] = useState(false);
  const [unseen, setUnseen] = useState(0);

  const remember = useCallback(() => {
    const el = viewport.current;
    if (!el) return;
    const top = el.getBoundingClientRect().top;
    const elements = content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]");
    const visible = elements && Array.from(elements).find((row) => row.getBoundingClientRect().bottom > top);
    anchor.current = visible ? { element: visible, key: visible.dataset.scrollAnchor!, offset: visible.getBoundingClientRect().top - top } : null;
    lastTop.current = el.scrollTop;
  }, []);

  const pause = useCallback(() => {
    following.current = false;
    setAway(true);
    remember();
  }, [remember]);

  const settle = useCallback(() => {
    const el = viewport.current;
    if (!el) return;
    if (following.current) el.scrollTop = el.scrollHeight;
    else if (anchor.current) {
      const row = anchor.current.element.isConnected ? anchor.current.element : Array.from(content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]") ?? []).find((item) => item.dataset.scrollAnchor === anchor.current!.key);
      const delta = row ? row.getBoundingClientRect().top - el.getBoundingClientRect().top - anchor.current.offset : 0;
      if (Math.abs(delta) > 0.5) el.scrollTop += delta;
    }
    lastTop.current = el.scrollTop;
    remember();
  }, [remember]);

  useLayoutEffect(() => {
    const latest = updateIds.length ? updateIds[updateIds.length - 1]! : null;
    if (!following.current && newest.current !== null) {
      const previous = newest.current;
      const count = updateIds.filter((id) => id > previous).length;
      if (count) setUnseen((n) => n + count);
    }
    newest.current = latest;
    settle();
  }, [revision, updateIds, settle]);

  useLayoutEffect(() => {
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(settle);
    if (content.current) observer.observe(content.current);
    if (viewport.current) observer.observe(viewport.current);
    // Observe rows as well: simultaneous growth/shrinkage may leave total height unchanged.
    content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]").forEach((row) => observer.observe(row));
    return () => observer.disconnect();
  }, [revision, settle]);

  const onScroll = useCallback((_event: UIEvent<HTMLDivElement>) => {
    const el = viewport.current;
    if (!el) return;
    // Ignore scroll events caused by our own anchor correction or explicit reveal.
    if (Math.abs(el.scrollTop - lastTop.current) < 0.5) return;
    const movedUp = el.scrollTop < lastTop.current - 1;
    if (movedUp) following.current = false;
    else if (el.scrollHeight - el.scrollTop - el.clientHeight < PIN_SLACK) following.current = true;
    setAway(!following.current);
    if (following.current) setUnseen(0);
    remember();
  }, [remember]);

  const jump = useCallback(() => {
    following.current = true;
    setAway(false);
    setUnseen(0);
    // Instant movement is intentional: no animated pursuit of a growing stream, including reduced motion.
    settle();
    viewport.current?.focus({ preventScroll: true });
  }, [settle]);

  const onKeyDown = useCallback((event: KeyboardEvent<HTMLDivElement>) => {
    if (event.target !== event.currentTarget) return;
    if (["ArrowUp", "PageUp", "Home"].includes(event.key)) pause();
    if (event.key === "End" && (event.ctrlKey || event.metaKey)) { event.preventDefault(); jump(); }
  }, [jump, pause]);

  const reveal = useCallback((key: string, hitId?: string) => {
    pause();
    const row = Array.from(content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]") ?? []).find((item) => item.dataset.scrollAnchor === key);
    const target = (hitId && Array.from(row?.querySelectorAll<HTMLElement>("[data-search-hit]") ?? []).find((hit) => hit.dataset.searchHit === hitId)) || row;
    const el = viewport.current;
    if (target && el) {
      el.scrollTop += target.getBoundingClientRect().top - el.getBoundingClientRect().top - el.clientHeight / 3;
      remember();
    }
  }, [pause, remember]);

  return { viewport, content, away, unseen, jump, pause, reveal, onScroll, onKeyDown };
}

export function ScrollAnchor({ unseen, onJump }: { unseen: number; onJump: () => void }) {
  return <Button size="sm" variant="outline" className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-background" onClick={onJump}>
    <ArrowDown aria-hidden data-icon="inline-start" />
    <span>{unseen ? `${unseen} new ${unseen === 1 ? "update" : "updates"}` : "Jump to latest"}</span>
    <span className="sr-only">{unseen ? " — jump to latest" : ""}</span>
  </Button>;
}
