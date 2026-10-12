import { useContext, type ReactNode } from "react";
import { AlertTriangle, EllipsisIcon, GripVertical, PlugZap, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { Menu, MenuCheckboxItem, MenuGroup, MenuGroupLabel, MenuItem, MenuPopup, MenuRadioGroup, MenuRadioItem, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { WIDGET_SIZES, WidgetFrameContext, type WidgetFrame, type WidgetSize } from "./frame";
import { cn } from "@/lib/utils";

export type WidgetStatus = "ready" | "loading" | "error" | "offline" | "unconfigured";

interface Props {
  title: string;
  icon?: ReactNode;
  status?: WidgetStatus;
  message?: string;
  action?: { label: string; onClick: () => void };
  onRemove?: () => void;
  count?: ReactNode;
  /** Controls in the heading, before the menu (filters, range pickers). */
  actions?: ReactNode;
  /** `card` (default) insets the body in a panel; `bare` drops the chrome for section-like widgets. */
  variant?: "card" | "bare";
  children?: ReactNode;
  className?: string;
}

const iconButton = "inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring";
const revealed = "opacity-0 focus-visible:opacity-100 group-focus-within/w:opacity-100 group-hover/w:opacity-100 data-popup-open:opacity-100";

/** The keyboard-reachable drag handle; pointer drags start anywhere on the heading. */
function MoveHandle({ attach, attributes, title, bare }: { attach: WidgetFrame["attachHandle"]; attributes: WidgetFrame["attributes"]; title: string; bare: boolean }) {
  return (
    <button type="button" ref={attach} {...attributes} aria-label={`Move ${title}`}
      className={cn(iconButton, revealed, "-mr-1 w-4 cursor-grab text-muted-foreground/70 active:cursor-grabbing", bare && "-ml-5")}>
      <GripVertical className="size-3.5" />
    </button>
  );
}

/**
 * Widget card: a heading row (drag handle, icon, title, count, actions, menu) over an inset body
 * panel. Shared loading / error / offline / not-connected states so no widget invents its own.
 * Inside the Home grid the heading drags the widget and the menu sets its size.
 */
export function Widget({ title, icon, status = "ready", message, action, onRemove, count, actions, variant = "card", children, className }: Props) {
  const frame = useContext(WidgetFrameContext);
  const bare = variant === "bare";
  return (
    <section aria-label={title} className={cn("group/w flex h-full min-h-0 flex-col", bare ? "pb-6" : "overflow-hidden rounded-xl border bg-card p-1 shadow-xs/5", className)}>
      <header {...frame?.listeners} className={cn("widget-handle flex shrink-0 touch-none items-center gap-2", frame && "cursor-grab active:cursor-grabbing", bare ? "mb-4 h-8" : "h-9 pr-1 pl-2")}>
        {frame && <MoveHandle attach={frame.attachHandle} attributes={frame.attributes} title={title} bare={bare} />}
        {icon && !bare && <span className="text-muted-foreground [&_svg]:size-3.5">{icon}</span>}
        <h2 className={cn("truncate", bare ? "font-heading font-semibold text-base tracking-tight" : "font-medium text-[13px]")}>{title}</h2>
        {count}
        <span className="flex-1" />
        {actions}
        {(onRemove || frame) && (
          <Menu>
            <MenuTrigger render={<button type="button" aria-label={`${title} options`} className={cn(iconButton, revealed, "data-popup-open:bg-accent")} />}>
              <EllipsisIcon className="size-3.5" />
            </MenuTrigger>
            <MenuPopup align="end" className="min-w-44">
              {frame && <>
                <MenuGroup>
                  <MenuGroupLabel>Width</MenuGroupLabel>
                  <MenuRadioGroup value={frame.size} onValueChange={(value: WidgetSize) => frame.onSize(value)}>
                    {WIDGET_SIZES.map((size) => <MenuRadioItem key={size.id} value={size.id}>{size.label}</MenuRadioItem>)}
                  </MenuRadioGroup>
                </MenuGroup>
                <MenuSeparator />
                <MenuCheckboxItem checked={frame.tall} onCheckedChange={(checked) => frame.onTall(checked)}>Tall</MenuCheckboxItem>
                {onRemove && <MenuSeparator />}
              </>}
              {onRemove && <MenuItem variant="destructive" onClick={onRemove}>Remove widget</MenuItem>}
            </MenuPopup>
          </Menu>
        )}
      </header>
      <div className={cn("min-h-0 flex-1 text-sm", bare ? "" : "overflow-auto rounded-lg border border-border/70 bg-foreground/[0.035] p-3")}>
        {status === "ready" ? children : <WidgetState status={status} message={message} action={action} />}
      </div>
    </section>
  );
}

export function WidgetState({ status, message, action }: { status: Exclude<WidgetStatus, "ready">; message?: string; action?: Props["action"] }) {
  const map = {
    loading: { icon: <Spinner size={20} />, text: "Loading…" },
    error: { icon: <AlertTriangle className="text-destructive-foreground" />, text: "Something went wrong" },
    offline: { icon: <WifiOff />, text: "Offline" },
    unconfigured: { icon: <PlugZap />, text: "Not connected" },
  }[status];
  return (
    <div role={status === "error" ? "alert" : "status"} className="flex h-full min-h-28 flex-col items-center justify-center gap-1.5 py-4 text-center text-muted-foreground">
      <span className="[&_svg]:size-5">{map.icon}</span>
      <p className="text-foreground/90">{map.text}</p>
      {message && <p className="max-w-[36ch] text-xs">{message}</p>}
      {action && <Button variant="outline" size="xs" className="mt-1" onClick={action.onClick}>{action.label}</Button>}
    </div>
  );
}
