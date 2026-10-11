/*
 * Adapted from assistant-ui elements/directive-text.tsx, commit 9201447608271c64a8a9ca66c69e81d27a1d9fab.
 * See docs/research/upstream-attribution.md.
 * MIT License — Copyright (c) 2026 AgentbaseAI Inc.
 */
"use client";

import type { FC, ReactNode } from "react";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

type IconComponent = FC<{ className?: string }>;

export type DirectiveTextSegment =
  | { readonly kind: "text"; readonly text: string }
  | {
      readonly kind: "mention";
      readonly type: string;
      readonly label: string;
      readonly id: string;
    };

export type DirectiveTextFormatter = {
  /** Parse text into alternating text and directive segments. */
  parse(text: string): readonly DirectiveTextSegment[];
};

export type CreateDirectiveTextOptions = {
  /** Maps a directive `type` to an icon component. */
  iconMap?: Record<string, IconComponent>;
  /** Icon rendered when `iconMap` has no entry for the segment type. */
  fallbackIcon?: IconComponent;
  /** Renders a chip's leading visual from the whole segment (a bot's avatar); wins over `iconMap`. */
  renderIcon?: (segment: Extract<DirectiveTextSegment, { kind: "mention" }>) => ReactNode;
  /** Extra classes for every chip, for a surface where the secondary badge would blend in. */
  chipClassName?: string;
};

/** Creates a text component that parses directive syntax and renders inline chips. */
export function createDirectiveText(
  formatter: DirectiveTextFormatter,
  options?: CreateDirectiveTextOptions,
): FC<{ text: string }> {
  const iconMap = options?.iconMap;
  const fallbackIcon = options?.fallbackIcon;

  const Component: FC<{ text: string }> = ({ text }) => {
    const segments = formatter.parse(text);

    if (segments.length === 1 && segments[0]!.kind === "text") {
      return <>{text}</>;
    }

    return (
      <>
        {segments.map((seg, i) => {
          if (seg.kind === "text") {
            return (
              <span key={i} className="whitespace-pre-wrap">
                {seg.text}
              </span>
            );
          }

          const Icon = iconMap?.[seg.type] ?? fallbackIcon;
          return (
            <Badge
              key={i}
              variant="secondary"
              data-slot="directive-text-chip"
              data-directive-type={seg.type}
              data-directive-id={seg.id}
              aria-label={`${seg.type}: ${seg.label}`}
              className={cn("aui-directive-chip items-baseline px-1.5 py-0.5 text-[13px] leading-none [&_svg]:self-center", options?.chipClassName)}
            >
              {options?.renderIcon?.(seg) ?? (Icon && <Icon />)}
              {seg.label}
            </Badge>
          );
        })}
      </>
    );
  };
  Component.displayName = "DirectiveText";
  return Component;
}
