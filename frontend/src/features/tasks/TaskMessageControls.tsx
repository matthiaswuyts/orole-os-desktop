import { useEffect, useRef, useState } from "react";
import { BlocksIcon, ChevronRight, PaperclipIcon, PlusIcon, ScrollTextIcon, SettingsIcon, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Menu, MenuItem, MenuPopup, MenuSeparator, MenuSub, MenuSubPopup, MenuSubTrigger, MenuTrigger } from "@/components/ui/menu";
import { MCP, Skills, type ServerState, type Skill } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useUI } from "@/stores/ui";
import { ServerLogo } from "@/features/integrations/ServerLogo";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { RangeSlider } from "@/components/ui/range-slider";
import { Separator } from "@/components/ui/separator";
import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { ModelSelectorSearch } from "@/features/home/ModelSelectorSearch";
import { effortChoices, effortLabel, modelLabel } from "@/features/home/model-options";
import type { TaskMessage } from "./useTaskMessage";

interface TaskMessageControlProps {
  message: TaskMessage;
  disabled?: boolean;
}

interface TaskAddMenuProps extends TaskMessageControlProps {
  /** Puts text into the draft; the composer owns the draft and its focus. */
  onInsert: (text: string) => void;
}

/** The + control: add files, or start the draft from a skill or a connected connector. Never opens the native picker by itself. */
export function TaskAddMenu({ message, disabled = false, onInsert }: TaskAddMenuProps) {
  const picker = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [skills, setSkills] = useState<Skill[] | null>(null);
  const [connectors, setConnectors] = useState<ServerState[] | null>(null);
  const unavailable = disabled || message.busy;
  const canAdd = !unavailable && message.attachments.canAdd;

  // Lists load when the menu first opens, not on every render of the composer.
  useEffect(() => {
    if (!open || !inDesktop) return;
    let live = true;
    if (skills === null) void Skills.list().then((list) => { if (live) setSkills(list.filter((skill) => skill.enabled)); }, () => { if (live) setSkills([]); });
    if (connectors === null) void MCP.list().then((list) => { if (live) setConnectors(list.filter((server) => server.enabled && server.status === "connected")); }, () => { if (live) setConnectors([]); });
    return () => { live = false; };
  }, [open, skills, connectors]);

  return <>
    <input ref={picker} type="file" multiple hidden aria-label="Attach files" disabled={!canAdd}
      onChange={(event) => {
        if (canAdd) void message.attachments.add(Array.from(event.target.files ?? []));
        event.target.value = "";
      }} />
    <Menu open={open} onOpenChange={setOpen}>
      <MenuTrigger disabled={unavailable} aria-label="Add to message" render={<Button type="button" variant="ghost" size="icon-sm" className="shrink-0 rounded-md text-muted-foreground hover:text-foreground" />}><PlusIcon aria-hidden /></MenuTrigger>
      <MenuPopup align="start" side="top" sideOffset={8} className="min-w-60">
        <MenuItem disabled={!canAdd} title={message.attachmentReason || undefined} onClick={() => picker.current?.click()}><PaperclipIcon />Add files or photos</MenuItem>
        <MenuSeparator />
        <MenuSub>
          <MenuSubTrigger><ScrollTextIcon />Skills</MenuSubTrigger>
          <MenuSubPopup className="min-w-56">
            {skills === null && inDesktop ? <p role="status" className="px-2 py-1.5 text-muted-foreground text-sm">Loading…</p>
              : skills?.length ? skills.map((skill) => <MenuItem key={skill.name} title={skill.description || undefined} onClick={() => onInsert(`Use the ${skill.name} skill to `)}><ScrollTextIcon />{skill.name}</MenuItem>)
              : <p role="status" className="px-2 py-1.5 text-muted-foreground text-sm">No skills enabled</p>}
            <MenuSeparator />
            <MenuItem onClick={() => useUI.getState().openSettings("skills")}><SettingsIcon />Manage skills</MenuItem>
          </MenuSubPopup>
        </MenuSub>
        <MenuSub>
          <MenuSubTrigger><BlocksIcon />Connectors</MenuSubTrigger>
          <MenuSubPopup className="min-w-56">
            {connectors === null && inDesktop ? <p role="status" className="px-2 py-1.5 text-muted-foreground text-sm">Loading…</p>
              : connectors?.length ? connectors.map((server) => <MenuItem key={server.id} onClick={() => onInsert(`Use the ${server.name} connector to `)}><ServerLogo server={server} className="size-4" />{server.name}</MenuItem>)
              : <p role="status" className="px-2 py-1.5 text-muted-foreground text-sm">No connectors connected</p>}
            <MenuSeparator />
            <MenuItem onClick={() => useUI.getState().openSettings("integrations")}><SettingsIcon />Manage connectors</MenuItem>
          </MenuSubPopup>
        </MenuSub>
      </MenuPopup>
    </Menu>
  </>;
}

export function TaskAttachmentPreviews({ message, disabled = false }: TaskMessageControlProps) {
  if (!message.attachments.files.length) return null;
  return <ComposerAttachments>
    {message.attachments.files.map((file) => <ComposerAttachmentChip key={file.key} attachment={file}
      onRemove={disabled || message.busy ? undefined : () => message.attachments.remove(file.key)} />)}
  </ComposerAttachments>;
}

export function TaskModelPicker({ message, disabled = false }: TaskMessageControlProps) {
  const [open, setOpen] = useState(false);
  const [page, setPage] = useState<"settings" | "models">("settings");
  const modelButton = useRef<HTMLButtonElement>(null);
  const model = message.models.find((option) => option.id === message.model);
  const label = modelLabel(model ?? (message.model ? { id: message.model, name: message.model } : undefined));
  const unavailable = disabled || message.busy;
  // Empty means keep the existing effort, so Auto is offered only while that is the session's actual setting.
  const choices = effortChoices(message.levels).filter((value) => value !== "" || !message.session.reasoning);
  const effortIndex = choices.indexOf(message.reasoning);
  const displayedChoices = effortIndex < 0 ? [message.reasoning, ...choices] : choices;
  function back() {
    setPage("settings");
    requestAnimationFrame(() => modelButton.current?.focus());
  }
  return <Popover open={open} onOpenChange={(next) => { setOpen(next); if (next) setPage("settings"); }}>
    <PopoverTrigger disabled={unavailable} aria-label="Model and reasoning" render={<Button type="button" variant="ghost" className="h-6 min-w-0 gap-1 rounded-md px-1.5 text-xs font-normal text-muted-foreground hover:text-foreground data-popup-open:text-foreground sm:text-xs" />}>
      <span className="inline-flex shrink-0 [&>span]:size-3.5 [&>svg]:size-3.5"><ProviderLogo kind={message.session.kind} /></span>
      <span className="max-w-24 truncate text-foreground sm:max-w-40">{label}</span>
      {(message.levels.length > 0 || message.reasoning) && <span className="hidden shrink-0 sm:inline">{effortLabel(message.reasoning)}</span>}
    </PopoverTrigger>
    <PopoverPopup side="top" align="end" sideOffset={8} className="w-80 max-w-[calc(100vw-2rem)] motion-reduce:transition-none [&_[data-slot=popover-viewport]]:p-0">
      <PopoverTitle className="sr-only">Model and reasoning</PopoverTitle>
      {page === "models" ? <ModelSelectorSearch
        kind="models"
        disabled={unavailable || !message.canChooseModel}
        selected={message.model}
        options={message.models.map((option) => ({ value: option.id, label: modelLabel(option), disabled: !!message.modelBlockedReason(option), description: message.modelBlockedReason(option) || (option.default ? "Provider default" : undefined) }))}
        onBack={back}
        onSelect={(id) => { if (unavailable || !message.canChooseModel) return; message.chooseModel(id); back(); }}
      /> : <div className="flex flex-col">
        <div className="flex flex-col gap-3 p-3">
          <div className="flex items-center gap-2">
            <span className="flex size-8 shrink-0 items-center justify-center" aria-hidden><ProviderLogo kind={message.session.kind} /></span>
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="font-medium">{effortLabel(message.reasoning)}</span>
              <span className="text-xs text-muted-foreground">Reasoning effort</span>
            </div>
            <Zap aria-hidden className="mr-1 size-4 text-muted-foreground" />
          </div>
          {message.levels.length > 0 ? <RangeSlider aria-label="Reasoning effort" min={0} max={displayedChoices.length - 1} step={1}
            value={displayedChoices.indexOf(message.reasoning)} disabled={unavailable || !message.canChooseReasoning}
            formatValueText={(index) => effortLabel(displayedChoices[index] ?? "")}
            onValueChange={(index) => { if (!unavailable) message.chooseReasoning(displayedChoices[index] ?? ""); }} />
            : <p className="text-xs text-muted-foreground">No effort control reported for this model.</p>}
        </div>
        <Separator />
        <div className="p-1.5">
          <Button ref={modelButton} type="button" variant="ghost" className="h-10 w-full justify-between gap-3 px-2.5" aria-label="Change model" disabled={unavailable || !message.canChooseModel} onClick={() => setPage("models")}>
            <span className="text-muted-foreground">Model</span>
            <span className="ml-auto truncate">{label}</span>
            <ChevronRight aria-hidden className="shrink-0 text-muted-foreground" />
          </Button>
        </div>
        {(message.settingsReason || !message.models.length) && <>
          <Separator />
          <p role="status" className="px-3 py-2.5 text-xs text-muted-foreground">{message.settingsReason || "No model catalog reported by this agent."}</p>
        </>}
      </div>}
    </PopoverPopup>
  </Popover>;
}
