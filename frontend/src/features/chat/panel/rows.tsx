import { ChevronLeftIcon, ChevronRightIcon, type LucideIcon } from "lucide-react";
import { useId, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";

/** A small caption over a rounded group of rows ("Brain", "Knows", "Does"). */
export function Group({ title, children, className }: { title: string; children: ReactNode; className?: string }) {
  const id = useId();
  return <section aria-labelledby={id} className={cn("flex min-w-0 flex-col gap-1.5", className)}>
    <h3 id={id} className="px-1 font-medium text-muted-foreground text-xs">{title}</h3>
    <div className="flex flex-col divide-y overflow-hidden rounded-xl border bg-card/60">{children}</div>
  </section>;
}

const rowClass = "flex min-h-10 w-full min-w-0 items-center gap-2.5 px-3 py-1.5 text-left text-sm";

function Lead({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return <>
    <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
    <span className="min-w-0 flex-1 truncate">{label}</span>
  </>;
}

/** A row that opens a page of its own: icon, name, a quiet value ("0 saved") and a chevron. An invalid row says so in red. */
export function NavRow({ icon, label, value, invalid, onOpen }: { icon: LucideIcon; label: string; value?: string; invalid?: boolean; onOpen: () => void }) {
  return <button type="button" onClick={onOpen} aria-invalid={invalid || undefined} className={cn(rowClass, "outline-none hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-inset")}>
    <Lead icon={icon} label={label} />
    {value && <span className={cn("shrink-0 text-xs", invalid ? "text-destructive-foreground" : "text-muted-foreground")}>{value}</span>}
    <ChevronRightIcon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
  </button>;
}

/** A row whose right side is a control (a picker, a select). The label names the control for assistive tech. */
export function ControlRow({ icon, label, htmlFor, children }: { icon: LucideIcon; label: string; htmlFor?: string; children: ReactNode }) {
  const Icon = icon;
  return <div className={cn(rowClass, "justify-between")}>
    <label htmlFor={htmlFor} className="flex min-w-0 shrink-0 items-center gap-2.5">
      <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
      <span>{label}</span>
    </label>
    <div className="flex min-w-0 flex-1 justify-end">{children}</div>
  </div>;
}

/** A row with a switch, for a yes/no setting that needs a sentence of explanation. */
export function SwitchRow({ icon, label, hint, checked, onChange }: { icon: LucideIcon; label: string; hint?: string; checked: boolean; onChange: (on: boolean) => void }) {
  const id = useId();
  const Icon = icon;
  return <div className={cn(rowClass, "py-2")}>
    <Icon aria-hidden className="size-4 shrink-0 text-muted-foreground" />
    <div className="flex min-w-0 flex-1 flex-col">
      <span id={`${id}-name`}>{label}</span>
      {hint && <span id={`${id}-hint`} className="text-muted-foreground text-xs">{hint}</span>}
    </div>
    <Switch checked={checked} aria-labelledby={`${id}-name`} aria-describedby={hint ? `${id}-hint` : undefined} onCheckedChange={onChange} />
  </div>;
}

/** The top of a sub-page: a way back and the page's name. */
export function PageHeader({ title, onBack }: { title: string; onBack: () => void }) {
  return <div className="flex items-center gap-1 pb-1">
    <Button type="button" variant="ghost" size="icon-xs" aria-label="Back to settings" onClick={onBack}><ChevronLeftIcon aria-hidden /></Button>
    <h2 className="font-medium text-sm">{title}</h2>
  </div>;
}
