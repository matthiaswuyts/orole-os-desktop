import { ChevronDown, FolderPlus } from "lucide-react";
import { useUI } from "@/stores/ui";

/**
 * The Projects section. Real projects come from the daemon's project registry (issue #46);
 * until a project is added it shows its empty state, never sample rows.
 */
export function SidebarProjects() {
  const setView = useUI((s) => s.setView);
  return (
    <section aria-label="Projects" className="mt-1 min-h-0 flex-1 overflow-auto">
      <div className="flex h-6 items-center gap-1 px-1 text-muted-foreground text-xs"><ChevronDown className="size-3" />Projects</div>
      <button type="button" onClick={() => setView("projects")} className="mt-1 flex h-side-row w-full items-center gap-2 rounded-md px-2 text-left text-muted-foreground text-sm outline-none hover:bg-sidebar-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring">
        <FolderPlus className="size-3.5" />Add a project
      </button>
    </section>
  );
}
