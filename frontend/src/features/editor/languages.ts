import type { Extension } from "@codemirror/state";

// The requested editor loads only the active file's language; static imports would eagerly bundle every grammar.
export async function languageFor(path: string): Promise<Extension> {
  const ext = path.split(".").at(-1)?.toLowerCase();
  switch (ext) {
    case "js": case "jsx": case "mjs": case "cjs": case "ts": case "tsx": case "mts": case "cts":
      return (await import("@codemirror/lang-javascript")).javascript({ typescript: ["ts", "tsx", "mts", "cts"].includes(ext), jsx: ext === "jsx" || ext === "tsx" });
    case "json": case "jsonc": return (await import("@codemirror/lang-json")).json();
    case "md": case "markdown": return (await import("@codemirror/lang-markdown")).markdown();
    case "go": return (await import("@codemirror/lang-go")).go();
    case "py": case "pyw": return (await import("@codemirror/lang-python")).python();
    case "css": return (await import("@codemirror/lang-css")).css();
    case "html": case "htm": return (await import("@codemirror/lang-html")).html();
    case "rs": return (await import("@codemirror/lang-rust")).rust();
    case "yaml": case "yml": return (await import("@codemirror/lang-yaml")).yaml();
    default: return [];
  }
}
