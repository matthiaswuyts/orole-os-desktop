import { useEffect, useState } from "react";
import { Mail } from "@/mygo";
import type { Capabilities } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";
import { Field, FieldLabel } from "@/components/ui/field";
import { Fieldset } from "@/components/ui/fieldset";
import { Badge } from "@/components/ui/badge";
import { Dialog, DialogDescription, DialogFooter, DialogHeader, DialogPanel, DialogPopup, DialogTitle } from "@/components/ui/dialog";
import { MCPSettings } from "@/features/mcp/MCPSettings";
import { errorText, Failure, Loading, NoResults, useRemote } from "@/features/mcp/shared";
import { VirtualList } from "@/features/mcp/VirtualList";
import { useUI } from "@/stores/ui";

export function MailView() {
  const status = useRemote(() => Mail.status(), "mail-status");
  const [settings, setSettings] = useState(false);
  if (status.loading) return <Loading>Loading Mail connection…</Loading>;
  if (status.error) return <div className="p-4"><Failure error={status.error} retry={status.reload} /></div>;
  if (!inDesktop || !status.data?.connected || settings) return <div className="h-full overflow-auto p-4"><div className="mb-4 flex items-center gap-2"><h1 className="text-base font-medium">Mail setup</h1>{status.data?.connected && <Button variant="outline" size="sm" onClick={() => setSettings(false)}>Back to Mail</Button>}</div><Failure error={status.data?.error ?? null} /><MCPSettings adapter="mail" /></div>;
  return <Mailbox key={status.data.serverId} capabilities={status.data.capabilities} setup={() => setSettings(true)} />;
}

function Mailbox({ capabilities, setup }: { capabilities: Capabilities; setup: () => void }) {
  const [folder, setFolder] = useState("");
  const [query, setQuery] = useState("");
  const [search, setSearch] = useState("");
  const [pages, setPages] = useState<string[]>([""]);
  const token = pages[pages.length - 1]!;
  const [threadId, setThreadId] = useState("");
  const [compose, setCompose] = useState<"new" | "reply" | null>(null);
  const [notice, setNotice] = useState("");
  const pending = useUI((state) => state.pendingMailThread);
  useEffect(() => {
    if (!pending) return;
    setThreadId(pending); setCompose(null); setNotice("");
    useUI.getState().clearMailThread();
  }, [pending]);
  const folders = useRemote(() => Mail.folders(), "mail-folders", capabilities.canFolders);
  const messages = useRemote(() => search ? Mail.search({ folderId: folder, query: search, pageToken: token, limit: 0 }) : Mail.list({ folderId: folder, query: "", pageToken: token, limit: 0 }), JSON.stringify([folder, search, token]), search ? capabilities.canSearch : capabilities.canList);
  const thread = useRemote(() => Mail.thread(threadId), threadId, Boolean(threadId) && capabilities.canThread);
  return <div className="flex h-full min-h-0 flex-col">
    <header className="flex flex-wrap items-center gap-2 border-b px-3 py-2"><Button size="sm" disabled={!capabilities.canSend} onClick={() => { setCompose("new"); setNotice(""); }}>Compose</Button><span className="flex-1" /><Button size="sm" variant="ghost" onClick={() => { folders.reload(); messages.reload(); thread.reload(); }}>Refresh</Button><Button size="sm" variant="outline" onClick={setup}>Connection</Button></header>
    {notice && <p role="status" className="border-b px-3 py-2">{notice}</p>}
    <div className="grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[10rem_minmax(14rem,1fr)_minmax(18rem,1.4fr)]">
      <aside aria-label="Mail folders" className="flex min-h-0 flex-col border-r max-md:max-h-40"><Button size="sm" variant="ghost" onClick={() => { setFolder(""); setPages([""]); setThreadId(""); }}>All messages</Button><Failure error={folders.error} retry={folders.reload} />{folders.loading && <Loading>Loading folders…</Loading>}{folders.data && <VirtualList items={folders.data} selected={folder} onSelect={(item) => { setFolder(item.id); setPages([""]); setThreadId(""); }} label="Mail folders" rowHeight={40} render={(item) => <span className="flex gap-2"><span className="truncate">{item.name}</span>{item.unreadCount > 0 && <span className="ml-auto text-muted-foreground tabular-nums">{item.unreadCount}</span>}</span>} />}{!capabilities.canFolders && <p className="p-3 text-xs text-muted-foreground">Folder listing is not exposed by this server.</p>}</aside>
      <section aria-label="Messages" className="flex min-h-0 flex-col border-r max-md:min-h-64">
        <form className="flex gap-2 border-b p-2" onSubmit={(event) => { event.preventDefault(); setSearch(query.trim()); setPages([""]); setThreadId(""); }}><Input aria-label="Search mail" placeholder="Search mail" value={query} disabled={!capabilities.canSearch} onChange={(event) => setQuery(event.target.value)} /><Button size="sm" type="submit" variant="outline" disabled={!capabilities.canSearch}>Search</Button></form>
        <Failure error={messages.error} retry={messages.reload} />
        {messages.loading && <Loading>Loading messages…</Loading>}
        {!capabilities.canList && !search && <NoResults title="Message listing unavailable">The connected server did not expose a compatible message-list tool.</NoResults>}
        {messages.data && !messages.data.messages?.length && <NoResults title="No messages">{search ? "Try another search or folder." : "This folder is empty."}</NoResults>}
        {messages.data && <VirtualList items={messages.data.messages ?? []} selected={messages.data.messages?.find((message) => (message.threadId || message.id) === threadId)?.id ?? ""} onSelect={(message) => { setThreadId(message.threadId || message.id); setCompose(null); }} label="Messages" rowHeight={84} render={(message) => <><div className="flex gap-2"><span className="truncate font-medium">{message.from}</span>{message.unread && <Badge variant="info">Unread</Badge>}</div><p className="truncate">{message.subject || "(No subject)"}</p><p className="truncate text-xs text-muted-foreground">{message.snippet || message.date}</p></>} />}
        <div className="flex justify-between gap-2 border-t p-2"><Button size="sm" variant="outline" disabled={pages.length <= 1 || messages.loading} onClick={() => { setPages((current) => current.slice(0, -1)); setThreadId(""); }}>Previous</Button><span className="self-center text-xs text-muted-foreground">Page {pages.length}</span><Button size="sm" variant="outline" disabled={!messages.data?.nextPageToken || messages.loading || pages.includes(messages.data?.nextPageToken ?? "")} onClick={() => { if (messages.data?.nextPageToken) { setPages((current) => [...current, messages.data!.nextPageToken]); setThreadId(""); } }}>Next</Button></div>
      </section>
      <section aria-label="Mail thread" className="min-h-0 overflow-auto p-4">
        {compose ? <Composer key={`${compose}:${threadId}`} threadId={compose === "reply" ? threadId : ""} cancel={() => setCompose(null)} sent={() => { setCompose(null); setNotice("Message sent."); messages.reload(); thread.reload(); }} /> : <>
          {!threadId && <NoResults title="Select a message">Message bodies are shown as plain text. Remote images and HTML are never loaded.</NoResults>}
          {threadId && !capabilities.canThread && <NoResults title="Thread reading unavailable">The server did not expose a compatible thread tool.</NoResults>}
          {thread.loading && <Loading>Loading thread…</Loading>}<Failure error={thread.error} retry={thread.reload} />
          {thread.data && <div className="flex flex-col gap-4"><div className="flex items-center justify-between gap-2"><h2 className="min-w-0 break-words font-medium">{thread.data.messages?.[0]?.subject || "Thread"}</h2>{capabilities.canSend && capabilities.canReply && <Button size="sm" onClick={() => setCompose("reply")}>Reply</Button>}</div><p className="text-xs text-muted-foreground">External mail content · plain text only</p>{thread.data.messages?.map((message) => <article key={message.id} className="flex flex-col gap-2 border-t pt-3"><div className="break-words text-sm"><p className="font-medium">{message.from}</p><p className="text-xs text-muted-foreground">To: {(message.to ?? []).join(", ")}</p><p className="text-xs text-muted-foreground">{message.date}</p></div><p className="selectable max-w-prose whitespace-pre-wrap break-words text-sm leading-relaxed">{message.body || "No plain-text body is available."}</p></article>)}</div>}
        </>}
      </section>
    </div>
  </div>;
}

function Composer({ threadId, cancel, sent }: { threadId: string; cancel: () => void; sent: () => void }) {
  const [to, setTo] = useState("");
  const [subject, setSubject] = useState("");
  const [body, setBody] = useState("");
  const [review, setReview] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  async function send() {
    setReview(false); setBusy(true); setError(null);
    try {
      const result = threadId ? await Mail.reply({ threadId, body }) : await Mail.send({ to: to.split(/[,;\n]/).map((address) => address.trim()).filter(Boolean), subject, body });
      if (!result.confirmed) throw new Error("The server did not confirm delivery. Check Sent before trying again to avoid duplicates.");
      sent();
    } catch (cause) { setError(errorText(cause)); }
    finally { setBusy(false); }
  }
  return <><form className="flex flex-col gap-4" onSubmit={(event) => { event.preventDefault(); setReview(true); }}><h2 className="font-medium">{threadId ? "Reply" : "New message"}</h2><Fieldset disabled={busy} className="flex flex-col gap-4">
    {!threadId && <><Field><FieldLabel htmlFor="mail-to">To (comma-separated)</FieldLabel><Input id="mail-to" required value={to} onChange={(event) => setTo(event.target.value)} /></Field><Field><FieldLabel htmlFor="mail-subject">Subject</FieldLabel><Input id="mail-subject" required value={subject} onChange={(event) => setSubject(event.target.value)} /></Field></>}
    <Field><FieldLabel htmlFor="mail-body">Message</FieldLabel><Textarea id="mail-body" className="min-h-56" required value={body} onChange={(event) => setBody(event.target.value)} /></Field>
    <Failure error={error} /><div className="flex gap-2"><Button type="submit" disabled={busy || !body.trim()}>{busy ? "Waiting for approval or delivery…" : "Review before sending"}</Button><Button variant="outline" disabled={busy} onClick={cancel}>Discard</Button></div>
  </Fieldset></form><Dialog open={review} onOpenChange={setReview}><DialogPopup><DialogHeader><DialogTitle>Send this {threadId ? "reply" : "message"}?</DialogTitle><DialogDescription>Sending changes your mailbox and cannot be undone. The daemon may require a separate policy approval.</DialogDescription></DialogHeader><DialogPanel><div className="flex flex-col gap-3">{threadId ? <p className="break-all text-sm">Reply to thread {threadId}</p> : <><p className="break-words">To: {to}</p><p className="break-words font-medium">{subject}</p></>}<p className="selectable whitespace-pre-wrap break-words text-sm">{body}</p></div></DialogPanel><DialogFooter><Button variant="outline" onClick={() => setReview(false)}>Keep editing</Button><Button onClick={() => void send()}>Send {threadId ? "reply" : "message"}</Button></DialogFooter></DialogPopup></Dialog></>;
}
