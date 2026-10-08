import { useCallback } from "react";
import GridLayout, { WidthProvider, type Layout } from "react-grid-layout/legacy";
import { useWorkspaces } from "@/stores/workspaces";
import { widgetByType } from "./registry";

const Grid = WidthProvider(GridLayout);

/** Draggable, resizable, persisted widget grid for the active workspace. */
export function WidgetGrid() {
  const { list, activeId, setWidgets, removeWidget, configureWidget } = useWorkspaces();
  const ws = list.find((w) => w.id === activeId);
  const onLayoutChange = useCallback(
    (l: Layout) => {
      if (!ws) return;
      const next = ws.layout.widgets.map((w) => {
        const p = l.find((x) => x.i === w.id);
        return p ? { ...w, x: p.x, y: p.y, w: p.w, h: p.h } : w;
      });
      if (next.some((w, i) => { const o = ws.layout.widgets[i]!; return o.x !== w.x || o.y !== w.y || o.w !== w.w || o.h !== w.h; })) setWidgets(next);
    },
    [ws, setWidgets],
  );
  if (!ws) return null;
  return (
    <Grid className="layout" layout={ws.layout.widgets.map((w) => ({ i: w.id, x: w.x, y: w.y, w: w.w, h: w.h, minW: 2, minH: 3 }))}
      cols={12} rowHeight={36} margin={[10, 10]} containerPadding={[0, 0]} draggableHandle=".widget-handle" draggableCancel="button,textarea,input,a"
      onLayoutChange={onLayoutChange} isResizable isDraggable>
      {ws.layout.widgets.map((w) => {
        const def = widgetByType(w.type);
        if (!def) return <div key={w.id} className="rounded-lg border bg-card p-3 text-muted-foreground">Unknown widget “{w.type}”<button className="ml-2 underline" onClick={() => removeWidget(w.id)}>remove</button></div>;
        const C = def.component;
        return <div key={w.id}><C config={w.config} onConfig={(p) => configureWidget(w.id, p)} onRemove={() => removeWidget(w.id)} /></div>;
      })}
    </Grid>
  );
}
