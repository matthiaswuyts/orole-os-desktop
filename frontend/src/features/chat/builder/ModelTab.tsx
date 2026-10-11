import { type Dispatch } from "react";
import { Button } from "@/components/ui/button";
import { Select, SelectGroup, SelectItem, SelectPopup, SelectTrigger, SelectValue } from "@/components/ui/select";
import { useUI } from "@/stores/ui";
import { useLLM } from "@/stores/llm";
import type { Action, FormState } from "./botForm";
import { Block, ExecutiveCard } from "./controls";
import { ModelPicker } from "./ModelPicker";
import { useModelOptions, type Option } from "./modelOptions";

/** The Persona tab's model settings: model, reasoning level and whether the bot is an executive assistant. */
export function ModelTab({ form, dispatch }: { form: FormState; dispatch: Dispatch<Action> }) {
  const models = useModelOptions(form.model);
  const { connected, current, loading, failed, catalogs } = models;
  const patch = (p: Partial<FormState>) => dispatch({ type: "patch", patch: p });
  const levels = current.reasoning;

  const choose = (option: Option | null) => {
    if (!option) return;
    patch({ model: option.value, reasoning: option.reasoning.includes(form.reasoning) ? form.reasoning : "" });
  };

  return (
    <div className="flex flex-col gap-4">
      {connected.length === 0 ? (
        <div className="flex items-center justify-between gap-2 text-sm">
          <span className="text-muted-foreground">No provider connected.</span>
          <Button size="sm" variant="outline" onClick={() => useUI.getState().openSettings("ai")}>Connect</Button>
        </div>
      ) : (
        <Block label="Model">
          {(id, describedBy) => <ModelPicker id={id} describedBy={describedBy} models={models} onPick={choose} />}
        </Block>
      )}
      {current.kind === "missing" && <p role="status" className="text-warning-foreground text-xs">This model is no longer offered. Pick another.</p>}
      {loading && <p role="status" className="text-muted-foreground text-xs">Loading models…</p>}
      {failed.map((p) => (
        <p key={p.id} role="alert" className="flex items-center justify-between gap-2 text-destructive-foreground text-xs">
          <span className="selectable break-words">{p.name}: {catalogs[p.id]?.error}</span>
          <Button size="xs" variant="outline" onClick={() => void useLLM.getState().loadModels(p.id, true)}>Retry</Button>
        </p>
      ))}
      {levels.length > 0 && (
        <Block label="Reasoning">
          {(id) => (
            <Select value={form.reasoning} items={[{ value: "", label: "Default" }, ...levels.map((l) => ({ value: l, label: l }))]} onValueChange={(v) => { if (v !== null) patch({ reasoning: v }); }}>
              <SelectTrigger id={id} size="sm"><SelectValue /></SelectTrigger>
              <SelectPopup>
                <SelectGroup>
                  <SelectItem value="">Default</SelectItem>
                  {levels.map((l) => <SelectItem key={l} value={l}>{l}</SelectItem>)}
                </SelectGroup>
              </SelectPopup>
            </Select>
          )}
        </Block>
      )}
      <ExecutiveCard checked={form.executive} onChange={(executive) => patch({ executive })} />
    </div>
  );
}
