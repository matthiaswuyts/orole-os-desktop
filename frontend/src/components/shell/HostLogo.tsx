import { siApple, siArm, siLinux } from "simple-icons";
import { cn } from "@/lib/utils";

/** Windows has no mark in simple-icons: four panes, the way the OS draws itself. */
const WINDOWS = "M3 5.4l7.6-1.05v7.25H3zM11.7 4.2L21 3v8.6h-9.3zM3 12.6h7.6v7.25L3 18.6zM11.7 12.6H21V21l-9.3-1.3z";

export type HostOS = "windows" | "macos" | "linux" | "unknown";

/** The OS a machine reports ("windows", "darwin", "linux/amd64", "Windows 11"...), reduced to the three that have a mark. */
export function hostOS(raw: string): HostOS {
  const os = raw.toLowerCase();
  if (os.includes("darwin") || os.includes("mac")) return "macos"; // before Windows: "darwin" contains "win"
  if (os.includes("windows") || os === "win32") return "windows";
  if (os.includes("linux") || os.includes("ubuntu") || os.includes("debian")) return "linux";
  return "unknown";
}

/** "x64" or "ARM64" from what a machine reports ("amd64", "x86_64", "arm64", "aarch64"); "" when it says nothing usable. */
export function hostArch(raw: string): { label: string; arm: boolean } | null {
  const arch = raw.toLowerCase();
  if (arch.includes("arm") || arch.includes("aarch")) return { label: "ARM64", arm: true };
  if (arch.includes("amd64") || arch.includes("x86_64") || arch.includes("x64")) return { label: "x64", arm: false };
  if (arch.includes("386") || arch.includes("x86")) return { label: "x86", arm: false };
  return null;
}

function Mark({ path, className }: { path: string; className?: string }) {
  return <svg viewBox="0 0 24 24" aria-hidden="true" fill="currentColor" className={cn("size-4 shrink-0", className)}><path d={path} /></svg>;
}

/** The mark of an operating system; nothing for one without a mark. Decorative: the OS is always named in text next to it. */
export function OsLogo({ os, className }: { os: string; className?: string }) {
  const kind = hostOS(os);
  if (kind === "unknown") return null;
  return <Mark path={kind === "windows" ? WINDOWS : kind === "macos" ? siApple.path : siLinux.path} className={className} />;
}

/** The CPU architecture as a small chip: the ARM mark for ARM, the plain label for x86. */
export function ArchChip({ architecture }: { architecture: string }) {
  const arch = hostArch(architecture);
  if (!arch) return null;
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">
      {arch.arm && <Mark path={siArm.path} className="size-3" />}
      {arch.label}
    </span>
  );
}
