import { useId, useRef, useState, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { cn } from "@/lib/utils";

/** Single-selection list with bounded DOM size and keyboard navigation across virtual rows. */
export function VirtualList<T extends { id: string }>({ items, selected, onSelect, label, rowHeight = 64, render }: { items: T[]; selected: string; onSelect: (item: T) => void; label: string; rowHeight?: number; render: (item: T) => ReactNode }) {
  const parent = useRef<HTMLDivElement>(null);
  const listId = useId();
  const [activeId, setActiveId] = useState("");
  const virtual = useVirtualizer({ count: items.length, getScrollElement: () => parent.current, estimateSize: () => rowHeight, overscan: 6 });
  const rows = virtual.getVirtualItems();
  const activeIndex = Math.max(0, items.findIndex((item) => item.id === (activeId || selected)));
  return <div ref={parent} role="listbox" aria-label={label} aria-activedescendant={rows.some((row) => row.index === activeIndex) ? `${listId}-${activeIndex}` : undefined} tabIndex={0} className="min-h-0 flex-1 overflow-auto outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring" onKeyDown={(event) => {
    if (!items.length) return;
    if (event.key === "Enter" || event.key === " ") { event.preventDefault(); onSelect(items[activeIndex]!); return; }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
    event.preventDefault();
    const index = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : Math.max(0, Math.min(items.length - 1, activeIndex + (event.key === "ArrowDown" ? 1 : -1)));
    virtual.scrollToIndex(index); setActiveId(items[index]!.id);
  }}>
    <div className="relative w-full" style={{ height: virtual.getTotalSize() }}>
      {rows.map((row) => { const item = items[row.index]!; return <div id={`${listId}-${row.index}`} key={item.id} role="option" aria-selected={item.id === selected} aria-posinset={row.index + 1} aria-setsize={items.length} onClick={() => { parent.current?.focus(); setActiveId(item.id); onSelect(item); }} className={cn("absolute inset-x-0 cursor-pointer overflow-hidden border-b px-3 py-2 hover:bg-accent", selected === item.id && "bg-accent", row.index === activeIndex && "outline-1 -outline-offset-1 outline-ring")} style={{ height: row.size, transform: `translateY(${row.start}px)` }}>{render(item)}</div>; })}
    </div>
  </div>;
}
