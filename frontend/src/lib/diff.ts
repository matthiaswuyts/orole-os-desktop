import type { DiffResult } from "@/mygo";

/** Line marker in a unified diff: context, addition or deletion. */
export type DiffLineKind = " " | "+" | "-";
export type DiffStatus = "added" | "deleted" | "modified" | "renamed" | "copied";

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
  /** 1-based line number in the old file, 0 when the line does not exist there. */
  old: number;
  /** 1-based line number in the new file, 0 when the line does not exist there. */
  new: number;
}

export interface DiffHunk {
  header: string;
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  lines: DiffLine[];
}

/** One changed file. This is the model `DiffView` renders; any slice can build it. */
export interface DiffFile {
  path: string;
  oldPath?: string;
  status: DiffStatus;
  binary: boolean;
  additions: number;
  deletions: number;
  hunks: DiffHunk[];
}

const STATUSES: readonly string[] = ["added", "deleted", "modified", "renamed", "copied"];

/** Converts the daemon's parsed diff (git.diff) into the view model. */
export function toDiffFiles(r: Pick<DiffResult, "files">): DiffFile[] {
  return r.files.map((f) => ({
    path: f.path,
    oldPath: f.oldPath,
    status: (STATUSES.includes(f.status) ? f.status : "modified") as DiffStatus,
    binary: f.binary,
    additions: f.additions,
    deletions: f.deletions,
    hunks: f.hunks.map((h) => ({
      ...h,
      lines: h.lines.map((l) => ({ ...l, kind: (l.kind === "+" || l.kind === "-" ? l.kind : " ") as DiffLineKind })),
    })),
  }));
}

/** One virtualized row of the unified view. */
export type DiffRow =
  | { t: "file"; file: number }
  | { t: "note"; file: number; text: string }
  | { t: "hunk"; file: number; header: string }
  | { t: "line"; file: number; line: DiffLine };

/** Flattens files into rows so a single virtualized list can render any size of diff. */
export function flattenDiff(files: DiffFile[]): DiffRow[] {
  const rows: DiffRow[] = [];
  files.forEach((f, file) => {
    rows.push({ t: "file", file });
    if (f.binary) rows.push({ t: "note", file, text: "Binary file not shown" });
    else if (f.hunks.length === 0) rows.push({ t: "note", file, text: f.status === "renamed" ? `Renamed from ${f.oldPath ?? "?"} without changes` : "No content changes" });
    for (const h of f.hunks) {
      rows.push({ t: "hunk", file, header: h.header });
      for (const line of h.lines) rows.push({ t: "line", file, line });
    }
  });
  return rows;
}

/** Index of the first row of each file within `flattenDiff` output. */
export function fileStartRows(rows: DiffRow[]): number[] {
  const starts: number[] = [];
  rows.forEach((r, i) => { if (r.t === "file") starts[r.file] = i; });
  return starts;
}
