/*
 * Adapted from assistant-ui elements/comparison-card.tsx, commit 9201447608271c64a8a9ca66c69e81d27a1d9fab.
 * See docs/research/upstream-attribution.md.
 * MIT License — Copyright (c) 2026 AgentbaseAI Inc.
 */
"use client";

import type { ComponentProps } from "react";
import { CheckIcon, MinusIcon } from "lucide-react";
import { cn } from "@/lib/utils";
import { bubble, mono } from "./surfaces";

export interface ComparisonOption {
  id: string;
  name: string;
  headline: string;
  traits: readonly (string | false)[];
}

export function ComparisonCard({
  traitLabels,
  options,
  recommendedId,
  reason,
  className,
  ...props
}: Omit<
  ComponentProps<"div">,
  "children" | "traitLabels" | "options" | "recommendedId" | "reason"
> & {
  traitLabels: readonly string[];
  options: readonly ComparisonOption[];
  recommendedId: string;
  reason: string;
}) {
  return (
    <div
      data-slot="comparison-card"
      className={cn(
        bubble,
        "flex w-full max-w-md flex-col gap-3 p-4",
        className,
      )}

      {...props}
    >
      <div className="flex gap-2">
        {options.map((option) => {
          const recommended = option.id === recommendedId;
          return (
            <div
              key={option.id}
              className={cn(
                "flex min-w-0 flex-1 flex-col gap-2 rounded-xl p-3 transition-colors",
                recommended ? "bg-blue-500/[0.07] dark:bg-blue-400/10" : "border bg-muted/40",
              )}
            >
              <div className="flex flex-col gap-0.5">
                <span className="flex items-baseline gap-1.5">
                  <span className="truncate text-[13px] font-medium">
                    {option.name}
                  </span>
                  {recommended && (
                    <span
                      className={cn(
                        mono,
                        "shrink-0 text-blue-600 dark:text-blue-400",
                      )}
                    >
                      pick
                    </span>
                  )}
                </span>
                <span className="text-muted-foreground text-xs break-words">
                  {option.headline}
                </span>
              </div>

              <div className="flex flex-col gap-1">
                {traitLabels.map((label, i) => {
                  const trait = option.traits[i];
                  const absent = !trait;
                  return (
                    <span
                      key={`${i}-${label}`}
                      className="flex items-start gap-1.5 text-xs"
                    >
                      {absent ? (
                        <MinusIcon className="text-muted-foreground mt-0.5 size-3 shrink-0" />
                      ) : (
                        <CheckIcon className="mt-0.5 size-3 shrink-0 text-emerald-500" />
                      )}
                      <span
                        className={cn(
                          "min-w-0 break-words",
                          absent
                            ? "text-muted-foreground"
                            : "text-foreground/65",
                        )}
                      >
                        {absent ? label : trait}
                      </span>
                    </span>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>

      <p className="text-muted-foreground text-xs leading-relaxed">{reason}</p>
    </div>
  );
}
