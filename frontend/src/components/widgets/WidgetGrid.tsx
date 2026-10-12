import { Suspense, useMemo, type CSSProperties, type PointerEvent as ReactPointerEvent } from "react";
import { closestCenter, DndContext, KeyboardSensor, MeasuringStrategy, PointerSensor, useSensor, useSensors, type Announcements, type DragEndEvent } from "@dnd-kit/core";
import { rectSortingStrategy, SortableContext, sortableKeyboardCoordinates, useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useDashboard, type WidgetInstance } from "@/stores/dashboard";
import { cn } from "@/lib/utils";
import { useWidgetRegistry, type WidgetDef } from "./registry";
import { Widget } from "./Widget";
import { fillRows, TWO_COLUMN_SPAN, WIDE_SPAN, WidgetFrameContext, type WidgetFrame } from "./frame";

/**
 * Column spans per layout: one column on narrow windows, two from `@2xl`, twelve from `@4xl`. The
 * spans come from `fillRows`, so a row whose sizes add up to less than the grid still ends at the
 * column's right edge (its last widget widens).
 */
const SPAN = "col-span-12 @2xl:col-(--span-two) @4xl:col-(--span-wide)";
type Spans = { two: number; wide: number };

/**
 * Starts a drag from a widget heading, but not from the controls in it (menu, range pickers):
 * those keep their own click. The move handle itself is a button and does start a drag.
 */
class HeadingPointerSensor extends PointerSensor {
  static activators = [{
    eventName: "onPointerDown" as const,
    handler: ({ nativeEvent: event }: ReactPointerEvent) => {
      if (!event.isPrimary || event.button !== 0) return false;
      const control = (event.target as Element | null)?.closest("button,a,input,textarea,select,[role=menuitem],[role=combobox]");
      return !control || control.getAttribute("aria-roledescription") === "sortable";
    },
  }];
}

function SortableWidget({ item, def, spans }: { item: WidgetInstance; def: WidgetDef | undefined; spans: Spans }) {
  const { removeWidget, configureWidget, resizeWidget, setTall } = useDashboard.getState();
  const { attributes, listeners, setNodeRef, setActivatorNodeRef, transform, transition, isDragging } = useSortable({ id: item.id });
  const frame = useMemo<WidgetFrame>(() => ({
    size: item.size,
    tall: !!item.tall,
    onSize: (size) => resizeWidget(item.id, size),
    onTall: (tall) => setTall(item.id, tall),
    attachHandle: setActivatorNodeRef,
    attributes,
    listeners,
  }), [item.id, item.size, item.tall, resizeWidget, setTall, setActivatorNodeRef, attributes, listeners]);
  const remove = () => removeWidget(item.id);
  const C = def?.component;
  return (
    <div ref={setNodeRef} data-widget-id={item.id}
      style={{ transform: CSS.Translate.toString(transform), transition, "--span-two": `span ${spans.two} / span ${spans.two}`, "--span-wide": `span ${spans.wide} / span ${spans.wide}` } as CSSProperties}
      className={cn("relative min-w-0", SPAN, item.tall ? "row-span-2 min-h-[30rem]" : item.size === "full" ? "" : "min-h-56",
        isDragging && "z-10 rounded-xl opacity-90 shadow-2xl ring-1 ring-ring/40")}>
      <WidgetFrameContext.Provider value={frame}>
        {!def || !C
          ? <Widget title="Unavailable widget" status="unconfigured" message={`“${item.type}” is unavailable. Its plugin may be disabled, removed, or still loading. Your layout is unchanged.`} onRemove={remove} action={{ label: "Remove widget", onClick: remove }} />
          : <Suspense fallback={<Widget title={def.title} status="loading" onRemove={remove} />}>
              <C widgetType={item.type} config={item.config} onConfig={(patch) => configureWidget(item.id, patch)} onRemove={remove} />
            </Suspense>}
      </WidgetFrameContext.Provider>
    </div>
  );
}

/** Home's widgets in a 12-column grid: drag a heading (or Space on its move handle) to reorder; the widget menu sets its size. Order and sizes persist. */
export function WidgetGrid() {
  const items = useDashboard((s) => s.widgets);
  const moveWidget = useDashboard((s) => s.moveWidget);
  const widgets = useWidgetRegistry();
  const spans = useMemo(() => {
    const two = fillRows(items.map((w) => ({ span: TWO_COLUMN_SPAN[w.size], tall: w.tall })));
    const wide = fillRows(items.map((w) => ({ span: WIDE_SPAN[w.size], tall: w.tall })));
    return items.map((_, i) => ({ two: two[i]!, wide: wide[i]! }));
  }, [items]);
  const sensors = useSensors(
    useSensor(HeadingPointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );
  const titleOf = (id: string | number) => {
    const type = items.find((w) => w.id === id)?.type;
    return widgets.find((w) => w.type === type)?.title ?? "Widget";
  };
  const position = (id: string | number) => items.findIndex((w) => w.id === id) + 1;
  const announcements: Announcements = {
    onDragStart: ({ active }) => `Picked up ${titleOf(active.id)}, position ${position(active.id)} of ${items.length}.`,
    onDragOver: ({ active, over }) => over ? `${titleOf(active.id)} moved to position ${position(over.id)} of ${items.length}.` : `${titleOf(active.id)} is no longer over a position.`,
    onDragEnd: ({ active, over }) => over ? `${titleOf(active.id)} dropped at position ${position(over.id)} of ${items.length}.` : `${titleOf(active.id)} dropped.`,
    onDragCancel: ({ active }) => `Moving ${titleOf(active.id)} was cancelled.`,
  };
  const onDragEnd = ({ active, over }: DragEndEvent) => {
    if (over && active.id !== over.id) moveWidget(String(active.id), String(over.id));
  };
  return (
    <DndContext sensors={sensors} collisionDetection={closestCenter} measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
      accessibility={{ announcements, screenReaderInstructions: { draggable: "To move a widget, press Space or Enter on its move handle, use the arrow keys, then Space or Enter to drop it. Escape cancels." } }}
      onDragEnd={onDragEnd}>
      <SortableContext items={items.map((w) => w.id)} strategy={rectSortingStrategy}>
        <div className="@container">
          <div className="grid grid-cols-12 gap-3">
            {items.map((item, i) => <SortableWidget key={item.id} item={item} def={widgets.find((w) => w.type === item.type)} spans={spans[i]!} />)}
          </div>
        </div>
      </SortableContext>
    </DndContext>
  );
}
