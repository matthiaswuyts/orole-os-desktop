import { useState, type FormEvent } from "react";
import { KeyRound, Terminal } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { errMsg, useGitHub } from "./store";

/** Connect GitHub: reuse the machine's `gh` login or paste a token. The token is never shown again. */
export function SetupScreen() {
  const { status, login, error: loadError } = useGitHub();
  const [token, setToken] = useState("");
  const [busy, setBusy] = useState<"gh" | "pat" | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const run = (source: "gh" | "pat") => {
    setBusy(source); setErr(null);
    login(source, source === "pat" ? token : "").then(
      () => setToken(""),
      (e) => setErr(errMsg(e)),
    ).finally(() => setBusy(null));
  };
  const submit = (e: FormEvent) => { e.preventDefault(); if (token.trim()) run("pat"); };

  return (
    <div className="mx-auto my-auto flex w-full max-w-lg flex-col gap-3 p-4">
      <div>
        <h1 className="font-medium text-base">Connect GitHub</h1>
        <p className="text-muted-foreground">Orole OS reads your repositories, issues, pull requests, notifications and CI. Anything it writes needs your explicit confirmation first.</p>
      </div>
      {(err || loadError || status?.error) && <p role="alert" className="rounded-lg border border-destructive/40 bg-destructive/8 p-2 text-destructive-foreground text-sm">{err ?? loadError ?? status?.error}</p>}

      <section className="rounded-lg border bg-card p-3">
        <h2 className="flex items-center gap-2 font-medium text-[13px]"><Terminal className="size-3.5" />GitHub CLI</h2>
        {status?.ghDetected ? (
          <>
            <p className="my-1 text-muted-foreground">Found an existing <code className="font-mono">gh</code> login as <strong className="text-foreground">{status.ghLogin}</strong>. The token stays with gh; Orole OS asks for it on demand and keeps it in memory only.</p>
            <Button size="sm" disabled={busy !== null} onClick={() => run("gh")}>{busy === "gh" ? "Connecting…" : `Use ${status.ghLogin}`}</Button>
          </>
        ) : (
          <p className="mt-1 text-muted-foreground">No <code className="font-mono">gh</code> login detected. Run <code className="font-mono">gh auth login</code> in a terminal and come back, or paste a token below.</p>
        )}
      </section>

      <form onSubmit={submit} className="rounded-lg border bg-card p-3">
        <h2 className="flex items-center gap-2 font-medium text-[13px]"><KeyRound className="size-3.5" />Personal access token</h2>
        <Field className="mt-2">
          <FieldLabel>Token</FieldLabel>
          <Input type="password" autoComplete="off" spellCheck={false} value={token} onChange={(e) => setToken(e.target.value)} placeholder="github_pat_…" className="font-mono" aria-label="GitHub personal access token" />
          <FieldDescription>Needs repo, read:org and notifications scopes (fine-grained: contents, issues, pull requests, actions read). Stored in Windows Credential Manager, never in the database, logs or this window.{status?.patStored ? " A token is already stored." : ""}</FieldDescription>
        </Field>
        <Button type="submit" size="sm" className="mt-2" disabled={busy !== null || !token.trim()}>{busy === "pat" ? "Verifying…" : "Verify and save"}</Button>
      </form>
    </div>
  );
}
