import { DownloadIcon, LogInIcon, LogOutIcon, RefreshCwIcon } from "lucide-react";
import { ProviderLogo } from "@/components/ProviderLogo";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { Failure, Loading } from "@/features/mcp/shared";
import type { CliruntimeStatus as Status } from "@/mygo";
import { useCLIRuntimes, useCLIRuntimesSync } from "@/stores/cliRuntimes";

/** What to tell the user about using a subscription through a vendor's CLI, by provider. */
const NOTES: Record<string, string> = {
  claude: "Uses your Claude plan through Anthropic's own CLI. Anthropic limits subscription sign-in to ordinary personal use.",
  codex: "Uses your ChatGPT plan through OpenAI's own Codex CLI.",
  grok: "Uses your Grok account through x.ai's own CLI.",
};

/** One line under the name: what is installed and who is signed in. */
function subtitle(s: Status): string {
  if (!s.installed) return s.installable ? "Not installed on this computer" : "Not available for this computer";
  const parts = [s.version ? `v${s.version}` : "Installed"];
  if (s.auth === "signed_in") parts.push(s.account || "Signed in");
  else if (s.auth === "signed_out") parts.push("Not signed in");
  parts.push(s.source === "managed" ? "Orole's copy" : s.source === "override" ? "Custom path" : "Your installed copy");
  return parts.join(" · ");
}

function StatusBadge({ s }: { s: Status }) {
  if (!s.installed) return null;
  if (s.updateAvailable) return <Tooltip><TooltipTrigger render={<Badge variant="warning" />}>Update available</TooltipTrigger><TooltipPopup>v{s.version} is installed; v{s.latest} is available</TooltipPopup></Tooltip>;
  if (s.auth === "signed_in") return <Badge variant="success">Connected</Badge>;
  if (s.auth === "signed_out") return <Badge variant="outline">Not signed in</Badge>;
  return null;
}

function AccountRow({ s }: { s: Status }) {
  const busy = useCLIRuntimes((state) => state.busy[s.id]);
  const problem = useCLIRuntimes((state) => state.problems[s.id]);
  const act = useCLIRuntimes.getState();
  // Signing in is offered whenever the CLI is not known to be signed in, including a CLI that cannot report it.
  const canSignIn = s.installed && s.auth !== "signed_in";
  return (
    <li aria-label={s.name} className="flex min-w-0 flex-col gap-1.5 p-3">
      <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-2">
        <ProviderLogo id={s.id} name={s.name} />
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h3 className="font-medium">{s.name}</h3>
            <StatusBadge s={s} />
          </div>
          <p className="truncate text-xs text-muted-foreground">{subtitle(s)}</p>
        </div>
        <div className="flex shrink-0 items-center gap-1.5">
          {!s.installed && s.installable && (
            <Button size="sm" loading={busy === "install"} disabled={!!busy} onClick={() => void act.install(s.id)}><DownloadIcon aria-hidden />Download</Button>
          )}
          {s.installed && s.updateAvailable && (
            <Button size="sm" loading={busy === "install"} disabled={!!busy || !s.installable} onClick={() => void act.install(s.id)}><RefreshCwIcon aria-hidden />Update</Button>
          )}
          {canSignIn && (
            <Button size="sm" variant={s.updateAvailable ? "outline" : "default"} loading={busy === "login"} disabled={!!busy} onClick={() => void act.login(s.id)}><LogInIcon aria-hidden />Sign in</Button>
          )}
          {s.installed && s.auth === "signed_in" && (
            <Button size="sm" variant="outline" loading={busy === "logout"} disabled={!!busy} onClick={() => void act.logout(s.id)}><LogOutIcon aria-hidden />Sign out</Button>
          )}
        </div>
      </div>
      {busy === "install" && <p role="status" className="text-muted-foreground text-xs">Downloading {s.name}. This can take a minute.</p>}
      {busy === "login" && <p role="status" className="text-muted-foreground text-xs">Finish signing in in your browser.</p>}
      {s.installed && s.updateAvailable && s.source === "system" && (
        <p className="text-muted-foreground text-xs">Update adds Orole's own copy v{s.latest}. The one you installed yourself is left as it is.</p>
      )}
      {problem && <p role="alert" className="selectable break-words text-destructive-foreground text-xs">{problem}</p>}
      {s.problem && !problem && <p className="text-muted-foreground text-xs">Could not {s.problem}.</p>}
      <p className="text-muted-foreground text-xs">{NOTES[s.id]}</p>
    </li>
  );
}

/**
 * Accounts: the Claude, ChatGPT and Grok CLIs. Each one signs in with the user's own account and bots and Code use it
 * with that account's plan. Orole downloads, updates and starts the CLI but never sees the credentials.
 */
export function AccountProviders() {
  useCLIRuntimesSync();
  const statuses = useCLIRuntimes((s) => s.statuses);
  const ready = useCLIRuntimes((s) => s.ready);
  const error = useCLIRuntimes((s) => s.error);
  return (
    <section aria-labelledby="ai-accounts-title" className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-col gap-0.5">
        <h2 id="ai-accounts-title" className="text-sm font-medium">Accounts</h2>
        <p className="text-muted-foreground text-xs">Sign in with a plan you already pay for. No API key needed.</p>
      </div>
      {!ready && <Loading>Checking installed tools…</Loading>}
      {ready && error && <Failure error={error} retry={() => void useCLIRuntimes.getState().load()} />}
      {ready && !error && <ul className="divide-y rounded-lg border bg-card">{statuses.map((s) => <AccountRow key={s.id} s={s} />)}</ul>}
    </section>
  );
}
