import { useId, useRef, useState, type FormEvent, type Ref } from "react";
import { ArrowUpIcon, PlusIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Field, FieldLabel } from "@/components/ui/field";
import { InputGroup, InputGroupAddon, InputGroupTextarea } from "@/components/ui/input-group";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { cn } from "@/lib/utils";
import { PermissionModeSelect } from "@/features/tasks/PermissionModeSelect";
import { ProjectMenu, SystemMenu, WorktreeMenu } from "./HomeTaskMenus";
import { AgentModelPicker } from "./AgentModelPicker";
import { useHomeTaskLaunch, type HomeTaskLaunch, type TaskLaunchOptions } from "./useHomeTaskLaunch";

const DEFAULT_PLACEHOLDER = "Describe a task, a bug to fix, an idea to try…";

interface ComposerViewProps {
  task: HomeTaskLaunch;
  autoFocus?: boolean;
  placeholder?: string;
  textareaRef?: Ref<HTMLTextAreaElement>;
  /** For a page that already fixed the project. */
  hideProjectMenu?: boolean;
}

/** The Home composer with its own launch state. */
export function HomeAgentComposer({ autoFocus, placeholder, ...launch }: TaskLaunchOptions & Omit<ComposerViewProps, "task" | "textareaRef">) {
  const task = useHomeTaskLaunch(launch);
  return <HomeComposerView task={task} autoFocus={autoFocus} placeholder={placeholder} />;
}

/** The composer for a caller that owns the launch state (the New task page also reads it for suggestions). */
export function HomeComposerView({ task, autoFocus = false, placeholder = DEFAULT_PLACEHOLDER, textareaRef, hideProjectMenu = false }: ComposerViewProps) {
  const id = useId();
  const picker = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  function submit(event: FormEvent) { event.preventDefault(); void task.start(); }
  return <section aria-label="New AI task" className="mx-auto w-full min-w-0 max-w-[720px]">
    <form onSubmit={submit}>
      <input ref={picker} type="file" multiple hidden aria-label="Attach files" disabled={!task.attachments.canAdd}
        onChange={(event) => { void task.attachments.add(Array.from(event.target.files ?? [])); event.target.value = ""; }} />
      <Field>
        <FieldLabel htmlFor={`${id}-prompt`} className="sr-only">Task for your agent</FieldLabel>
        <InputGroup className={cn("rounded-2xl bg-card! has-[input:disabled,textarea:disabled]:opacity-100", dragging && "ring-2 ring-ring")}
          onDragOver={(event) => { if (event.dataTransfer.types.includes("Files")) { event.preventDefault(); event.dataTransfer.dropEffect = task.attachments.canAdd ? "copy" : "none"; setDragging(task.attachments.canAdd); } }}
          onDragLeave={(event) => { if (!(event.relatedTarget instanceof Node) || !event.currentTarget.contains(event.relatedTarget)) setDragging(false); }}
          onDrop={(event) => { event.preventDefault(); setDragging(false); void task.attachments.add(Array.from(event.dataTransfer.files)); }}>
          {task.attachments.files.length > 0 && <InputGroupAddon align="block-start"><ComposerAttachments>
            {task.attachments.files.map((file) => <ComposerAttachmentChip key={file.key} attachment={file} onRemove={task.busy ? undefined : () => task.attachments.remove(file.key)} />)}
          </ComposerAttachments></InputGroupAddon>}
          <InputGroupTextarea ref={textareaRef} id={`${id}-prompt`} autoFocus={autoFocus} disabled={task.busy} value={task.prompt} rows={3}
            className="w-full [&_textarea]:max-h-64 [&_textarea]:min-h-28 [&_textarea]:resize-none [&_textarea]:px-4 [&_textarea]:py-4 [&_textarea]:text-base [&_textarea]:leading-relaxed"
            placeholder={placeholder}
            onChange={(event) => task.setPrompt(event.target.value)}
            onPaste={(event) => { const files = Array.from(event.clipboardData.files); if (files.length) { event.preventDefault(); void task.attachments.add(files); } }}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.nativeEvent.isComposing && event.nativeEvent.keyCode !== 229 && !event.altKey) {
                event.preventDefault(); if (!event.repeat) event.currentTarget.form?.requestSubmit();
              }
              if (event.key === "Escape") event.currentTarget.blur();
            }} />
          <InputGroupAddon align="block-end" className="flex-wrap gap-x-1 gap-y-2">
            <Tooltip><TooltipTrigger render={<Button type="button" variant="ghost" size="icon-sm" className="rounded-full" aria-label="Add attachment" disabled={!task.attachments.canAdd} onClick={() => picker.current?.click()}><PlusIcon /></Button>} />
              <TooltipPopup>Add photos &amp; files · up to 8 files, 2 MB each</TooltipPopup></Tooltip>
            {!hideProjectMenu && <ProjectMenu task={task} />}<SystemMenu task={task} /><WorktreeMenu task={task} />
            <PermissionModeSelect value={task.permissionMode} support={task.permissionSupport} disabled={task.busy} onChange={task.choosePermissionMode} />
            <div className="ml-auto flex min-w-0 items-center gap-2">
              <AgentModelPicker task={task} />
              <Button type="submit" disabled={!task.canStart} loading={task.busy} size="icon" className="shrink-0 rounded-full" aria-label="Start task"><ArrowUpIcon /></Button>
            </div>
          </InputGroupAddon>
        </InputGroup>
      </Field>
      {(task.blocked || task.machine !== "local") && <div className="mt-2 flex flex-col gap-1 px-1 text-xs text-muted-foreground">
        {task.blocked && <p role="status">{task.blocked}</p>}
        {task.machine !== "local" && <p>Remote tasks use that system's skills and MCP configuration.</p>}
      </div>}
    </form>
  </section>;
}
