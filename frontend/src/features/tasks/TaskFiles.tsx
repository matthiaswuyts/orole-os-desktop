import { useEffect, useState } from "react";
import { Download, FileText, RefreshCw } from "lucide-react";
import { Agents, type TaskFilePreview } from "@/mygo";
import { Button } from "@/components/ui/button";
import { SyntaxHighlighter } from "@/components/SyntaxHighlighter";
import { errText } from "@/features/agents/format";

export const RASTER_MIMES: Record<string, true | undefined> = { "image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true };

type FileState = { key: string; file?: TaskFilePreview; error?: string };
export function useTaskFile(sessionId: string, path: string) {
  const [version, setVersion] = useState(0);
  const [result, setResult] = useState<FileState | null>(null);
  const key = `${sessionId}\0${path}\0${version}`;
  useEffect(() => {
    let active = true;
    void Agents.taskFile(sessionId, path).then(
      (file) => { if (active) setResult({ key, file }); },
      (error: unknown) => { if (active) setResult({ key, error: errText(error) }); },
    );
    return () => { active = false; };
  }, [key, sessionId, path]);
  return { file: result?.key === key ? result.file : undefined, error: result?.key === key ? result.error : undefined, retry: () => setVersion((v) => v + 1) };
}

export function TaskFilePreviewContent({ sessionId, path }: { sessionId: string; path: string }) {
  const { file, error, retry } = useTaskFile(sessionId, path);
  const [decodeError, setDecodeError] = useState<string | null>(null);
  if (error) return <div className="space-y-2 p-3"><p role="alert" className="selectable break-words text-sm text-destructive-foreground">Could not load file: {error}</p><Button variant="outline" size="sm" onClick={retry}><RefreshCw aria-hidden />Retry file</Button></div>;
  if (!file) return <p role="status" className="p-3 text-sm text-muted-foreground">Loading file…</p>;
  const raster = RASTER_MIMES[file.mime] === true && file.data !== undefined;
  const canDownload = !file.truncated && (file.data !== undefined || file.text !== undefined);
  return <div className="min-w-0 space-y-3 p-3">
    <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground"><FileText aria-hidden className="size-4" /><span className="min-w-0 break-all">{file.path}</span><span>{file.size.toLocaleString()} bytes · {file.mime || "Unknown type"}</span>
      {canDownload && <Button size="xs" variant="outline" onClick={() => {
        try {
          const content = file.data !== undefined ? Uint8Array.from(atob(file.data), (c) => c.charCodeAt(0)) : file.text!;
          const url = URL.createObjectURL(new Blob([content], { type: "application/octet-stream" }));
          const link = document.createElement("a"); link.href = url; link.download = file.name; link.click();
          setTimeout(() => URL.revokeObjectURL(url), 0);
          setDecodeError(null);
        } catch (e) { setDecodeError(errText(e)); }
      }}><Download aria-hidden />Download</Button>}
    </div>
    {decodeError && <p role="alert" className="text-xs text-destructive-foreground">Could not display or download file: {decodeError}</p>}
    {file.truncated && <p role="status" className="text-xs text-warning-foreground">Preview truncated. Download is unavailable because the full file was not returned.</p>}
    {raster && !decodeError ? <img src={`data:${file.mime};base64,${file.data}`} alt={file.name} className="mx-auto max-h-[65vh] max-w-full object-contain" onError={() => setDecodeError("The image could not be decoded.")} />
      : file.text !== undefined ? <SyntaxHighlighter code={file.text} language={file.name.split(".").at(-1) ?? ""} />
      : !raster && <p className="text-sm text-muted-foreground">No inline preview is available for this file type.</p>}
  </div>;
}
