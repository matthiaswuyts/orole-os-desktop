import { createElement } from "react";
import { File, Folder } from "lucide-react";
import { Search } from "@/mygo";
import { inDesktop } from "@/lib/backend";
import { registerProvider } from "@/lib/commands";
import type { SearchProvider } from "@/lib/commands";
import { openFile, useFiles } from "@/stores/files";
import { useUI } from "@/stores/ui";

/** Imported by the global view registry, not the lazy Files view. */
export const filesProvider = fileProvider("files", "Files", ["files:local", "files:git"]);
export const remoteFilesProvider = fileProvider("remote-files", "Remote files", ["files:remote"], true);

function fileProvider(id: string, label: string, providers: string[], explicitOnly = false): SearchProvider {
  return {
  id,
  label,
  explicitOnly,
  async search(query, signal, reportError) {
    if (!inDesktop || !query.trim() || signal.aborted) return [];
    const response = await Search.query(query, providers, 40);
    if (signal.aborted) return [];
    if (response.errors && Object.keys(response.errors).length) {
      const message = Object.entries(response.errors).map(([provider, error]) => `${provider}: ${error}`).join("; ");
      if (!response.results?.length || !reportError) throw new Error(message);
      reportError(message);
    }
    return (response.results ?? []).flatMap((result) => {
      const { provider, path } = result.action;
      if (!provider || !path) return [];
      const directory = result.action.isDir === "true";
      return [{
        id: `files.${provider}:${path}`,
        title: result.title,
        category: `${provider === "git" ? "Projects" : provider === "remote" ? "Remote" : "Local"}: ${result.subtitle ?? path}`,
        icon: createElement(directory ? Folder : File),
        run: () => {
          if (directory) { useFiles.getState().setLocation(provider, path); useUI.getState().setView("files"); }
          else return openFile(provider, path);
        },
      }];
    });
  },
  };
}
const unregister = [registerProvider(filesProvider), registerProvider(remoteFilesProvider)];
if (import.meta.hot) import.meta.hot.dispose(() => unregister.forEach((off) => off()));
