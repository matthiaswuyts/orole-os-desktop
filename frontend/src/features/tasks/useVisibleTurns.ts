import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";
import type { MapTurn } from "@/lib/conversationMap";

interface Params {
  viewport: RefObject<HTMLElement | null>;
  content: RefObject<HTMLElement | null>;
  turns: readonly MapTurn[];
  /** Entry index of a row's scroll-anchor key, and the turn of each entry (see mapTurns). */
  indexOfKey: ReadonlyMap<string, number>;
  turnOfEntry: readonly number[];
}

const NONE = { activeId: undefined as string | undefined, visibleIds: [] as string[] };

/**
 * Which turns the scroll viewport shows and which one is being read: the turn under a line a third of the way down, or the
 * last turn while the reader is at the end. It reads the rendered rows, so a windowed list only reports what is mounted.
 */
export function useVisibleTurns({ viewport, content, turns, indexOfKey, turnOfEntry }: Params) {
  const [state, setState] = useState(NONE);
  const latest = useRef({ turns, indexOfKey, turnOfEntry });
  useLayoutEffect(() => { latest.current = { turns, indexOfKey, turnOfEntry }; });

  const measure = useCallback(() => {
    const el = viewport.current;
    if (!el) return;
    const { turns: all, indexOfKey: index, turnOfEntry: turnOf } = latest.current;
    const box = el.getBoundingClientRect();
    const pivot = box.top + box.height * 0.3;
    const visible = new Set<number>();
    let reading = -1;
    let first = -1;
    for (const row of content.current?.querySelectorAll<HTMLElement>("[data-scroll-anchor]") ?? []) {
      const rect = row.getBoundingClientRect();
      if (rect.bottom <= box.top || rect.top >= box.bottom) continue;
      const entry = index.get(row.dataset.scrollAnchor ?? "");
      const turn = entry === undefined ? undefined : turnOf[entry];
      if (turn === undefined) continue;
      visible.add(turn);
      if (first < 0) first = turn;
      if (rect.top <= pivot) reading = turn;
    }
    if (reading < 0) reading = first;
    if (el.scrollHeight - el.scrollTop - el.clientHeight < 8 && all.length) reading = all.length - 1;
    const activeId = all[reading]?.id;
    const visibleIds = Array.from(visible).sort((a, b) => a - b).flatMap((turn) => all[turn]?.id ?? []);
    setState((prior) => prior.activeId === activeId && prior.visibleIds.length === visibleIds.length && prior.visibleIds.every((id, i) => id === visibleIds[i]) ? prior : { activeId, visibleIds });
  }, [viewport, content]);

  // One measurement per frame while scrolling or resizing.
  useEffect(() => {
    const el = viewport.current;
    if (!el) return;
    let frame = 0;
    const schedule = () => { if (!frame) frame = requestAnimationFrame(() => { frame = 0; measure(); }); };
    el.addEventListener("scroll", schedule, { passive: true });
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(schedule);
    observer?.observe(el);
    if (content.current) observer?.observe(content.current);
    schedule();
    return () => { el.removeEventListener("scroll", schedule); observer?.disconnect(); if (frame) cancelAnimationFrame(frame); };
  }, [viewport, content, measure]);

  // New or reshaped turns change what is on screen without a scroll.
  useLayoutEffect(() => { measure(); }, [measure, turns, indexOfKey, turnOfEntry]);

  return state;
}
