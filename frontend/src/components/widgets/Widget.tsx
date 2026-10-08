import type { ReactNode } from "react";
import { AlertTriangle, EllipsisIcon, Loader2, PlugZap, WifiOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";
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
  children?: ReactNode;
  className?: string;
}

/**
 * Widget card, after Shipyard's views/home/widgets/card.tsx: one bordered surface with a 36px
 * heading (icon, title, count, menu) and a body. Shared loading / error / offline / not-connected
 * states so no widget invents its own.
 */
export function Widget({ title, icon, status = "ready", message, action, onRemove, count, children, className }: Props) {
  return (
    <section aria-label={title} className={cn("group/w flex h-full min-h-0 flex-col overflow-hidden rounded-lg border bg-card", className)}>
      <header className="widget-handle flex h-9 shrink-0 cursor-grab items-center gap-2 pr-1.5 pl-3 active:cursor-grabbing">
        <span className="text-muted-foreground [&_svg]:size-3.5">{icon}</span>
        <h2 className="truncate font-medium text-[13px]">{title}</h2>
        {count}
        <span className="flex-1" />
        {onRemove && (
          <Menu>
            <MenuTrigger render={<button type="button" aria-label={`${title} options`} onMouseDown={(e) => e.stopPropagation()}
              className="inline-flex size-6 shrink-0 items-center justify-center rounded-md text-muted-foreground opacity-0 outline-none hover:bg-accent hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring group-focus-within/w:opacity-100 group-hover/w:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100" />}>
              <EllipsisIcon className="size-3.5" />
            </MenuTrigger>
            <MenuPopup align="end" className="min-w-40">
              <MenuItem variant="destructive" onClick={onRemove}>Remove widget</MenuItem>
            </MenuPopup>
          </Menu>
        )}
      </header>
      <div className="min-h-0 flex-1 overflow-auto px-3 pb-3 text-sm">
        {status === "ready" ? children : <WidgetState status={status} message={message} action={action} />}
      </div>
    </section>
  );
}

export function WidgetState({ status, message, action }: { status: Exclude<WidgetStatus, "ready">; message?: string; action?: Props["action"] }) {
  const map = {
    loading: { icon: <Loader2 className="animate-spin" />, text: "Loading…" },
    error: { icon: <AlertTriangle className="text-destructive-foreground" />, text: "Something went wrong" },
    offline: { icon: <WifiOff />, text: "Offline" },
    unconfigured: { icon: <PlugZap />, text: "Not connected" },
  }[status];
  return (
    <div role={status === "error" ? "alert" : "status"} className="flex h-full flex-col items-center justify-center gap-1.5 text-center text-muted-foreground">
      <span className="[&_svg]:size-5">{map.icon}</span>
      <p className="text-foreground/90">{map.text}</p>
      {message && <p className="max-w-[30ch] text-xs">{message}</p>}
      {action && <Button variant="outline" size="xs" className="mt-1" onClick={action.onClick}>{action.label}</Button>}
    </div>
  );
}
