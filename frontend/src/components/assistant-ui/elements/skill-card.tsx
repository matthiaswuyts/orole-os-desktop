"use client";

import type { ComponentProps, ReactNode } from "react";
import { CheckIcon, ChevronRightIcon, ScrollTextIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Collapsible, CollapsiblePanel, CollapsibleTrigger } from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import { mono, paper } from "./surfaces";

/** Where a skill proposal stands. `loading` is the first look at the live state; `failed` means that look did not work. */
export type SkillCardStatus = "loading" | "pending" | "approved" | "rejected" | "replaced" | "failed";

export function SkillCard({
  name,
  description,
  body,
  updates = false,
  status,
  busy = null,
  disabled = false,
  approvedNote,
  error,
  onAdd,
  onDismiss,
  onRetry,
  className,
  ...props
}: Omit<ComponentProps<"div">, "children"> & {
  name: string;
  description: string;
  /** The SKILL.md text, shown exactly as it will be saved. */
  body: string;
  /** True when this replaces a skill the bot created earlier. */
  updates?: boolean;
  status: SkillCardStatus;
  /** The request in flight, if any. Both buttons stay disabled while it runs. */
  busy?: "approve" | "reject" | null;
  /** Disables both buttons, e.g. when there is no daemon to ask. */
  disabled?: boolean;
  /** Shown once approved, e.g. "Added. Atlas can use it now." */
  approvedNote: string;
  /** What went wrong with the last Add/Dismiss (pending) or with loading the state (failed). */
  error?: string | null;
  onAdd: () => void;
  onDismiss: () => void;
  onRetry: () => void;
}) {
  const locked = disabled || busy !== null;
  return (
    <div role="group" aria-label={`Skill ${name}`} data-slot="skill-card" data-status={status} className={cn(paper, "flex w-full max-w-lg min-w-0 flex-col gap-3 rounded-2xl p-4", className)} {...props}>
      <div className="flex items-start gap-3">
        <span aria-hidden="true" className="flex size-9 shrink-0 items-center justify-center rounded-lg border bg-muted/40 text-muted-foreground">
          <ScrollTextIcon className="size-4.5" />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span data-slot="skill-card-name" className="selectable min-w-0 break-all font-mono text-[13px] font-medium">{name}</span>
            <Badge variant={updates ? "info" : "secondary"} size="sm">{updates ? "Update" : "New skill"}</Badge>
          </div>
          {description && <p data-slot="skill-card-description" className="selectable line-clamp-3 break-words text-[13px] text-muted-foreground">{description}</p>}
        </div>
      </div>

      <Collapsible className="flex flex-col gap-2">
        <CollapsibleTrigger className="group -ms-1 inline-flex w-fit items-center gap-1 rounded-md px-1 py-0.5 text-xs font-medium text-muted-foreground outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring">
          <ChevronRightIcon aria-hidden="true" className="size-3.5 transition-transform motion-reduce:transition-none group-data-panel-open:rotate-90" />
          View instructions
        </CollapsibleTrigger>
        <CollapsiblePanel className="motion-reduce:transition-none">
          <pre
            aria-label="Skill instructions"
            tabIndex={0}
            className={cn(mono, "selectable max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-lg border bg-muted/40 p-3 leading-5 text-foreground outline-none focus-visible:ring-2 focus-visible:ring-ring")}
          >{body}</pre>
        </CollapsiblePanel>
      </Collapsible>

      <Footer
        status={status}
        approvedNote={approvedNote}
        error={error}
      >
        {status === "pending" && (
          <div className="flex justify-end gap-2">
            <Button type="button" size="sm" variant="ghost" disabled={locked} onClick={onDismiss}>Dismiss</Button>
            <Button type="button" size="sm" disabled={locked} loading={busy === "approve"} onClick={onAdd}>Add skill</Button>
          </div>
        )}
        {status === "failed" && (
          <div className="flex justify-end">
            <Button type="button" size="sm" variant="outline" onClick={onRetry}>Retry</Button>
          </div>
        )}
      </Footer>
    </div>
  );
}

const MUTED_NOTES: Partial<Record<SkillCardStatus, string>> = {
  loading: "Checking this draft…",
  rejected: "Dismissed",
  replaced: "Replaced by a newer draft",
};

/** The card's outcome line (a polite live region, always mounted so changes are announced) plus any inline error and the actions. */
function Footer({ status, approvedNote, error, children }: { status: SkillCardStatus; approvedNote: string; error?: string | null; children: ReactNode }) {
  const note = status === "approved" ? approvedNote : MUTED_NOTES[status];
  return (
    <div className="flex flex-col gap-2">
      <p role="status" className={cn("flex items-center gap-1.5 text-xs", status === "approved" ? "text-success-foreground" : "text-muted-foreground", !note && "sr-only")}>
        {status === "approved" && <CheckIcon aria-hidden="true" className="size-3.5 shrink-0" />}
        {note}
      </p>
      {error && <p role="alert" data-slot="skill-card-error" className="selectable break-words text-xs text-destructive-foreground">{error}</p>}
      {children}
    </div>
  );
}
