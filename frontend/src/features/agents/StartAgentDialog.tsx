import { useState } from "react";
import { useAgents } from "@/stores/agents";
import { useUI } from "@/stores/ui";
import { Dialog, DialogPopup, DialogHeader, DialogTitle, DialogDescription, DialogPanel } from "@/components/ui/dialog";
import { AgentLaunchForm } from "./AgentLaunchForm";

export function StartAgentDialog() {
  const { setStartDialogOpen, upsert, setOpenId } = useAgents();
  const [busy, setBusy] = useState(false);
  return <Dialog open modal={false} onOpenChange={(open) => { if (!busy) setStartDialogOpen(open); }}>
    <DialogPopup className="max-w-2xl" showCloseButton={!busy}>
      <DialogHeader>
        <DialogTitle>New agent</DialogTitle>
        <DialogDescription>Choose a box, project and installed harness. Leave the task empty to start an idle session where supported.</DialogDescription>
      </DialogHeader>
      <DialogPanel className="p-0">
        <AgentLaunchForm requirePrompt={false} onBusyChange={setBusy} onStarted={(session) => {
          upsert(session);
          setOpenId(session.id);
          setStartDialogOpen(false);
          useUI.getState().setView("agents");
        }} />
      </DialogPanel>
    </DialogPopup>
  </Dialog>;
}
