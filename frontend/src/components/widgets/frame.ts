import { createContext } from "react";
import type { DraggableAttributes, DraggableSyntheticListeners } from "@dnd-kit/core";

/** Column span of a widget on the 12-column Home grid (narrow windows stack every widget full width). */
export type WidgetSize = "s" | "m" | "l" | "full";

export const WIDGET_SIZES: readonly { id: WidgetSize; label: string; span: number }[] = [
  { id: "s", label: "Small", span: 4 },
  { id: "m", label: "Medium", span: 6 },
  { id: "l", label: "Large", span: 8 },
  { id: "full", label: "Full width", span: 12 },
];

/** Grid columns each size spans when the Home grid shows two columns (under ~900px wide). */
export const TWO_COLUMN_SPAN: Readonly<Record<WidgetSize, number>> = { s: 6, m: 6, l: 12, full: 12 };
export const WIDE_SPAN: Readonly<Record<WidgetSize, number>> = Object.fromEntries(WIDGET_SIZES.map((size) => [size.id, size.span])) as Record<WidgetSize, number>;

/**
 * Column spans that make every row of a `cols`-column grid end at its right edge. It places the
 * items the way CSS grid's sparse row auto-placement does (a tall item takes two rows), then widens
 * each item over the free cells to its right, so a Medium + Small row (10 of 12 columns) gets a
 * wider Small instead of a ragged edge. Widening only takes cells that stay empty, so the browser
 * places every item at the same spot with the returned spans.
 */
export function fillRows(items: readonly { span: number; tall?: boolean }[], cols = 12): number[] {
  const taken: boolean[][] = [];
  const free = (row: number, col: number, height: number) => {
    for (let r = row; r < row + height; r++) if (taken[r]?.[col]) return false;
    return true;
  };
  const take = (row: number, col: number, height: number) => {
    for (let r = row; r < row + height; r++) (taken[r] ??= [])[col] = true;
  };
  const placed: { row: number; col: number; span: number; height: number }[] = [];
  let row = 0, col = 0;
  for (const item of items) {
    const span = Math.min(Math.max(1, item.span), cols), height = item.tall ? 2 : 1;
    for (;;) {
      if (col + span > cols) { row++; col = 0; continue; }
      let fits = true;
      for (let c = col; c < col + span && fits; c++) fits = free(row, c, height);
      if (fits) break;
      col++;
    }
    for (let c = col; c < col + span; c++) take(row, c, height);
    placed.push({ row, col, span, height });
    col += span;
  }
  return placed.map((p) => {
    let end = p.col + p.span;
    while (end < cols && free(p.row, end, p.height)) take(p.row, end++, p.height);
    return end - p.col;
  });
}

/** What the Home grid gives the widget it hosts: the drag handle and the size controls. */
export interface WidgetFrame {
  size: WidgetSize;
  tall: boolean;
  onSize: (size: WidgetSize) => void;
  onTall: (tall: boolean) => void;
  /** Registers the move handle (keyboard activator, focus return after a drag). */
  attachHandle: (element: HTMLElement | null) => void;
  attributes: DraggableAttributes;
  listeners: DraggableSyntheticListeners;
}

export const WidgetFrameContext = createContext<WidgetFrame | null>(null);
