import { useCallback, useEffect, useState } from "react";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { useUI } from "@/stores/ui";
import { NeedsDesktop } from "@/features/shared/States";
import { api, errorText, newDefinition, type Definition, type Issue, type StepType, type Summary, type Topic } from "./api";
import { FlowBuilder } from "./FlowBuilder";
import { FlowList } from "./FlowList";
import { useAutomationResource } from "./useAutomationResource";

interface Catalog { types: StepType[]; templates: Definition[]; topics: Topic[] }
interface Open { key: number; definition: Definition; issues: Issue[]; run: string | null }

export function AutomationsView() {
  const offline = useDaemon((s) => s.link === "offline");
  const focus = useUI((s) => s.automationFocus);
  const load = useCallback(async () => {
    const [items, approvals] = await Promise.all([api.list(), api.approvals()]);
    return { items: items ?? [], approvals: approvals ?? [] };
  }, []);
  const resource = useAutomationResource(load);
  const [catalog, setCatalog] = useState<Catalog | null>(null);
  const [catalogError, setCatalogError] = useState<string | null>(null);
  const [catalogRevision, setCatalogRevision] = useState(0);
  const [open, setOpen] = useState<Open | null>(null);

  useEffect(() => {
    if (!inDesktop || offline) return;
    let active = true;
    Promise.all([api.stepTypes(), api.templates(), api.events()]).then(([types, templates, topics]) => {
      if (active) { setCatalog({ types: types ?? [], templates: templates ?? [], topics: topics ?? [] }); setCatalogError(null); }
    }, (e) => { if (active) setCatalogError(errorText(e)); });
    return () => { active = false; };
  }, [offline, catalogRevision]);

  const openSummary = useCallback((summary: Summary, run: string | null = null) => {
    setOpen({ key: Date.now(), definition: structuredClone(summary.definition), issues: summary.issues ?? [], run });
  }, []);
  const create = (definition: Definition) => setOpen({ key: Date.now(), definition, issues: [], run: null });

  // A flow handed over by the palette or sidebar ("new" starts a blank one) opens once its data is here.
  // An open builder may hold unsaved work, so the handoff waits until the user goes back to the list.
  const items = resource.data?.items;
  const editing = open !== null;
  useEffect(() => {
    if (!focus || editing) return;
    if (focus === "new") {
      if (!catalog) return;
      setOpen({ key: Date.now(), definition: newDefinition(), issues: [], run: null });
    } else {
      if (!items) return;
      const summary = items.find((s) => s.definition.id === focus);
      if (summary) openSummary(summary);
    }
    useUI.getState().focusAutomation(null);
  }, [focus, editing, catalog, items, openSummary]);

  if (!inDesktop) return <NeedsDesktop what="Automations" />;
  if (open) return <FlowBuilder key={open.key} initial={open.definition} initialIssues={open.issues} initialRun={open.run} types={catalog?.types ?? []} topics={catalog?.topics ?? []} offline={offline}
    onBack={() => { setOpen(null); void resource.refresh(); }} onSaved={() => void resource.refresh()} />;
  return <FlowList items={resource.data?.items ?? null} approvals={resource.data?.approvals ?? []} loading={resource.loading} error={resource.error} offline={offline}
    topics={catalog?.topics ?? []} templates={catalog?.templates ?? []} catalog={{ ready: !!catalog, error: catalogError, retry: () => setCatalogRevision((n) => n + 1) }}
    onRefresh={() => void resource.refresh()} onOpen={openSummary} onCreate={create} />;
}
