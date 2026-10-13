import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Bell, Check, CheckCheck, CircleAlert, CircleCheck, Ellipsis, RefreshCw, X } from "lucide-react";
import { useNotifications } from "@/stores/notifications";
import { useDaemon } from "@/stores/daemon";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Popover, PopoverClose, PopoverDescription, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Menu, MenuCheckboxItem, MenuItem, MenuPopup, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Skeleton } from "@/components/ui/skeleton";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";

const SourceFilter = lazy(() => import("./SourceFilter"));

export function NotificationCenter() {
  const [open, setOpen] = useState(false);
  const cornerAnchor = useRef<HTMLSpanElement>(null);
  const n = useNotifications();
  const link = useDaemon((state) => state.link);
  useEffect(() => n.startSync(), [n.startSync]);
  const offline = link === "offline";
  const unavailable = !inDesktop || offline;
  const countKnown = n.loaded && !n.error && !unavailable;
  const matchingUnread = n.source ? (n.sources.find((source) => source.source === n.source)?.unread ?? 0) : n.unread;
  const sourceItems = [{ value: "", label: "All" }, ...Array.from(new Set([...n.sources.map((source) => source.source), ...(n.source ? [n.source] : [])])).map((source) => ({ value: source, label: source }))];

  return (
    <Popover open={open} onOpenChange={(value) => { setOpen(value); if (value) void n.load(); }}>
      <span ref={cornerAnchor} aria-hidden="true" className="pointer-events-none fixed right-3 top-[var(--titlebar-h)] size-px" />
      <Tooltip><TooltipTrigger render={<PopoverTrigger render={<Button variant="ghost" size="icon-xs" className="relative" />} />} aria-label={countKnown ? `Notifications, ${n.unread} unread` : "Notifications, unread count unavailable"}>
        <Bell />
        {countKnown && n.unread > 0 && <Badge size="sm" className="absolute -right-1 -top-1" aria-hidden="true">{n.unread > 99 ? "99+" : n.unread}</Badge>}
      </TooltipTrigger><TooltipPopup>Notifications</TooltipPopup></Tooltip>
      <PopoverPopup anchor={cornerAnchor} align="end" sideOffset={8} className="w-[min(420px,calc(100vw-24px))] rounded-xl [&>[data-slot=popover-viewport]]:p-0">
        <div className="flex max-h-[min(440px,var(--available-height))] flex-col">
          <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b px-4 py-3">
            <PopoverTitle className="text-sm">Notifications</PopoverTitle>
            {inDesktop && <Suspense fallback={null}><SourceFilter value={n.source} items={sourceItems} onChange={(value) => n.filter(value, n.unreadOnly)} /></Suspense>}
            <div className="ml-auto flex items-center gap-1">
              {inDesktop && <><Button size="xs" variant="ghost" className="text-muted-foreground" disabled={unavailable || n.busy || n.loading || matchingUnread === 0} onClick={() => void n.markRead([], true)}><CheckCheck />{n.source ? "Mark source read" : "Mark all read"}</Button>
                <Menu><MenuTrigger render={<Button size="icon-xs" variant="ghost" aria-label="Notification options" />}><Ellipsis /></MenuTrigger>
                  <MenuPopup align="end">
                    <MenuCheckboxItem checked={n.unreadOnly} onCheckedChange={(checked) => n.filter(n.source, checked)}>Unread only</MenuCheckboxItem>
                    <MenuItem disabled={n.loading} onClick={() => { void useDaemon.getState().refresh(); void n.load(); }}><RefreshCw />Refresh notifications</MenuItem>
                    <MenuSeparator />
                    <MenuItem disabled={unavailable || n.busy || n.loading || n.items.length === 0} onClick={() => void n.dismiss(n.items.map((item) => item.id))}>Dismiss shown</MenuItem>
                  </MenuPopup>
                </Menu>
              </>}
              <PopoverClose render={<Button size="icon-xs" variant="ghost" aria-label="Close notifications" />}><X /></PopoverClose>
            </div>
            <PopoverDescription className="sr-only">{countKnown ? `${n.unread} unread across all sources` : "Activity from your connected services and local tools."}</PopoverDescription>
          </div>
          <div className="min-h-0 overflow-y-auto px-4 py-2">
          {!inDesktop ? (
            <Empty><EmptyHeader><EmptyTitle>Open the desktop app</EmptyTitle><EmptyDescription>Notification history is stored by the Orole daemon. It is not available in this browser preview.</EmptyDescription></EmptyHeader></Empty>
          ) : (
            <div className="flex flex-col gap-3">
              {n.unreadOnly && <p className="text-xs text-muted-foreground">Unread notifications</p>}
              {(offline || n.error) && <Alert variant="warning"><AlertTitle>{offline ? "Daemon offline" : "Could not load notifications"}</AlertTitle><AlertDescription>{offline ? "Reconnect the daemon, then refresh. Any entries below are the last loaded history." : n.error}</AlertDescription></Alert>}
              {n.actionError && <Alert variant="error"><AlertTitle>Notification was not changed</AlertTitle><AlertDescription>{n.actionError}</AlertDescription></Alert>}
              {n.loading && <div role="status" aria-label="Loading notifications" className="flex flex-col gap-2"><Skeleton className="h-14 w-full" /><Skeleton className="h-14 w-full" /></div>}
              {!n.loading && n.loaded && !n.error && !offline && n.items.length === 0 && <Empty><EmptyHeader><EmptyTitle>{n.unreadOnly ? "No unread notifications" : "No notifications"}</EmptyTitle><EmptyDescription>{n.source ? "There is no matching activity from this source." : "New activity will appear here when your services send notifications."}</EmptyDescription></EmptyHeader></Empty>}
              <ul className="flex flex-col gap-1" aria-busy={n.loading || n.busy} aria-label="Notification history">
                {n.items.map((item, index) => {
                  const date = new Date(item.at * 1000);
                  const day = date.toLocaleDateString(undefined, { month: "long", day: "numeric", year: "numeric" });
                  const previous = n.items[index - 1];
                  const startsDay = !previous || new Date(previous.at * 1000).toDateString() !== date.toDateString();
                  const Icon = item.level === "warning" || item.level === "error" ? CircleAlert : item.level === "success" ? CircleCheck : Bell;
                  return <li key={item.id}>
                    {startsDay && <h3 className="pb-2 pt-3 text-xs font-medium text-muted-foreground">{day}</h3>}
                    <div className="group flex items-start gap-3 rounded-lg py-3">
                      <span aria-label={item.read ? "Read" : "Unread"} className={cn("mt-1.5 size-1.5 shrink-0 rounded-full", item.read ? "bg-transparent" : item.level === "warning" || item.level === "error" ? "bg-warning" : "bg-primary")} />
                      <Icon aria-hidden="true" className={cn("mt-0.5 size-4 shrink-0", item.read ? "text-muted-foreground" : item.level === "warning" ? "text-warning-foreground" : item.level === "error" ? "text-destructive-foreground" : "text-muted-foreground")} />
                      <div className="min-w-0 flex-1">
                        <p className={cn("break-words text-sm leading-snug", item.read ? "text-muted-foreground" : "font-semibold")}>{item.title}</p>
                        <p className="mt-1 truncate text-xs text-muted-foreground">{item.source}{item.level !== "info" && ` · ${item.level}`}</p>
                        {item.body && <p className="mt-1 whitespace-pre-wrap break-words text-sm text-muted-foreground">{item.body}</p>}
                      </div>
                      <div className="flex shrink-0 flex-col items-end gap-1">
                        <time dateTime={date.toISOString()} aria-label={date.toLocaleString()} className="text-xs tabular-nums text-muted-foreground">{date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })}</time>
                        <div className="flex gap-0.5 opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 [@media(hover:none)]:opacity-100">
                          {!item.read && <Tooltip><TooltipTrigger render={<Button size="icon-xs" variant="ghost" disabled={unavailable || n.busy} aria-label={`Mark read: ${item.title}`} onClick={() => void n.markRead([item.id])} />}><Check /></TooltipTrigger><TooltipPopup>Mark read</TooltipPopup></Tooltip>}
                          <Tooltip><TooltipTrigger render={<Button size="icon-xs" variant="ghost" disabled={unavailable || n.busy} aria-label={`Dismiss: ${item.title}`} onClick={() => void n.dismiss([item.id])} />}><X /></TooltipTrigger><TooltipPopup>Dismiss</TooltipPopup></Tooltip>
                        </div>
                      </div>
                    </div>
                  </li>;
                })}
              </ul>
              {n.items.length > 0 && <p className="text-xs text-muted-foreground">Showing the latest {n.items.length} matching notifications (up to 100).</p>}
            </div>
          )}
          </div>
        </div>
      </PopoverPopup>
    </Popover>
  );
}
