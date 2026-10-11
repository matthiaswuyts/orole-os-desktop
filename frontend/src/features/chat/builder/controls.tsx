import { useId, type ReactNode } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";
import { cn } from "@/lib/utils";

/** The executive-assistant option as a selectable card that says what the role does. */
export function ExecutiveCard({ checked, onChange }: { checked: boolean; onChange: (checked: boolean) => void }) {
  const id = useId();
  return (
    <Label className="flex items-start gap-2 rounded-lg border p-3 hover:bg-accent/50 has-data-checked:border-primary/48 has-data-checked:bg-accent/50">
      <Checkbox checked={checked} onCheckedChange={(next) => onChange(next === true)} aria-labelledby={`${id}-title`} aria-describedby={`${id}-hint`} />
      <div className="flex flex-col gap-1">
        <p id={`${id}-title`}>Executive assistant</p>
        <p id={`${id}-hint`} className="font-normal text-muted-foreground text-xs">
          Hands work to your other bots and talks with them to get it done, instead of doing everything itself.
        </p>
      </div>
    </Label>
  );
}

/** A labelled control with optional hint and inline validation message. */
export function Block({ label, hint, error, children, className, aside }: {
  label: string; hint?: ReactNode; error?: string; children: (id: string, describedBy: string | undefined) => ReactNode; className?: string; aside?: ReactNode;
}) {
  const id = useId();
  const describedBy = [hint ? `${id}-hint` : "", error ? `${id}-error` : ""].filter(Boolean).join(" ") || undefined;
  return (
    <div className={cn("flex min-w-0 flex-col gap-1.5", className)}>
      <div className="flex items-baseline justify-between gap-2">
        <label htmlFor={id} className="font-medium text-sm">{label}</label>
        {aside}
      </div>
      {children(id, describedBy)}
      {hint && <p id={`${id}-hint`} className="text-muted-foreground text-xs">{hint}</p>}
      {error && <p id={`${id}-error`} role="alert" className="text-destructive-foreground text-xs">{error}</p>}
    </div>
  );
}

/** A titled group inside a tab. */
export function Section({ title, description, children, id }: { title: string; description?: ReactNode; children: ReactNode; id?: string }) {
  const heading = useId();
  return (
    <section id={id} aria-labelledby={heading} className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-col gap-0.5">
        <h3 id={heading} className="font-medium text-sm">{title}</h3>
        {description && <p className="text-muted-foreground text-xs">{description}</p>}
      </div>
      {children}
    </section>
  );
}
