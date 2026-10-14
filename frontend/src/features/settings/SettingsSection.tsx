import type { ReactNode } from "react";
import { cn } from "@/lib/utils";

/** A titled settings block whose rows share one bordered card. */
export function SettingsSection({ title, description, children, id }: { title: string; description?: string; children: ReactNode; id?: string }) {
  return (
    <section id={id} className="mb-5 scroll-mt-2">
      <h2 className={cn("text-sm font-medium", !description && "mb-2")}>{title}</h2>
      {description && <p className="mb-2 text-muted-foreground">{description}</p>}
      <div className="divide-y rounded-lg border bg-card">{children}</div>
    </section>
  );
}
