import { useEffect, useRef } from "react";
import { Check, Folder, ListTodo } from "lucide-react";
import { AsciiMark } from "@/components/ascii/AsciiMark";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { COLUMN_SPECS, columnOf } from "@/features/agents/columns";
import { ago, kindLabel } from "@/features/agents/format";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { StateGlyph } from "@/features/agents/shared";
import { HomeComposerView } from "@/features/home/HomeAgentComposer";
import { providerLabel } from "@/features/home/model-options";
import { useHomeTaskLaunch } from "@/features/home/useHomeTaskLaunch";
import { repoFromRemote } from "@/features/issues/repos";
import { useAgents } from "@/stores/agents";
import { useBoxes } from "@/stores/boxes";
import { useProjects } from "@/stores/projects";
import { openProjectTasks, openTask, projectTasks } from "./tasks";
import { GithubMark, OsMark, hostFamily } from "./BadgeIcons";

const row = "flex h-8 w-full items-center gap-3 rounded-lg px-2 text-left text-sm outline-none transition-colors hover:bg-muted/60 focus-visible:ring-2 focus-visible:ring-ring";
const chip = "inline-flex max-w-full items-center gap-1.5 rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-muted-foreground";

/** A project's New task page: the Home composer, quick ways to pick the agent, and tasks to pick back up. */
export function NewTaskView() {
  const { projectId, prompt, seq } = useProjects((state) => state.newTask);
  return <NewTaskPage key={seq} initialProjectId={projectId} initialPrompt={prompt} />;
}

function NewTaskPage({ initialProjectId, initialPrompt }: { initialProjectId: string | null; initialPrompt: string }) {
  const task = useHomeTaskLaunch({ launcher: "new-task", initialProjectId, initialPrompt });
  const close = useProjects((state) => state.closeNewTask);
  const registered = useProjects((state) => state.projects.find((project) => project.id === task.project?.id));
  const machines = useBoxes((state) => state.machines);
  const observations = useBoxes((state) => state.observations);
  const sessions = useAgents((state) => state.sessions);
  const input = useRef<HTMLTextAreaElement>(null);

  // Escape leaves the page once nothing is focused: the first press only blurs the input, popups handle their own.
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape" && document.activeElement === document.body) close(); };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [close]);

  const project = task.project;
  const repo = registered ? repoFromRemote(registered.remoteUrl) : "";
  const machineRecord = project ? machines.find((item) => item.id === project.machineId) : undefined;
  const machine = project ? machineRecord?.name ?? (project.machineId === "local" ? "This system" : project.machineId) : "";
  const family = project ? hostFamily(project.machineId, machineRecord, observations[project.machineId]?.health) : null;
  const recent = registered ? projectTasks(Object.values(sessions), registered).slice(0, 3) : [];
  const agents = task.harnesses.filter((harness) => harness.available);

  return <div className="h-full overflow-auto">
    <div className="mx-auto flex min-h-full w-full max-w-[640px] flex-col justify-center gap-4 px-6 py-12">
      <header className="flex flex-col gap-2">
        <AsciiMark align="start" />
        <h1 className="break-words font-heading font-semibold text-xl">{project?.name ?? "New task"}</h1>
        {project && <p className="flex flex-wrap items-center gap-1.5">
          {repo && <span className={chip}><GithubMark /><span className="truncate">{repo}</span></span>}
          <span className={chip}>{family && <OsMark family={family} />}<span className="truncate">{machine}</span></span>
          {project.path
            ? <Tooltip><TooltipTrigger render={<span className={chip} />}><Folder aria-hidden className="size-3 shrink-0" /><span className="truncate">{project.path}</span></TooltipTrigger><TooltipPopup className="max-w-sm break-all">{project.path}</TooltipPopup></Tooltip>
            : <span className={chip}><Folder aria-hidden className="size-3 shrink-0" /><span className="truncate">Managed workspace</span></span>}
        </p>}
      </header>

      <HomeComposerView task={task} autoFocus textareaRef={input} hideProjectMenu={!!project} placeholder={project ? `What should an agent do in ${project.name}?` : undefined} />

      <section aria-label="Quick actions">
        <ul className="flex flex-col">
          {agents.map((harness) => {
            const current = task.harness?.id === harness.id;
            return <li key={harness.id}>
              <button type="button" aria-pressed={current} disabled={task.busy} onClick={() => { task.chooseAgent(harness.id); input.current?.focus(); }} className={row}>
                <ProviderLogo kind={harness.id} /><span className="flex-1 truncate">New {providerLabel(harness)}</span>
                {current && <Check aria-label="Selected" className="size-3.5 text-muted-foreground" />}
              </button>
            </li>;
          })}
          {registered && <li>
            <button type="button" onClick={() => openProjectTasks(registered.id)} className={row}>
              <ListTodo aria-hidden className="size-4 text-muted-foreground" /><span className="flex-1 truncate">All tasks in {registered.name}</span>
            </button>
          </li>}
        </ul>
      </section>

      {recent.length > 0 && <section aria-label="Pick up where you left off" className="flex flex-col gap-1">
        <h2 className="px-2 text-muted-foreground text-xs">Pick up where you left off</h2>
        <ul className="flex flex-col">
          {recent.map((session) => <li key={session.id} className="flex h-8 items-center gap-3 rounded-lg px-2 text-sm hover:bg-muted/60">
            <ProviderLogo kind={session.kind} />
            <span className="min-w-0 truncate">{session.title}</span>
            <span className="shrink-0 text-muted-foreground text-xs">{kindLabel(session.kind)}</span>
            <span className="ml-auto inline-flex shrink-0 items-center gap-1.5 text-muted-foreground text-xs"><StateGlyph status={session.status} />{COLUMN_SPECS.find((column) => column.id === columnOf(session.status))!.title.toLowerCase()} {ago(session.updatedAt)}</span>
            <Button size="xs" variant="ghost" onClick={() => openTask(session.id)}>Open</Button>
          </li>)}
        </ul>
      </section>}
    </div>
  </div>;
}
