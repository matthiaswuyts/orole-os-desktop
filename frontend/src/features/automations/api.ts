import { Automations } from "@/mygo";

export interface Trigger { type: string; schedule: string; catchUp: string; event: string; filter: string }
export interface Step { id: string; name: string; type: string; params: Record<string, unknown>; when: string; timeoutSec: number; retries: number; backoffMs: number; onError: string; strategy: string }
export interface Definition { schemaVersion: number; id: string; name: string; description: string; enabled: boolean; trigger: Trigger; steps: Step[]; allowedExecutables: string[]; allowedHosts: string[]; allowHttp: boolean; concurrency: number; timeoutSec: number }
export interface Issue { path: string; message: string }
export interface SaveResult { definition: Definition; issues: Issue[]; saved: boolean }
export interface Summary { definition: Definition; lastStatus: string; lastRunAt: number }
export interface Run { id: string; automationId: string; name: string; status: string; triggerType: string; error: string; createdAt: number; startedAt: number; finishedAt: number; definition: Definition }
export interface Journal { runId: string; stepId: string; seq: number; type: string; status: string; attempt: number; input: unknown; output: unknown; error: string; startedAt: number; finishedAt: number }
export interface Detail { run: Run; steps: Journal[] }
export interface Approval { runId: string; stepId: string; name: string; message: string; expiresAt: number }
export interface Config { globalConcurrency: number; perAutomation: number; webhookEnabled: boolean; webhookPort: number; hasSecret: boolean }
export interface SchemaField { key: string; label: string; kind: string; required: boolean; options: string[]; hint: string }
export interface StepType { type: string; label: string; available: boolean; reason: string; sideEffect: boolean; keySupported: boolean; fields: SchemaField[] }
interface AutomationAPI {
  list(): Promise<Summary[]>;
  save(definition: Definition): Promise<SaveResult>;
  delete(id: string): Promise<void>;
  run(id: string): Promise<Run>;
  cancel(id: string): Promise<void>;
  runs(id: string): Promise<Run[]>;
  detail(id: string): Promise<Detail>;
  approvals(): Promise<Approval[]>;
  approve(runId: string, stepId: string, approved: boolean, note: string): Promise<void>;
  stepTypes(): Promise<StepType[]>;
  config(): Promise<Config>;
  setConfig(config: Config): Promise<Config>;
  generateSecret(): Promise<void>;
  preview(schedule: string): Promise<string[]>;
  templates(): Promise<Definition[]>;
}
// Generated Go names can collide with other services; this is the automation wire contract.
export const api = Automations as unknown as AutomationAPI;
export const errorText = (e: unknown) => e instanceof Error ? e.message : String(e);
export const newDefinition = (): Definition => ({ schemaVersion: 1, id: "", name: "", description: "", enabled: false, trigger: { type: "manual", schedule: "", catchUp: "skip", event: "", filter: "" }, steps: [], allowedExecutables: [], allowedHosts: [], allowHttp: false, concurrency: 1, timeoutSec: 300 });
