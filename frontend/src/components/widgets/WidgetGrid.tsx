import { Suspense, useCallback } from "react";
import GridLayout, { WidthProvider, type Layout } from "react-grid-layout/legacy";
import { useWorkspaces } from "@/stores/workspaces";
import { useWidgetRegistry } from "./registry";
import { Widget } from "./Widget";

const Grid = WidthProvider(GridLayout);

/** Draggable, resizable, persisted widget grid for the active workspace. */
export function WidgetGrid() {
  const { list, activeId, setWidgets, removeWidget, configureWidget } = useWorkspaces();
  const widgets = useWidgetRegistry();
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
      cols={12} rowHeight={36} margin={[10, 10]} containerPadding={[0, 0]} draggableHandle=".widget-handle" draggableCancel="button,textarea,input,a,summary,pre"
      onLayoutChange={onLayoutChange} isResizable isDraggable>
      {ws.layout.widgets.map((w) => {
        const def = widgets.find((widget) => widget.type === w.type);
        if (!def) return <div key={w.id}><Widget title="Unavailable widget" status="unconfigured" message={`“${w.type}” is unavailable. Its plugin may be disabled, removed, or still loading. Your layout is unchanged.`} onRemove={() => removeWidget(w.id)} action={{ label: "Remove widget", onClick: () => removeWidget(w.id) }} /></div>;
        const C = def.component;
        return (
          <div key={w.id}>
            <Suspense fallback={<Widget title={def.title} status="loading" onRemove={() => removeWidget(w.id)} />}>
              <C widgetType={w.type} config={w.config} onConfig={(p) => configureWidget(w.id, p)} onRemove={() => removeWidget(w.id)} />
            </Suspense>
          </div>
        );
      })}
    </Grid>
  );
}
