import { Automations, type Definition } from "@/mygo";

export type {
  Approval, AutomationsIssue as Issue, AutomationsRun as Run, Config, Definition, Detail, Field as SchemaField, Journal,
  SaveResult, Step, StepType, Summary, Topic, TopicField, Trigger, Watch, WatchStatus,
} from "@/mygo";

/** The daemon-owned automation engine (typed bindings from mygo.ts). */
export const api = Automations;
export const errorText = (e: unknown) => e instanceof Error ? e.message : String(e);
export const newDefinition = (): Definition => ({
  schemaVersion: 2, id: "", name: "Untitled flow", description: "", enabled: false,
  trigger: { type: "manual", schedule: "", catchUp: "skip", event: "", filter: "" },
  steps: [], allowedExecutables: [], allowedHosts: [], allowHttp: false, concurrency: 1, timeoutSec: 3600,
});
