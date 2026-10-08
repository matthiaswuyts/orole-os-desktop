import { useEffect, useState } from "react";
import { Bell, Check, RefreshCw, X } from "lucide-react";
import { useNotifications } from "@/stores/notifications";
import { useDaemon } from "@/stores/daemon";
import { inDesktop } from "@/lib/backend";
import { cn } from "@/lib/utils";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Sheet, SheetDescription, SheetHeader, SheetPanel, SheetPopup, SheetTitle, SheetTrigger } from "@/components/ui/sheet";
import { Skeleton } from "@/components/ui/skeleton";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";

export function NotificationCenter() {
  const [open, setOpen] = useState(false);
  const n = useNotifications();
  const link = useDaemon((state) => state.link);
  useEffect(() => n.startSync(), [n.startSync]);
  const offline = link === "offline";
  const unavailable = !inDesktop || offline;
  const countKnown = n.loaded && !n.error && !unavailable;
  const matchingUnread = n.source ? (n.sources.find((source) => source.source === n.source)?.unread ?? 0) : n.unread;
  const sourceItems = [{ value: "", label: "All sources" }, ...Array.from(new Set([...n.sources.map((source) => source.source), ...(n.source ? [n.source] : [])])).map((source) => ({ value: source, label: source }))];

  return (
    <Sheet open={open} onOpenChange={(value) => { setOpen(value); if (value) void n.load(); }}>
      <SheetTrigger render={<Button variant="ghost" size="icon-xs" className="relative" />} aria-label={countKnown ? `Notifications, ${n.unread} unread` : "Notifications, unread count unavailable"} title="Notifications">
        <Bell />
        {countKnown && n.unread > 0 && <Badge size="sm" className="absolute -right-1 -top-1" aria-hidden="true">{n.unread > 99 ? "99+" : n.unread}</Badge>}
      </SheetTrigger>
      <SheetPopup>
        <SheetHeader>
          <SheetTitle>Notifications</SheetTitle>
          <SheetDescription>{countKnown ? `${n.unread} unread across all sources` : "Activity from your connected services and local tools."}</SheetDescription>
        </SheetHeader>
        <SheetPanel>
          {!inDesktop ? (
            <Empty><EmptyHeader><EmptyTitle>Open the desktop app</EmptyTitle><EmptyDescription>Notification history is stored by the Orole daemon. It is not available in this browser preview.</EmptyDescription></EmptyHeader></Empty>
          ) : (
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center gap-2">
                <Select value={n.source} items={sourceItems} onValueChange={(value) => n.filter(value ?? "", n.unreadOnly)}>
                  <SelectTrigger size="sm" aria-label="Notification source" className="min-w-0 flex-1"><SelectValue /></SelectTrigger>
                  <SelectPopup><SelectGroup>{sourceItems.map((source) => <SelectItem key={source.value} value={source.value}>{source.label}</SelectItem>)}</SelectGroup></SelectPopup>
                </Select>
                <ToggleGroup size="sm" variant="outline" value={[n.unreadOnly ? "unread" : "all"]} onValueChange={(values) => { if (values[0]) n.filter(n.source, values[0] === "unread"); }} aria-label="Notification visibility">
                  <ToggleGroupItem value="all">All</ToggleGroupItem><ToggleGroupItem value="unread">Unread</ToggleGroupItem>
                </ToggleGroup>
                <Button size="icon-sm" variant="ghost" aria-label="Refresh notifications" disabled={n.loading} onClick={() => { void useDaemon.getState().refresh(); void n.load(); }}><RefreshCw /></Button>
              </div>
              {(offline || n.error) && <Alert variant="warning"><AlertTitle>{offline ? "Daemon offline" : "Could not load notifications"}</AlertTitle><AlertDescription>{offline ? "Reconnect the daemon, then refresh. Any entries below are the last loaded history." : n.error}</AlertDescription></Alert>}
              {n.actionError && <Alert variant="error"><AlertTitle>Notification was not changed</AlertTitle><AlertDescription>{n.actionError}</AlertDescription></Alert>}
              <div className="flex flex-wrap items-center gap-2">
                <Button size="xs" variant="outline" disabled={unavailable || n.busy || n.loading || matchingUnread === 0} onClick={() => void n.markRead([], true)}><Check data-icon="inline-start" />{n.source ? "Mark source read" : "Mark all read"}</Button>
                <Button size="xs" variant="ghost" disabled={unavailable || n.busy || n.loading || n.items.length === 0} onClick={() => void n.dismiss(n.items.map((item) => item.id))}>Dismiss shown</Button>
              </div>
              {n.loading && <div role="status" aria-label="Loading notifications" className="flex flex-col gap-2"><Skeleton className="h-14 w-full" /><Skeleton className="h-14 w-full" /></div>}
              {!n.loading && n.loaded && !n.error && !offline && n.items.length === 0 && <Empty><EmptyHeader><EmptyTitle>{n.unreadOnly ? "No unread notifications" : "No notifications"}</EmptyTitle><EmptyDescription>{n.source ? "There is no matching activity from this source." : "New activity will appear here when your services send notifications."}</EmptyDescription></EmptyHeader></Empty>}
              <ul className="flex flex-col divide-y" aria-busy={n.loading || n.busy} aria-label="Notification history">
                {n.items.map((item) => (
                  <li key={item.id} className="flex gap-2 py-3">
                    <div className="min-w-0 flex-1">
                      <div className="mb-1 flex flex-wrap items-center gap-1.5"><Badge variant="outline" size="sm">{item.source}</Badge>{!item.read && <Badge size="sm">Unread</Badge>}{item.level !== "info" && <Badge variant={item.level === "error" ? "error" : item.level === "warning" ? "warning" : "success"} size="sm">{item.level}</Badge>}</div>
                      <p className={cn("break-words", !item.read && "font-medium")}>{item.title}</p>
                      {item.body && <p className="mt-1 whitespace-pre-wrap break-words text-muted-foreground">{item.body}</p>}
                      <time dateTime={new Date(item.at * 1000).toISOString()} className="mt-1 block text-xs text-muted-foreground">{new Date(item.at * 1000).toLocaleString()}</time>
                    </div>
                    <div className="flex shrink-0 items-start gap-0.5">
                      {!item.read && <Button size="icon-xs" variant="ghost" disabled={unavailable || n.busy} aria-label={`Mark read: ${item.title}`} title="Mark read" onClick={() => void n.markRead([item.id])}><Check /></Button>}
                      <Button size="icon-xs" variant="ghost" disabled={unavailable || n.busy} aria-label={`Dismiss: ${item.title}`} title="Dismiss" onClick={() => void n.dismiss([item.id])}><X /></Button>
                    </div>
                  </li>
                ))}
              </ul>
              {n.items.length > 0 && <p className="text-xs text-muted-foreground">Showing the latest {n.items.length} matching notifications (up to 100).</p>}
            </div>
          )}
        </SheetPanel>
      </SheetPopup>
    </Sheet>
  );
}
