import { NAV } from "@/lib/nav";
import { cn } from "@/lib/utils";
import { useUI } from "@/stores/ui";
import { Tip } from "@/components/tip";

/** Sidebar after Shipyard's app-sidebar: bg-sidebar, hairline right border, h-side-row rows. */
export function Sidebar() {
  const { view, setView } = useUI();
  const row = (n: (typeof NAV)[number]) => (
    <Tip key={n.id} label={n.label} side="right">
      <button type="button" aria-label={n.label} aria-current={view === n.id ? "page" : undefined} onClick={() => setView(n.id)}
        className={cn("flex h-side-row w-full items-center gap-2 rounded-md px-2 text-left text-sidebar-foreground text-sm outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring",
          view === n.id && "bg-sidebar-accent text-sidebar-accent-foreground")}>
        <n.icon className="size-3.5 shrink-0" />
        <span className="truncate max-[1000px]:hidden">{n.label}</span>
      </button>
    </Tip>
  );
  return (
    <nav aria-label="Applications" className="flex w-52 shrink-0 flex-col border-r bg-sidebar p-2 max-[1000px]:w-11 max-[1000px]:px-1.5">
      <div className="flex flex-1 flex-col gap-0.5">{NAV.filter((n) => n.group === "main").map(row)}</div>
      <div className="flex flex-col gap-0.5 border-t pt-2">{NAV.filter((n) => n.group === "bottom").map(row)}</div>
    </nav>
  );
}
