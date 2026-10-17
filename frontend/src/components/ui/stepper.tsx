import { useId } from "react";
import { CheckIcon } from "lucide-react";
import { motion, useReducedMotion } from "motion/react";
import { cn } from "@/lib/utils";

export interface StepperStep {
  id: string;
  label: string;
  /** Finished: drawn as a check and fills the line up to the next step. */
  done?: boolean;
}

/**
 * A horizontal step indicator. The line fills and the active marker springs as `current` moves; every step is a
 * button, so a user can jump back to fix an earlier one. The panel of the active step is the caller's to render.
 */
export function Stepper({ steps, current, onSelect, className, "aria-label": label = "Setup steps" }: {
  steps: readonly StepperStep[];
  current: number;
  onSelect: (index: number) => void;
  className?: string;
  "aria-label"?: string;
}) {
  const reduced = useReducedMotion();
  const uid = useId();
  const transition = reduced ? { duration: 0 } : { type: "spring" as const, stiffness: 380, damping: 30 };
  return (
    <ol aria-label={label} className={cn("flex w-full items-start", className)}>
      {steps.map((step, i) => {
        const active = i === current;
        const statusId = `${uid}-${step.id}`;
        return (
          <li key={step.id} className={cn("flex min-w-0 items-start", i < steps.length - 1 && "flex-1")}>
            <button
              type="button"
              aria-current={active ? "step" : undefined}
              aria-describedby={statusId}
              onClick={() => onSelect(i)}
              className="group flex w-16 shrink-0 cursor-pointer flex-col items-center gap-1 rounded-md px-1 pb-1 outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <motion.span
                aria-hidden
                data-done={step.done || undefined}
                animate={{ scale: active ? 1.12 : 1 }}
                transition={transition}
                className={cn(
                  "relative grid size-7 place-items-center rounded-full border text-xs font-medium transition-colors",
                  step.done ? "border-primary bg-primary text-primary-foreground"
                    : active ? "border-primary text-foreground" : "border-border text-muted-foreground group-hover:text-foreground",
                )}
              >
                {step.done
                  ? <motion.span initial={reduced ? false : { scale: 0 }} animate={{ scale: 1 }} transition={transition}><CheckIcon aria-hidden className="size-3.5" /></motion.span>
                  : i + 1}
                {active && !step.done && <motion.span layoutId={`${uid}-ring`} transition={transition} className="absolute -inset-1 rounded-full border border-primary/40" />}
              </motion.span>
              <span className={cn("max-w-full truncate text-xs", active ? "font-medium text-foreground" : "text-muted-foreground")}>{step.label}</span>
            </button>
            <span id={statusId} className="sr-only">{step.done ? "Done" : "Not done"}</span>
            {i < steps.length - 1 && (
              <div aria-hidden className="relative mt-3.5 h-0.5 min-w-3 flex-1 overflow-hidden rounded-full bg-border">
                <motion.div
                  className="absolute inset-0 origin-left bg-primary"
                  initial={false}
                  animate={{ scaleX: step.done ? 1 : 0 }}
                  transition={reduced ? { duration: 0 } : { duration: 0.35, ease: "easeOut" }}
                />
              </div>
            )}
          </li>
        );
      })}
    </ol>
  );
}
