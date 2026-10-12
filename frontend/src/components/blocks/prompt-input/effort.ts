/*
 * Effort level state for the Prompt Input block, adapted from HextaUI (https://hextaui.com/blocks/prompt-input).
 * See docs/research/upstream-attribution.md.
 */
import { useState } from "react";

export interface PromptEffort { value: string; label: string }

/** The selected level and which way it last moved, for FlowText. */
export function useEffortLevel(levels: PromptEffort[], value: string) {
  const index = Math.max(0, levels.findIndex((level) => level.value === value));
  const [last, setLast] = useState<{ index: number; direction: 1 | -1 }>({ index, direction: 1 });
  if (last.index !== index) setLast({ index, direction: index > last.index ? 1 : -1 });
  const direction = last.index === index ? last.direction : index > last.index ? 1 : -1;
  return { index, current: levels[index], direction };
}

