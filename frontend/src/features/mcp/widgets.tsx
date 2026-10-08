import { HardDrive, Mail as MailIcon } from "lucide-react";
import { Drive, Mail } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useUI } from "@/stores/ui";
import { Widget, type WidgetStatus } from "@/components/widgets/Widget";
import type { WidgetProps } from "@/components/widgets/registry";
import { useRemote } from "./shared";

export function MailWidget({ onRemove }: WidgetProps) {
  const connection = useRemote(() => Mail.status(), "mail-widget-status");
  const canList = connection.data?.connected === true && connection.data.capabilities.canList;
  const messages = useRemote(() => Mail.list({ folderId: "", query: "", pageToken: "", limit: 0 }), "mail-widget-messages", canList);
  const error = connection.error ?? messages.error;
  const state: WidgetStatus = !inDesktop ? "offline" : error ? "error" : connection.loading ? "loading" : !connection.data?.connected || !canList ? "unconfigured" : messages.loading ? "loading" : "ready";
  const open = () => useUI.getState().setView("mail");
  return <Widget title="Mail" icon={<MailIcon />} onRemove={onRemove} status={state} message={error ?? (!inDesktop ? "Open the desktop app to load mail." : !connection.data?.connected ? "Connect and authorize Mail to see your messages." : "This server does not expose a compatible message-list tool.")} action={error ? { label: "Retry", onClick: () => { connection.reload(); messages.reload(); } } : { label: connection.data?.connected ? "Open Mail" : "Set up Mail", onClick: open }}>
    {messages.data?.messages?.length ? <ul>{messages.data.messages.slice(0, 10).map((message) => <li key={message.id}><button className="flex w-full flex-col gap-0.5 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring" onClick={open}><span className="w-full truncate font-medium">{message.subject || "(No subject)"}</span><span className="w-full truncate text-xs text-muted-foreground">{message.from}</span></button></li>)}</ul> : <p className="p-4 text-sm text-muted-foreground">No messages returned by your mailbox.</p>}
  </Widget>;
}

export function DriveWidget({ onRemove }: WidgetProps) {
  const connection = useRemote(() => Drive.status(), "drive-widget-status");
  const canList = connection.data?.connected === true && connection.data.capabilities.canList;
  const files = useRemote(() => Drive.list({ parentId: "", query: "", pageToken: "", limit: 0 }), "drive-widget-files", canList);
  const error = connection.error ?? files.error;
  const state: WidgetStatus = !inDesktop ? "offline" : error ? "error" : connection.loading ? "loading" : !connection.data?.connected || !canList ? "unconfigured" : files.loading ? "loading" : "ready";
  const open = () => useUI.getState().setView("drive");
  return <Widget title="Drive files" icon={<HardDrive />} onRemove={onRemove} status={state} message={error ?? (!inDesktop ? "Open the desktop app to load Drive files." : !connection.data?.connected ? "Connect and authorize Drive to see your files." : "This server does not expose a compatible file-list tool.")} action={error ? { label: "Retry", onClick: () => { connection.reload(); files.reload(); } } : { label: connection.data?.connected ? "Open Drive" : "Set up Drive", onClick: open }}>
    {files.data?.files?.length ? <ul>{files.data.files.slice(0, 10).map((file) => <li key={file.id}><button className="flex w-full flex-col gap-0.5 rounded px-2 py-1.5 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring" onClick={open}><span className="w-full truncate font-medium">{file.name}</span><span className="w-full truncate text-xs text-muted-foreground">{file.isFolder ? "Folder" : file.mimeType || "File"}</span></button></li>)}</ul> : <p className="p-4 text-sm text-muted-foreground">No files returned by Drive.</p>}
  </Widget>;
}
