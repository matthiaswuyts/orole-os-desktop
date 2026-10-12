import { createContext, use } from "react";
import type { Step, StepType } from "./api";
import type { IssueMap, Names } from "./flow";

/** What every block card of the builder canvas reads: the tree, catalog, issues and actions. */
export interface FlowContextValue {
  tree: Step[];
  types: StepType[];
  issues: IssueMap;
  names: Names;
  selected: string | null;
  select: (id: string) => void;
  insert: (lane: string, index: number, type: string) => void;
  remove: (id: string) => void;
}
export const FlowContext = createContext<FlowContextValue | null>(null);
export function useFlow() {
  const value = use(FlowContext);
  if (!value) throw new Error("BlockStack needs a FlowContext");
  return value;
}

/** Drag data: a block of the tree, the end of a lane, or a new block from the library. */
export type DragData = { kind: "block"; lane: string } | { kind: "lane"; lane: string } | { kind: "new"; type: string };
