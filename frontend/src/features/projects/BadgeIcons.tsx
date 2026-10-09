import type { Machine, MachineHealth } from "@/mygo";
import github from "@/assets/logos/github.svg?raw";
import apple from "@/assets/logos/apple.svg?raw";
import windows from "@/assets/logos/windows.svg?raw";
import linux from "@/assets/logos/linux.svg?raw";

export type OsFamily = "apple" | "windows" | "linux";

const OS_MARKS: Record<OsFamily, { svg: string; label: string }> = {
  apple: { svg: apple, label: "macOS" },
  windows: { svg: windows, label: "Windows" },
  linux: { svg: linux, label: "Linux" },
};

/** Maps a GOOS value or user-agent string to a logo family. "darwin" contains "win", so Apple is tested first. */
export function osFamily(os: string): OsFamily | null {
  if (/darwin|mac|apple/i.test(os)) return "apple";
  if (/windows|win32|\bwin\b/i.test(os)) return "windows";
  if (/linux|ubuntu|debian|fedora|alpine|x11/i.test(os)) return "linux";
  return null;
}

/** What the host runs: a live health report first, then the registered machine, then (for this system) the browser's own platform. */
export function hostFamily(id: string, machine: Machine | undefined, health: MachineHealth | null | undefined): OsFamily | null {
  return osFamily(health?.os ?? "") ?? osFamily(machine?.os ?? "") ?? (id === "local" ? osFamily(navigator.userAgent) : null);
}

/** Vetted bundled SVGs only; never render server-supplied markup here. */
function Mark({ svg, label }: { svg: string; label: string }) {
  return <span role="img" aria-label={label} className="inline-flex size-3 shrink-0 items-center justify-center [&>svg]:size-full" dangerouslySetInnerHTML={{ __html: svg }} />;
}

export const GithubMark = () => <Mark svg={github} label="GitHub" />;
export const OsMark = ({ family }: { family: OsFamily }) => <Mark svg={OS_MARKS[family].svg} label={OS_MARKS[family].label} />;
