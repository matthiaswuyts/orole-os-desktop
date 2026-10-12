import type { Ref } from "react";
import { ComposerAttachmentChip } from "@/components/assistant-ui/elements/composer-attachments";
import { PromptInput, PromptInputActions, PromptInputAddFiles, PromptInputAttachments, PromptInputBody, PromptInputFooter, PromptInputInset, PromptInputSubmit, PromptInputTextarea, PromptInputTools } from "@/components/blocks/prompt-input/prompt-input";
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

/**
 * The agent composer (HextaUI Prompt Input, agent harness layout): attachments, permission mode and the
 * agent/model/effort menu around the prompt; where it runs, the project and the worktree in the footer.
 * Used by Home and by the New task page, which also reads the launch state for its quick actions.
 */
export function HomeComposerView({ task, autoFocus = false, placeholder = DEFAULT_PLACEHOLDER, textareaRef, hideProjectMenu = false }: ComposerViewProps) {
  return <section aria-label="New AI task" className="mx-auto w-full min-w-0 max-w-[720px]">
    <PromptInput value={task.prompt} onValueChange={task.setPrompt} onSubmit={task.start} canSubmit={task.canStart} disabled={task.busy}
      onFiles={(files) => void task.attachments.add(files)} filesEnabled={task.attachments.canAdd}>
      <PromptInputInset>
        <PromptInputBody>
          {task.attachments.files.length > 0 && <PromptInputAttachments>
            {task.attachments.files.map((file) => <ComposerAttachmentChip key={file.key} attachment={file} onRemove={task.busy ? undefined : () => task.attachments.remove(file.key)} />)}
          </PromptInputAttachments>}
          <PromptInputTextarea ref={textareaRef} aria-label="Task for your agent" autoFocus={autoFocus} placeholder={placeholder} className="min-h-20" />
          <PromptInputTools>
            <PromptInputAddFiles hint="Add photos & files · up to 8 files, 2 MB each" />
            <PermissionModeSelect value={task.permissionMode} support={task.permissionSupport} disabled={task.busy} onChange={task.choosePermissionMode}
              className="h-8 min-h-8 rounded-full px-2.5 text-sm sm:h-7 sm:min-h-7 sm:text-sm" />
          </PromptInputTools>
          <PromptInputActions>
            <AgentModelPicker task={task} />
            <PromptInputSubmit label="Start task" loading={task.busy} />
          </PromptInputActions>
        </PromptInputBody>
        <PromptInputFooter>
          <SystemMenu task={task} />
          {!hideProjectMenu && <ProjectMenu task={task} />}
          <WorktreeMenu task={task} />
        </PromptInputFooter>
      </PromptInputInset>
      {(task.blocked || task.machine !== "local") && <div className="flex flex-col gap-1 px-2 text-xs text-muted-foreground">
        {task.blocked && <p role="status">{task.blocked}</p>}
        {task.machine !== "local" && <p>Remote tasks use that system's skills and MCP configuration.</p>}
      </div>}
    </PromptInput>
  </section>;
}
