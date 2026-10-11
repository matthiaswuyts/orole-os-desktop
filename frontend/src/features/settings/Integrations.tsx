import { useEffect, useState, type ReactNode } from "react";
import { ArrowLeft, Cloud, Triangle } from "lucide-react";
import github from "@/assets/logos/github.svg?raw";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { GitHubSettings } from "@/features/github/GitHubSettings";
import { useGitHub } from "@/features/github/store";
import { inDesktop } from "@/lib/backend";
import { useUI } from "@/stores/ui";

interface Item { id: string; name: string; description: string; icon: ReactNode }

const ITEMS: Item[] = [
  // Vetted bundled SVG only, never server-supplied markup.
  { id: "github", name: "GitHub", description: "Repositories, issues, pull requests, notifications and CI.", icon: <span role="img" aria-label="GitHub" className="inline-flex size-5 [&>svg]:size-full" dangerouslySetInnerHTML={{ __html: github }} /> },
  { id: "vercel", name: "Vercel", description: "Deployments and projects.", icon: <Triangle className="size-5" aria-label="Vercel" /> },
  { id: "cloudflare", name: "Cloudflare", description: "Workers, Pages and DNS.", icon: <Cloud className="size-5" aria-label="Cloudflare" /> },
];

/** Vercel and Cloudflare have no backend yet: they are listed as not implemented, with no fake connection state. */
const NOT_IMPLEMENTED = new Set(["vercel", "cloudflare"]);

function Row({ item, status, onOpen }: { item: Item; status: ReactNode; onOpen?: () => void }) {
  return (
    <li className="flex items-center gap-3 px-3 py-3">
      <span className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-background">{item.icon}</span>
      <div className="min-w-0 flex-1">
        <span className="block font-medium">{item.name}</span>
        <span className="block text-xs text-muted-foreground">{item.description}</span>
      </div>
      {status}
      <Button size="sm" variant="outline" disabled={!onOpen} onClick={onOpen} aria-label={`${onOpen ? "Manage" : "Connect"} ${item.name}`}>{onOpen ? "Manage" : "Connect"}</Button>
    </li>
  );
}

/** Settings > Integrations: one list of services Orole can connect to. GitHub is built; the rest are placeholders. */
export function Integrations() {
  const requested = useUI((s) => s.settingsTab);
  const [open, setOpen] = useState<string | null>(requested === "github" ? "github" : null);
  const { status, load, startSync } = useGitHub();
  useEffect(() => { if (!inDesktop) return; void load(); return startSync(); }, [load, startSync]);

  if (open === "github") {
    return (
      <div className="flex flex-col gap-3">
        <Button size="sm" variant="ghost" className="self-start" onClick={() => setOpen(null)}><ArrowLeft />All integrations</Button>
        <GitHubSettings />
      </div>
    );
  }

  const githubStatus = !inDesktop
    ? <Badge variant="outline">Needs desktop app</Badge>
    : !status ? <Badge variant="outline">Checking</Badge>
    : status.connected ? <Badge variant="success">Connected{status.user?.login ? ` as ${status.user.login}` : ""}</Badge>
    : <Badge variant="outline">Not connected</Badge>;

  return (
    <ul aria-label="Integrations" className="divide-y rounded-lg border bg-card">
      {ITEMS.map((item) => NOT_IMPLEMENTED.has(item.id)
        ? <Row key={item.id} item={item} status={<Badge variant="outline">Not implemented</Badge>} />
        : <Row key={item.id} item={item} status={githubStatus} onOpen={inDesktop ? () => setOpen(item.id) : undefined} />)}
    </ul>
  );
}
