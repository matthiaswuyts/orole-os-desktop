import { useState } from "react";
import { Field as FieldPrimitive } from "@base-ui/react/field";
import type { PermissionModeSupport } from "@/mygo";
import { AlertDialog, AlertDialogClose, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogPopup, AlertDialogTitle } from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { toastManager } from "@/components/ui/toast";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { errText } from "@/features/agents/format";
import { cn } from "@/lib/utils";
import { modeSupport, normalizePermissionMode, type PermissionMode } from "./permissionModes";

const MODES: Record<PermissionMode, { label: string; description: string }> = {
  manual: { label: "Manual", description: "Asks before anything it isn't sure is safe." },
  auto: { label: "Auto", description: "The agent decides which actions are safe." },
  yolo: { label: "Yolo", description: "Never asks. Everything runs without a prompt." },
};
const ORDER: readonly PermissionMode[] = ["manual", "auto", "yolo"];

interface PermissionModeSelectProps {
  value: string;
  /**
   * Applies the choice. YOLO arrives with confirmYolo true, only after the person confirmed the dialog.
   * Rejecting reports the failure: inline in the dialog for YOLO, as a toast otherwise.
   */
  onChange: (mode: PermissionMode, confirmYolo: boolean) => Promise<void> | void;
  /** How the harness realises each mode (Detection / HarnessOption.permissionModes). */
  support?: readonly PermissionModeSupport[];
  disabled?: boolean;
  /** Shown in the tooltip while disabled. */
  disabledReason?: string;
  className?: string;
}

/** Compact Manual / Auto / YOLO control. The daemon enforces the YOLO confirmation; the dialog only asks for it. */
export function PermissionModeSelect({ value, onChange, support, disabled = false, disabledReason = "", className }: PermissionModeSelectProps) {
  const current = normalizePermissionMode(value);
  const [confirming, setConfirming] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const readOnly = current === "plan";
  const selected: PermissionMode = readOnly ? "manual" : current;
  const unavailable = disabled || pending || readOnly;
  const view = readOnly ? { label: "Read-only", description: "This review session cannot run or change anything." } : MODES[selected];
  const approximated = !readOnly && modeSupport(support, selected).approximated;
  const hint = disabled ? disabledReason : readOnly ? view.description : `${view.label}: ${approximated ? modeSupport(support, selected).detail : view.description}`;

  async function choose(next: string | null) {
    if (!next || next === selected || (next !== "manual" && next !== "auto" && next !== "yolo")) return;
    if (next === "yolo") { setError(null); setConfirming(true); return; }
    setPending(true);
    try { await onChange(next, false); }
    catch (failure) { toastManager.add({ type: "error", title: "Could not change permission mode", description: errText(failure), timeout: 8000 }); }
    finally { setPending(false); }
  }
  async function confirmYolo() {
    setPending(true);
    setError(null);
    try { await onChange("yolo", true); setConfirming(false); }
    catch (failure) { setError(errText(failure)); }
    finally { setPending(false); }
  }

  // Its own Field scope: inside a launcher's Field this select would otherwise adopt that field's label and id.
  return <FieldPrimitive.Root render={<span className="contents" />}>
    <Select value={selected} disabled={unavailable} onValueChange={(next) => void choose(next)}>
      <Tooltip>
        <TooltipTrigger render={<span className="inline-flex min-w-0" />}>
          <SelectTrigger size="sm" aria-label={`Permission mode: ${view.label}`}
            className={cn("h-6 min-h-6 w-auto min-w-0 gap-1 rounded-md border-transparent bg-transparent px-1.5 text-xs font-normal text-muted-foreground shadow-none not-data-disabled:hover:text-foreground before:hidden data-popup-open:text-foreground motion-reduce:transition-none sm:text-xs dark:bg-transparent [&_[data-slot=select-icon]]:hidden", className)}>
            <SelectValue>{() => view.label}</SelectValue>
          </SelectTrigger>
        </TooltipTrigger>
        {hint && <TooltipPopup>{hint}</TooltipPopup>}
      </Tooltip>
      <SelectPopup side="top" align="end" sideOffset={8} alignItemWithTrigger={false} className="w-64 max-w-[calc(100vw-2rem)]">
        {ORDER.map((mode) => {
          const info = MODES[mode];
          const entry = modeSupport(support, mode);
          return <SelectItem key={mode} value={mode} disabled={!entry.available} className="items-start py-1.5">
            <span className="flex min-w-0 flex-col gap-0.5">
              <span className="flex items-center gap-1.5 font-medium">
                {info.label}
                {entry.approximated && <Badge variant="outline" size="sm">Approximated</Badge>}
              </span>
              <span className="text-xs font-normal text-muted-foreground">{!entry.available || entry.approximated ? entry.detail || info.description : info.description}</span>
            </span>
          </SelectItem>;
        })}
      </SelectPopup>
    </Select>
    <AlertDialog open={confirming} onOpenChange={(open) => { if (!pending) setConfirming(open); }}>
      <AlertDialogPopup>
        <AlertDialogHeader>
          <AlertDialogTitle>Run without asking?</AlertDialogTitle>
          <AlertDialogDescription render={<div className="flex flex-col gap-2" />}>
            <p>In Yolo mode the agent never asks before it runs commands, edits files or uses tools. No prompts will be shown, so nothing can be reviewed first.</p>
            <p>Connected services still follow your Orole permission rules. Use Yolo only for work you would let the agent finish unattended.</p>
          </AlertDialogDescription>
        </AlertDialogHeader>
        {error && <p role="alert" className="px-6 pb-4 text-sm whitespace-pre-wrap break-words text-destructive-foreground">{error}</p>}
        <AlertDialogFooter>
          <AlertDialogClose render={<Button variant="outline" disabled={pending} />}>Cancel</AlertDialogClose>
          <Button variant="destructive" loading={pending} onClick={() => void confirmYolo()}>Enable Yolo</Button>
        </AlertDialogFooter>
      </AlertDialogPopup>
    </AlertDialog>
  </FieldPrimitive.Root>;
}
