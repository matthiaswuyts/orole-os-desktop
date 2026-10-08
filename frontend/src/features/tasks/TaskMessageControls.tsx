import { useRef, useState } from "react";
import { ChevronDown, ChevronRight, PlusIcon, Zap } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Popover, PopoverPopup, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { RangeSlider } from "@/components/ui/range-slider";
import { Separator } from "@/components/ui/separator";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { ComposerAttachmentChip, ComposerAttachments } from "@/components/assistant-ui/elements/composer-attachments";
import { ProviderLogo } from "@/features/agents/ProviderLogo";
import { kindLabel } from "@/features/agents/format";
import { ModelSelectorSearch } from "@/features/home/ModelSelectorSearch";
import { effortChoices, effortLabel, modelLabel, providerLabel } from "@/features/home/model-options";
import type { TaskMessage } from "./useTaskMessage";

interface TaskMessageControlProps {
  message: TaskMessage;
  disabled?: boolean;
}

export function TaskAttachmentButton({ message, disabled = false }: TaskMessageControlProps) {
  const picker = useRef<HTMLInputElement>(null);
  const unavailable = disabled || message.busy || !message.attachments.canAdd;
  return <>
    <input ref={picker} type="file" multiple hidden aria-label="Attach files" disabled={unavailable}
      onChange={(event) => {
        if (!unavailable) void message.attachments.add(Array.from(event.target.files ?? []));
        event.target.value = "";
      }} />
    <Tooltip>
      <TooltipTrigger render={<Button type="button" variant="ghost" size="icon-sm" className="shrink-0 rounded-full" aria-label="Add attachment" disabled={unavailable} onClick={() => picker.current?.click()}><PlusIcon aria-hidden /></Button>} />
      <TooltipPopup>{message.attachmentReason || "Add photos & files · up to 8 files, 2 MB each"}</TooltipPopup>
    </Tooltip>
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
  const provider = message.harness ? providerLabel(message.harness) : kindLabel(message.session.kind);
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
    <PopoverTrigger disabled={unavailable} aria-label="Model and reasoning" render={<Button type="button" variant="ghost" className="h-8 min-w-0 gap-1.5 px-2 font-medium" />}>
      <ProviderLogo kind={message.session.kind} />
      <span className="max-w-20 truncate sm:max-w-36">{label}</span>
      {(message.levels.length > 0 || message.reasoning) && <span className="hidden shrink-0 text-muted-foreground sm:inline">{effortLabel(message.reasoning)}</span>}
      <ChevronDown aria-hidden className="shrink-0 opacity-60" />
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
        <Separator />
        <div className="flex flex-col gap-1 px-3 py-2.5 text-xs text-muted-foreground">
          <p>{provider} stays with this conversation. Settings apply to your next message.</p>
          {message.settingsReason && <p role="status">{message.settingsReason}</p>}
          {!message.settingsReason && !message.models.length && <p role="status">No model catalog reported by this agent.</p>}
        </div>
      </div>}
    </PopoverPopup>
  </Popover>;
}
