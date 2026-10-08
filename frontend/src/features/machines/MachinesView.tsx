import { useCallback, useEffect, useState } from "react";
import { Monitor, Plus, RefreshCw, Server, ShieldCheck } from "lucide-react";
import { Machines, type Machine, type MachineHealth as Health, type Identity } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { Spinner } from "@/components/ui/spinner";
import { NeedsDesktop } from "@/features/shared/States";
import { cn } from "@/lib/utils";
import { useBoxes } from "@/stores/boxes";
import { Field, FieldLabel } from "@/components/ui/field";

const message = (error: unknown) => error instanceof Error ? error.message : String(error);

export function MachinesView() {
  const [items, setItems] = useState<Machine[]>([]);
  const selected = useBoxes((state) => state.selectedId);
  const setSelected = (id: string) => useBoxes.setState({ selectedId: id });
  const [adding, setAdding] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    if (!inDesktop) return;
    setLoading(true);
    try { const machines = await Machines.list(); setItems(machines); useBoxes.setState({ machines, loaded: true, error: "" }); setError(""); }
    catch (e) { setError(message(e)); }
    finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);
  if (!inDesktop) return <NeedsDesktop what="Machines" />;
  const current = items.find((item) => item.id === selected);
  return <div className="flex h-full min-h-0 flex-col sm:flex-row">
    <aside aria-label="Machines" className="flex shrink-0 flex-col border-b sm:w-60 sm:border-r sm:border-b-0">
      <div className="flex h-10 items-center justify-between border-b px-3"><h2 className="text-sm font-medium">Machines</h2><Button size="xs" variant="outline" onClick={() => setAdding(true)}><Plus />Pair</Button></div>
      <div className="overflow-auto p-1">
        {items.map((item) => <button key={item.id} type="button" aria-current={!adding && selected === item.id ? "true" : undefined} onClick={() => { setSelected(item.id); setAdding(false); }} className={cn("flex w-full items-center gap-2 rounded-md px-2 py-2 text-left text-sm outline-none hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring", !adding && selected === item.id && "bg-accent")}>
          {item.kind === "local" ? <Monitor className="size-4 shrink-0" /> : <Server className="size-4 shrink-0" />}<span className="min-w-0"><span className="block truncate font-medium">{item.name}</span><span className="block truncate text-xs text-muted-foreground">{item.kind === "local" ? "This computer · native" : item.address}</span></span>
        </button>)}
      </div>
      <Button size="sm" variant="ghost" disabled={loading} onClick={() => void load()}><RefreshCw />Refresh machines</Button>
    </aside>
    <main className="min-w-0 flex-1 overflow-auto p-4">
      {error && <p role="alert" className="mb-3 text-sm text-destructive-foreground">{error}</p>}
      {loading && items.length === 0 && <p role="status" className="flex items-center gap-2 text-sm"><Spinner />Loading machines…</p>}
      {adding ? <PairMachine onCancel={() => setAdding(false)} onPaired={async (id) => { setSelected(id); setAdding(false); await load(); }} /> : current && <MachineDetail key={`${current.id}:${current.roots.join("|")}:${current.allowCommands}:${current.allowWrite}`} machine={current} onChange={load} onRemoved={async () => { setSelected("local"); await load(); }} />}
    </main>
  </div>;
}

function PairMachine({ onCancel, onPaired }: { onCancel: () => void; onPaired: (id: string) => Promise<void> }) {
  const [name, setName] = useState("");
  const [address, setAddress] = useState("");
  const [username, setUsername] = useState("");
  const [windows, setWindows] = useState(false);
  const [keyPath, setKeyPath] = useState("");
  const [identity, setIdentity] = useState<Identity | null>(null);
  const [confirmed, setConfirmed] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const probe = async () => { setBusy(true); setError(""); setIdentity(null); setConfirmed(""); try { setIdentity(await Machines.probe(address)); } catch (e) { setError(message(e)); } finally { setBusy(false); } };
  const pair = async () => {
    if (!identity || identity.address !== address || confirmed.trim() !== identity.fingerprint) return;
    setBusy(true); setError("");
    try { const machine = await Machines.pair({ name, address, username, os: windows ? "windows" : "linux", fingerprint: confirmed.trim(), privateKeyPath: keyPath }); await onPaired(machine.id); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  };
  return <section className="max-w-2xl space-y-5">
    <header><h2 className="text-base font-medium">Pair an SSH machine</h2><p className="mt-1 text-sm text-muted-foreground">Connect directly to your own Linux or Windows SSH server. No Tailscale or Orole daemon is required on the remote host.</p></header>
    <div className="grid gap-3 sm:grid-cols-2">
      <div className="space-y-1"><Label htmlFor="machine-name">Name</Label><Input id="machine-name" value={name} onChange={(e) => setName(e.target.value)} disabled={busy} /></div>
      <div className="space-y-1"><Label htmlFor="machine-address">SSH address, including port</Label><Input id="machine-address" placeholder="host.example:22" value={address} onChange={(e) => { setAddress(e.target.value); setIdentity(null); setConfirmed(""); }} disabled={busy} /></div>
      <div className="space-y-1"><Label htmlFor="machine-user">SSH username</Label><Input id="machine-user" value={username} onChange={(e) => setUsername(e.target.value)} disabled={busy} autoComplete="off" /></div>
      <div className="flex items-center gap-2"><Checkbox id="machine-windows" checked={windows} onCheckedChange={setWindows} disabled={busy} /><Label htmlFor="machine-windows">Remote machine runs Windows</Label></div>
    </div>
    <div className="space-y-1"><Label htmlFor="machine-key">Local private key file path</Label><Input id="machine-key" value={keyPath} onChange={(e) => setKeyPath(e.target.value)} disabled={busy} /><p className="text-xs text-muted-foreground">The daemon imports an unencrypted OpenSSH key into Windows Credential Manager. The key itself never enters this form or the database. Protect the source file with your Windows account permissions. Encrypted key files are not supported.</p></div>
    <Button variant="outline" disabled={busy || !address.trim()} onClick={() => void probe()}><ShieldCheck />Read server identity</Button>
    {identity && <div className="space-y-3 border-t pt-4">
      <h3 className="text-sm font-medium">Verify before trusting</h3><p className="text-sm text-muted-foreground">This fingerprint was received from the network, not yet trusted. Compare it through an independent channel with your server administrator or the server’s host public key. A matching network address alone is not proof.</p>
      <p className="break-all rounded-md bg-muted p-3 font-mono text-xs">{identity.keyType}<br />{identity.fingerprint}</p>
      <div className="space-y-1"><Label htmlFor="machine-fingerprint">Paste the independently verified SHA256 fingerprint</Label><Input id="machine-fingerprint" value={confirmed} onChange={(e) => setConfirmed(e.target.value)} disabled={busy} autoComplete="off" /></div>
    </div>}
    {error && <p role="alert" className="text-sm text-destructive-foreground">{error}</p>}
    {busy && <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Spinner />Connecting or waiting for approval in Permissions…</p>}
    <div className="flex flex-wrap gap-2"><Button disabled={busy || !name.trim() || !username.trim() || !keyPath.trim() || !identity || confirmed.trim() !== identity.fingerprint} onClick={() => void pair()}>Trust identity and pair</Button><Button variant="outline" disabled={busy} onClick={onCancel}>Cancel</Button></div>
    <p className="text-xs text-muted-foreground">Pairing grants no file roots or command access. Configure those separately after pairing. Changed server keys are refused, never silently accepted.</p>
  </section>;
}

function MachineDetail({ machine, onChange, onRemoved }: { machine: Machine; onChange: () => Promise<void>; onRemoved: () => Promise<void> }) {
  const [health, setHealth] = useState<Health | null>(null);
  const [name, setName] = useState(machine.name);
  const [roots, setRoots] = useState(machine.roots.join("\n"));
  const [commands, setCommands] = useState(machine.allowCommands);
  const [write, setWrite] = useState(machine.allowWrite);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [removeConfirm, setRemoveConfirm] = useState(false);
  const refresh = useCallback(async () => { setBusy(true); setError(""); setHealth(null); try { setHealth(await Machines.health(machine.id)); } catch (e) { setError(message(e)); } finally { setBusy(false); } }, [machine.id]);
  useEffect(() => { void refresh(); }, [refresh]);
  const save = async () => { setBusy(true); setError(""); setNotice(""); try { await Machines.setAccess({ id: machine.id, roots: roots.split("\n").map((root) => root.trim()).filter(Boolean), allowCommands: commands, allowWrite: write }); setNotice("Machine access saved."); await onChange(); } catch (e) { setError(message(e)); } finally { setBusy(false); } };
  const remove = async () => { setBusy(true); setError(""); try { await Machines.remove(machine.id); await onRemoved(); } catch (e) { setError(message(e)); } finally { setBusy(false); } };
  const rename = async () => {
    setBusy(true); setError(""); setNotice("");
    try { await Machines.rename(machine.id, name); setNotice("Box name saved."); await onChange(); }
    catch (e) { setError(message(e)); } finally { setBusy(false); }
  };
  return <section className="max-w-3xl space-y-6">
    <header className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-base font-medium">{machine.name}</h2><p className="mt-1 text-sm text-muted-foreground">{machine.os} · {machine.transport} transport · {machine.backend} execution</p></div><Button size="sm" variant="outline" onClick={() => void refresh()} disabled={busy}><RefreshCw />Check health</Button></header>
    {error && <p role="alert" className="text-sm text-destructive-foreground">{error}</p>}
    {busy && <p role="status" className="flex items-center gap-2 text-sm text-muted-foreground"><Spinner />Working; sensitive changes may need approval in Permissions…</p>}
    {notice && <p role="status" className="text-sm">{notice}</p>}
    <form className="flex items-end gap-2" onSubmit={(event) => { event.preventDefault(); void rename(); }}>
      <Field className="min-w-0 flex-1"><FieldLabel htmlFor="box-display-name">Box name</FieldLabel><Input id="box-display-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} disabled={busy} /></Field>
      <Button type="submit" variant="outline" disabled={busy || !name.trim() || name.trim() === machine.name}>Save name</Button>
    </form>
    {health && <div className="flex flex-col gap-2">
      <Badge variant={health.reachable ? "success" : "outline"}>{health.reachable ? machine.kind === "local" ? "Native host available" : "SSH authenticated" : "Host offline"}</Badge>
      <p className="text-sm text-muted-foreground">{health.detail}</p>
      {health.metricError && <p className="text-sm text-muted-foreground">Host memory unavailable: {health.metricError}</p>}
      <dl className="grid grid-cols-2 gap-x-6 gap-y-2 text-sm sm:grid-cols-3">
        <div><dt className="text-muted-foreground">Hostname</dt><dd>{health.hostname || "Not observed"}</dd></div>
        <div><dt className="text-muted-foreground">Checked</dt><dd>{new Date(health.checkedAt).toLocaleTimeString()}</dd></div>
        <div><dt className="text-muted-foreground">Connection time</dt><dd className="tabular-nums">{health.latencyMs} ms</dd></div>
        <div><dt className="text-muted-foreground">Architecture</dt><dd>{health.architecture || "Not observed"}</dd></div>
        {health.memoryReady && <div><dt className="text-muted-foreground">Host memory in use</dt><dd>{((health.memoryTotalBytes - health.memoryAvailableBytes) / 1073741824).toFixed(1)} of {(health.memoryTotalBytes / 1073741824).toFixed(1)} GiB</dd></div>}
        {machine.kind === "local" && <><div><dt className="text-muted-foreground">Logical CPUs</dt><dd>{health.logicalCpus}</dd></div><div><dt className="text-muted-foreground">Daemon uptime</dt><dd>{health.daemonUptimeSeconds}s</dd></div><div><dt className="text-muted-foreground">Daemon heap</dt><dd>{(health.daemonHeapBytes / 1048576).toFixed(1)} MiB</dd></div></>}
      </dl>
    </div>}
    {machine.kind === "remote" && <div className="space-y-1 border-t pt-4"><h3 className="text-sm font-medium">Pinned SSH identity</h3><p className="break-all font-mono text-xs">{machine.fingerprint}</p><p className="text-xs text-muted-foreground">{machine.username}@{machine.address}. To rotate a server key, independently verify the new identity, remove this pairing, then pair again.</p></div>}
    <div className="space-y-3 border-t pt-4"><h3 className="text-sm font-medium">Machine access</h3><p className="text-sm text-muted-foreground">Each machine has separate grants. Local Files and Projects permissions do not grant access here.</p><div className="space-y-1"><Label htmlFor="machine-roots">Approved directories, one absolute path per line</Label><Textarea id="machine-roots" value={roots} onChange={(e) => setRoots(e.target.value)} rows={4} className="font-mono text-xs" disabled={busy} /></div><div className="flex items-center gap-2"><Checkbox id="machine-write" checked={write} onCheckedChange={setWrite} disabled={busy} /><Label htmlFor="machine-write">Allow file writes inside these roots</Label></div><div className="flex items-center gap-2"><Checkbox id="machine-commands" checked={commands} onCheckedChange={setCommands} disabled={busy} /><Label htmlFor="machine-commands">Allow command requests</Label></div><p className="text-xs text-muted-foreground">Commands still require Go-side approval and run with the account’s full OS permissions; approved directories are not a command sandbox. Remote file browsing and editing are available in Files → Remote. Remote folder creation, rename, and deletion are not supported.</p><Button disabled={busy} onClick={() => void save()}>Save machine access</Button></div>
    <p className="text-xs text-muted-foreground">Command approvals show the executable, every argument, and the target account. Remote requests require an absolute executable path, such as <code>/usr/bin/printf</code> on Linux or <code>C:\Windows\System32\whoami.exe</code> on Windows; bare command names are not accepted. Changing access or removing a pairing invalidates pending operations. It cannot undo commands already sent to a remote host or guarantee detached processes stop.</p>
    {machine.kind === "remote" && <div className="space-y-2 border-t pt-4"><p className="text-xs text-muted-foreground">Removing this machine forgets its pinned key and credential. It does not delete anything on the server.</p>{removeConfirm ? <div className="flex gap-2"><Button variant="destructive-outline" disabled={busy} onClick={() => void remove()}>Confirm removal</Button><Button variant="outline" disabled={busy} onClick={() => setRemoveConfirm(false)}>Keep machine</Button></div> : <Button variant="destructive-outline" disabled={busy} onClick={() => setRemoveConfirm(true)}>Remove pairing…</Button>}</div>}
  </section>;
}
