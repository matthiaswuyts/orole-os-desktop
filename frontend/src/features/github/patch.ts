import type { PullFile } from "@/mygo";
import type { DiffFile, DiffHunk, DiffLine, DiffStatus } from "@/lib/diff";

const HUNK = /^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(.*)$/;

/** Parses the `patch` text GitHub returns for one file (hunks only, no file header). */
export function parsePatch(patch: string): DiffHunk[] {
  const hunks: DiffHunk[] = [];
  let cur: DiffHunk | null = null;
  let o = 0, n = 0;
  for (const raw of patch.split("\n")) {
    const m = HUNK.exec(raw);
    if (m) {
      cur = { header: raw, oldStart: +m[1]!, oldLines: m[2] === undefined ? 1 : +m[2], newStart: +m[3]!, newLines: m[4] === undefined ? 1 : +m[4], lines: [] };
      hunks.push(cur);
      o = cur.oldStart; n = cur.newStart;
      continue;
    }
    if (!cur || raw.startsWith("\\")) continue; // "\ No newline at end of file"
    const kind = raw[0];
    const text = raw.slice(1);
    let line: DiffLine;
    if (kind === "+") line = { kind: "+", text, old: 0, new: n++ };
    else if (kind === "-") line = { kind: "-", text, old: o++, new: 0 };
    else line = { kind: " ", text, old: o++, new: n++ };
    cur.lines.push(line);
  }
  return hunks;
}

const STATUS: Record<string, DiffStatus> = { added: "added", removed: "deleted", modified: "modified", renamed: "renamed", copied: "copied", changed: "modified" };

/** Converts pull request files to DiffView's model. Files without a patch (binary / too large) are marked binary. */
export function pullFilesToDiff(files: PullFile[]): DiffFile[] {
  return files.map((f) => ({
    path: f.filename,
    oldPath: f.previous_filename || undefined,
    status: STATUS[f.status] ?? "modified",
    binary: !f.patch && f.changes > 0,
    additions: f.additions,
    deletions: f.deletions,
    hunks: f.patch ? parsePatch(f.patch) : [],
  }));
}
