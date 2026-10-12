import { Search } from "lucide-react";
import { useUI } from "@/stores/ui";
import { Kbd, KbdGroup } from "@/components/ui/kbd";

/** The sidebar's palette entry point, shared by both modes. */
export function SidebarSearch() {
  const setPalette = useUI((s) => s.setPalette);
  return (
    <button type="button" onClick={() => setPalette(true)} aria-label="Search (Ctrl+K)" className="flex h-7 shrink-0 items-center gap-2 rounded-md border bg-background/40 px-2 text-muted-foreground text-sm outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
      <Search className="size-3.5" /><span className="flex-1 text-left">Search</span><KbdGroup aria-label="Ctrl+K"><Kbd>Ctrl</Kbd><Kbd>K</Kbd></KbdGroup>
    </button>
  );
}
