import { Component, type ErrorInfo, type ReactNode } from "react";
import { MonitorX, PlugZap, Hammer, TriangleAlert } from "lucide-react";
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

/**
 * Keeps one screen's crash, or a screen whose code failed to load (a dev server that re-bundled or restarted),
 * inside that screen instead of blanking the whole window. Key it by view so moving to another screen recovers.
 */
export class ViewBoundary extends Component<{ app: string; children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) { return { error }; }
  componentDidCatch(error: Error, info: ErrorInfo) { console.error(`${this.props.app} failed to render`, error, info.componentStack); }
  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    const unloaded = /dynamically imported module|Importing a module script failed|Loading chunk/i.test(error.message);
    return <Centered icon={<TriangleAlert />} title={unloaded ? `${this.props.app} could not be loaded` : `${this.props.app} stopped working`}
      action={{ label: "Reload window", onClick: () => window.location.reload() }}>
      <p>{unloaded ? "Its code could not be fetched, usually because the app was updated or the dev server restarted. Reload to fetch it again." : "Reloading the window usually recovers. Your tasks keep running in the daemon."}</p>
      <p className="mt-2 break-words font-mono text-xs">{error.message}</p>
    </Centered>;
  }
}
