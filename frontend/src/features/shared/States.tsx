import type { ReactNode } from "react";
import { MonitorX, PlugZap, Hammer } from "lucide-react";
import { Button } from "@/components/ui/button";

function Centered({ icon, title, children, action }: { icon: ReactNode; title: string; children?: ReactNode; action?: { label: string; onClick: () => void } }) {
  return (
    <div role="status" className="mx-auto my-auto flex max-w-md flex-col items-center gap-2 rounded-lg border bg-card p-8 text-center">
      <div className="text-muted-foreground [&_svg]:size-7">{icon}</div>
      <h2 className="text-sm font-medium">{title}</h2>
      <div className="text-muted-foreground">{children}</div>
      {action && <Button variant="outline" onClick={action.onClick} className="mt-2">{action.label}</Button>}
    </div>
  );
}

export function NeedsDesktop({ what }: { what: string }) {
  return (
    <Centered icon={<MonitorX />} title={`${what} needs the desktop app`}>
      <p>This is a browser preview with no daemon behind it. Launch Orole OS (<code className="font-mono">go tool mygo dev</code> or the installed app) to use it.</p>
    </Centered>
  );
}

export function NotConnected({ app, what, action }: { app: string; what: string; action?: { label: string; onClick: () => void } }) {
  return <Centered icon={<PlugZap />} title={`${app} is not connected`} action={action}><p>{what}</p></Centered>;
}

/** For modules whose backend is not built yet. Says so plainly. */
export function NotImplemented({ app, issue, children }: { app: string; issue?: string; children?: ReactNode }) {
  return (
    <Centered icon={<Hammer />} title={`${app} is not built yet`}>
      <p>{children ?? "This module has no working backend in this build, so it shows nothing rather than sample data."}</p>
      {issue && <p className="mt-1 font-mono text-xs">Tracked in {issue}</p>}
    </Centered>
  );
}
