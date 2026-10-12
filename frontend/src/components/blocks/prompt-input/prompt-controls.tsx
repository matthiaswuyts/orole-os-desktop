/*
 * Adapted from the HextaUI Prompt Input block (prompt-controls.tsx), https://hextaui.com/blocks/prompt-input.
 * Used under the HextaUI Pro License. See docs/research/upstream-attribution.md.
 */
import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode, type Ref } from "react";
import { CircleHelpIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Menu, MenuGroup, MenuGroupLabel, MenuPopup, MenuRadioGroup, MenuRadioItem, MenuSeparator, MenuTrigger } from "@/components/ui/menu";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";
import { EffortFill } from "./effort-fill";
import { prefersReducedMotion, usePromptInput } from "./context";
import { useEffortLevel, type PromptEffort } from "./effort";

const spring = "ease-[cubic-bezier(0.34,1.36,0.64,1)]";
const easeOut = "cubic-bezier(0.215, 0.61, 0.355, 1)";

export interface PromptOption {
  value: string;
  label: string;
  icon?: ReactNode;
  description?: string;
  disabled?: boolean;
}

/** A compact picker for where the agent runs, its folder or its worktree. */
export function PromptInputSelect({ options, value, onValueChange, label, placeholder, icon, heading, disabled = false, side = "top", align = "start", className, children }: {
  options: PromptOption[];
  value?: string;
  onValueChange: (value: string) => void;
  /** Accessible name and tooltip, like "Run on". */
  label: string;
  placeholder?: string;
  /** Shown when the selected option has no icon. */
  icon?: ReactNode;
  heading?: string;
  disabled?: boolean;
  side?: "top" | "bottom";
  align?: "start" | "end";
  className?: string;
  /** Extra menu items below the options, like "Create new project…". */
  children?: ReactNode;
}) {
  const context = usePromptInput("PromptInputSelect");
  const current = options.find((option) => option.value === value);
  const shown = current?.label ?? placeholder ?? label;
  return <Menu>
    <Tooltip>
      <TooltipTrigger render={<MenuTrigger render={<Button type="button" size="sm" variant="ghost" aria-label={`${label}: ${shown}`} disabled={disabled || context.disabled}
        className={cn("min-w-0 rounded-full font-normal text-muted-foreground data-popup-open:text-foreground", className)} />} />}>
        {(current?.icon ?? icon) && <span className="flex shrink-0 [&_svg]:size-4">{current?.icon ?? icon}</span>}
        <span className="max-w-40 truncate">{shown}</span>
      </TooltipTrigger>
      <TooltipPopup>{label}</TooltipPopup>
    </Tooltip>
    <MenuPopup side={side} align={align} sideOffset={8} finalFocus={context.textareaRef} className="min-w-56 max-w-[calc(100vw-2rem)]">
      <MenuGroup>
        {heading && <MenuGroupLabel>{heading}</MenuGroupLabel>}
        <MenuRadioGroup value={value ?? ""} onValueChange={(next) => onValueChange(String(next))}>
          {options.map((option) => <MenuRadioItem key={option.value} value={option.value} disabled={option.disabled} closeOnClick>
            <span className="flex min-w-0 items-center gap-2">
              {option.icon && <span className="flex shrink-0 [&_svg]:size-4">{option.icon}</span>}
              <span className="min-w-0 truncate">{option.label}</span>
              {option.description && <span className="ms-auto shrink-0 ps-3 text-xs text-muted-foreground">{option.description}</span>}
            </span>
          </MenuRadioItem>)}
        </MenuRadioGroup>
      </MenuGroup>
      {children && <><MenuSeparator />{children}</>}
    </MenuPopup>
  </Menu>;
}

interface FlowState { current: string; previous: string | null; direction: 1 | -1; key: number }

/** A label that rolls its characters up or down when it changes. */
export function FlowText({ value, direction, className }: { value: string; direction: 1 | -1; className?: string }) {
  const [state, setState] = useState<FlowState>({ current: value, previous: null, direction, key: 0 });
  const incomingRef = useRef<HTMLSpanElement>(null);
  const outgoingRef = useRef<HTMLSpanElement>(null);
  if (value !== state.current) setState({ current: value, previous: state.current, direction, key: state.key + 1 });

  useLayoutEffect(() => {
    if (state.previous === null) return;
    const incoming = Array.from(incomingRef.current?.children ?? []) as HTMLElement[];
    const outgoing = Array.from(outgoingRef.current?.children ?? []) as HTMLElement[];
    if (prefersReducedMotion() || typeof incoming[0]?.animate !== "function") {
      setState((last) => ({ ...last, previous: null }));
      return;
    }
    const shift = `${state.direction * 100}%`;
    const back = `${state.direction * -100}%`;
    const animations = [
      ...outgoing.map((char, index) => char.animate(
        [{ translate: "0 0", opacity: 1, filter: "blur(0)" }, { translate: `0 ${back}`, opacity: 0, filter: "blur(2px)" }],
        { duration: 220, delay: index * 18, easing: easeOut, fill: "both" })),
      ...incoming.map((char, index) => char.animate(
        [{ translate: `0 ${shift}`, opacity: 0, filter: "blur(2px)" }, { translate: "0 0", opacity: 1, filter: "blur(0)" }],
        { duration: 360, delay: index * 22, easing: easeOut, fill: "backwards" })),
    ];
    let active = true;
    Promise.all(animations.map((animation) => animation.finished)).then(() => { if (active) setState((last) => ({ ...last, previous: null })); }, () => {});
    return () => { active = false; };
  }, [state.key, state.direction, state.previous]);

  return <span className={cn("relative inline-flex overflow-hidden whitespace-pre", className)}>
    <span className="sr-only">{state.current}</span>
    <span key={`in-${state.key}`} ref={incomingRef} aria-hidden className="inline-flex">
      {Array.from(state.current).map((char, index) => <span key={index} className="inline-block">{char}</span>)}
    </span>
    {state.previous !== null && <span key={`out-${state.key}`} ref={outgoingRef} aria-hidden className="absolute inset-y-0 start-0 inline-flex">
      {Array.from(state.previous).map((char, index) => <span key={index} className="inline-block">{char}</span>)}
    </span>}
  </span>;
}

/** Reserves the width of the widest level so a trigger does not jump as the level changes. */
export function EffortWidth({ levels }: { levels: PromptEffort[] }) {
  return levels.map((level) => <span key={level.value} aria-hidden className="invisible">{level.label}</span>);
}

function EffortTrack({ levels, index, onIndexChange, disabled, label, sliderRef }: {
  levels: PromptEffort[];
  index: number;
  onIndexChange: (index: number) => void;
  disabled: boolean;
  label: string;
  sliderRef?: Ref<HTMLDivElement>;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [dragging, setDragging] = useState(false);
  const [popped, setPopped] = useState<number | null>(null);
  const last = levels.length - 1;
  const max = last > 0 && index === last;

  // Thumb and fill follow the pointer continuously while dragging, and snap to the level otherwise.
  const place = useCallback((share: number) => {
    const root = rootRef.current;
    if (!root) return;
    root.style.setProperty("--effort-thumb", `calc((100% - 2rem) * ${share} + 0.125rem)`);
    root.style.setProperty("--effort-fill", `calc((100% - 2rem) * ${share} + 2rem)`);
  }, []);
  useLayoutEffect(() => { if (!dragging) place(last > 0 ? index / last : 0); }, [index, last, dragging, place]);
  useEffect(() => {
    if (popped === null) return;
    const timer = setTimeout(() => setPopped(null), 240);
    return () => clearTimeout(timer);
  }, [popped]);

  const commit = (next: number) => {
    if (next === index || disabled) return;
    onIndexChange(next);
    setPopped(next);
  };
  const follow = (track: HTMLElement, clientX: number) => {
    const box = track.getBoundingClientRect();
    let share = (clientX - box.left - 16) / Math.max(1, box.width - 32);
    if (getComputedStyle(track).direction === "rtl") share = 1 - share;
    share = Math.min(1, Math.max(0, share));
    place(share);
    commit(Math.round(share * last));
  };

  return <div ref={rootRef} className="flex flex-col">
    <div ref={sliderRef} role="slider" tabIndex={disabled ? -1 : 0} aria-label={label} aria-disabled={disabled || undefined}
      aria-valuemin={0} aria-valuemax={last} aria-valuenow={index} aria-valuetext={levels[index]?.label}
      data-dragging={dragging ? "" : undefined}
      onPointerDown={(event) => {
        if (event.button !== 0 || disabled) return;
        event.currentTarget.setPointerCapture(event.pointerId);
        setDragging(true);
        follow(event.currentTarget, event.clientX);
      }}
      onPointerMove={(event) => { if (dragging) follow(event.currentTarget, event.clientX); }}
      onPointerUp={() => setDragging(false)}
      onPointerCancel={() => setDragging(false)}
      onLostPointerCapture={() => setDragging(false)}
      onKeyDown={(event) => {
        const rtl = getComputedStyle(event.currentTarget).direction === "rtl";
        let next = index;
        if (event.key === (rtl ? "ArrowLeft" : "ArrowRight") || event.key === "ArrowUp") next = index + 1;
        else if (event.key === (rtl ? "ArrowRight" : "ArrowLeft") || event.key === "ArrowDown") next = index - 1;
        else if (event.key === "Home") next = 0;
        else if (event.key === "End") next = last;
        else return;
        event.preventDefault();
        commit(Math.min(last, Math.max(0, next)));
      }}
      className="group/effort relative h-7 w-full cursor-grab touch-none select-none overflow-hidden rounded-lg bg-foreground/6 outline-none focus-visible:ring-2 focus-visible:ring-ring aria-disabled:cursor-not-allowed aria-disabled:opacity-64 data-dragging:cursor-grabbing pointer-coarse:h-11 dark:bg-foreground/10">
      <div className={cn("absolute inset-y-0 start-0 w-(--effort-fill) overflow-hidden rounded-lg transition-all duration-500 group-data-dragging/effort:duration-0 motion-reduce:transition-none", spring)}>
        <EffortFill level={last > 0 ? index / last : 0} />
      </div>
      <div aria-hidden className={cn("pointer-events-none absolute inset-0 flex items-center justify-between px-4 transition-opacity duration-300", max && "opacity-0")}>
        {levels.map((item, position) => <span key={item.value} className="flex w-0 justify-center">
          <span className={cn("size-1 shrink-0 rounded-full transition-all duration-200", spring, position <= index ? "bg-background/70" : "bg-muted-foreground/45", popped === position && "scale-200")} />
        </span>)}
      </div>
      <div className={cn("pointer-events-none absolute inset-y-0.5 start-(--effort-thumb) w-7 rounded-md border border-foreground/10 bg-background shadow-sm transition-all duration-500 group-hover/effort:border-foreground/20 group-data-dragging/effort:scale-95 group-data-dragging/effort:shadow-md group-data-dragging/effort:duration-0 motion-reduce:transition-none dark:bg-foreground/90", spring)} />
    </div>
  </div>;
}

/** Effort heading, Faster/Smarter captions and the slider. The top level lights up. */
export function PromptInputEffortPanel({ levels, value, onValueChange, disabled = false, label = "Effort", description, sliderRef }: {
  levels: PromptEffort[];
  value: string;
  onValueChange: (value: string) => void;
  disabled?: boolean;
  /** The slider's accessible name. */
  label?: string;
  description?: string;
  sliderRef?: Ref<HTMLDivElement>;
}) {
  const { index, current, direction } = useEffortLevel(levels, value);
  return <div className="flex flex-col gap-2">
    <div className="flex items-center gap-1.5 text-sm">
      <span className="text-muted-foreground">Effort</span>
      <FlowText value={current?.label ?? ""} direction={direction}
        className={cn("font-medium transition-colors duration-300", index === levels.length - 1 && levels.length > 1 && "text-chart-4")} />
      {description && <Tooltip>
        <TooltipTrigger render={<Button type="button" size="icon-xs" variant="ghost" aria-label="About effort" className="ms-auto" />}><CircleHelpIcon /></TooltipTrigger>
        <TooltipPopup className="max-w-64">{description}</TooltipPopup>
      </Tooltip>}
    </div>
    <div className="-mb-1 flex justify-between text-xs text-muted-foreground"><span>Faster</span><span>Smarter</span></div>
    <EffortTrack levels={levels} index={index} disabled={disabled} label={label} sliderRef={sliderRef} onIndexChange={(next) => onValueChange(levels[next]!.value)} />
  </div>;
}
