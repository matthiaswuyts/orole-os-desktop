import { useEffect, useState } from "react";
import { Agents, type LaunchOptions } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { useDaemon } from "@/stores/daemon";
import { errText } from "./format";

/** Machine-scoped discovery; late replies never replace another box's choices. */
export function useHarnessOptions(machine: string) {
  const link = useDaemon((state) => state.link);
  const [result, setResult] = useState<{ machine: string; link: typeof link; options?: LaunchOptions; error?: string } | null>(null);
  useEffect(() => {
    if (!inDesktop) return;
    let active = true;
    setResult(null);
    void Agents.options(machine).then(
      (options) => { if (active) setResult({ machine, link, options }); },
      (error: unknown) => { if (active) setResult({ machine, link, error: errText(error) }); },
    );
    return () => { active = false; };
  }, [machine, link]);
  const current = result?.machine === machine && result.link === link ? result : null;
  return { options: current?.options, error: current?.error, loading: inDesktop && current === null, offline: link === "offline" };
}
