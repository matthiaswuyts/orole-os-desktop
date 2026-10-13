import { ChevronRightIcon, ShieldCheckIcon } from "lucide-react";
import type { ComponentProps, ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { surfaces } from "./surfaces";

/** What the user chose. `session` and `always` exist for hosts that really persist such a grant; the app offers only what its backend can honour. */
export type GrantScope = "once" | "session" | "always" | "denied";
export interface GrantReach {
  /** "Command", "Folder", "Files": names the line, for people and screen readers. */
  label?: string;
  /** Rendered as inert text, scrollable and wrapped, never truncated. The caller has already made control characters visible. */
  value: string;
}

const grantLabel: Record<Exclude<GrantScope, "denied">, string> = { once: "Allow once", session: "This session", always: "Always" };
const resolvedLabel: Record<GrantScope | "closed", string> = { once: "Allowed once", session: "Allowed for this session", always: "Always allowed", denied: "Denied", closed: "No longer pending" };

/**
 * assistant-ui's permission grant (https://www.assistant-ui.com/elements/permission-grant), standalone lane: what is being
 * asked for, who is asking, and exactly what saying yes lets happen, before the user commits. Fully controlled: the host owns
 * the decision (`scope`) and what it grants. Pending buttons render only while `scope` is `"pending"` and `onGrant` is supplied.
 *
 * Adapted to a single request: `grants` lists only the scopes the backend offers (default just `once`), `blocked` explains a
 * request that cannot be allowed (Deny stays available), and `details` is a disclosure for the raw request.
 */
export function PermissionGrant({ capability, requester, reach, scope, onGrant, grants = ["once"], busy = false, blocked, error, reason, details, className, ...props }: Omit<ComponentProps<"div">, "children"> & {
  capability: string;
  requester: string;
  reach: readonly (string | GrantReach)[];
  /** `closed`: no longer pending, but this client holds no decision receipt for it. */
  scope: GrantScope | "pending" | "closed";
  onGrant?: (scope: GrantScope) => void;
  grants?: readonly Exclude<GrantScope, "denied">[];
  busy?: boolean;
  /** Why this request cannot be allowed. */
  blocked?: string;
  error?: string | null;
  /** The reason attached to the recorded decision, when there is one. */
  reason?: string;
  details?: ReactNode;
}) {
  const pending = scope === "pending";
  return (
    <div role="group" aria-label={`Permission request: ${capability}`} data-slot="permission-grant" data-scope={scope} className={cn(surfaces.paper, "flex min-w-0 flex-col gap-2 p-3 text-xs", className)} {...props}>
      <div className="flex min-w-0 items-center gap-2">
        <ShieldCheckIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0 truncate text-sm font-medium">{capability}</span>
        <span className="min-w-0 shrink truncate text-muted-foreground">requested by {requester}</span>
      </div>
      {reach.length > 0 && <div data-slot="permission-reach" className="flex min-w-0 flex-col gap-1">
        {pending && <span className={surfaces.mono}>This allows</span>}
        {reach.map((item, index) => {
          const { label, value } = typeof item === "string" ? { label: undefined, value: item } : item;
          return <div key={index} className="min-w-0">
            {label && <span className="mb-0.5 block text-muted-foreground">{label}</span>}
            <pre aria-label={label} tabIndex={0} className="selectable m-0 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-md bg-muted/40 px-2 py-1.5 font-mono outline-none focus-visible:ring-2 focus-visible:ring-ring">{value}</pre>
          </div>;
        })}
      </div>}
      {blocked && <p role="alert" className="text-destructive-foreground">{blocked}</p>}
      {details && <Collapsible>
        <CollapsibleTrigger className="group/details flex items-center gap-1 rounded-md text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
          <ChevronRightIcon aria-hidden className="size-3 transition-transform group-data-[panel-open]/details:rotate-90 motion-reduce:transition-none" />Details
        </CollapsibleTrigger>
        <CollapsiblePanel><div className="mt-1.5 min-w-0">{details}</div></CollapsiblePanel>
      </Collapsible>}
      {pending ? onGrant && <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => onGrant("denied")}>Deny</Button>
        {grants.map((grant, index) => <Button key={grant} type="button" size="sm" variant={index === 0 ? "default" : "outline"} disabled={!!blocked || busy} loading={busy && index === 0} onClick={() => onGrant(grant)}>{grantLabel[grant]}</Button>)}
      </div> : <p role="status" data-slot="permission-resolved" key={scope} className="flex flex-wrap items-center gap-x-1.5 text-muted-foreground">
        <span className={cn(surfaces.field, "px-2 py-0.5 font-medium", scope === "once" || scope === "session" || scope === "always" ? "text-success-foreground" : "text-foreground")}>{resolvedLabel[scope]}</span>
        {reason && <span className="selectable min-w-0 whitespace-pre-wrap break-words">{reason}</span>}
      </p>}
      {error && <p role="alert" className="whitespace-pre-wrap break-words text-destructive-foreground">{error}</p>}
    </div>
  );
}
