import { useEffect, useRef, useState } from "react";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, drawSelection, highlightActiveLine, highlightActiveLineGutter, keymap, lineNumbers } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { search, searchKeymap } from "@codemirror/search";
import { HighlightStyle, bracketMatching, indentOnInput, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";
import { languageFor } from "./languages";

export const editorTheme = EditorView.theme({
  "&": { height: "100%", backgroundColor: "var(--background)", color: "var(--foreground)", fontSize: "12px" },
  "&.cm-focused": { outline: "none" },
  ".cm-scroller": { overflow: "auto", fontFamily: "var(--font-mono)", lineHeight: "1.65" },
  ".cm-content": { caretColor: "var(--foreground)", padding: "12px 0" },
  ".cm-line": { padding: "0 12px" },
  ".cm-cursor": { borderLeftColor: "var(--foreground)" },
  ".cm-gutters": { backgroundColor: "var(--background)", color: "var(--muted-foreground)", borderRight: "1px solid var(--border)" },
  ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "var(--muted)" },
  "&.cm-focused .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection": { backgroundColor: "var(--accent)" },
  ".cm-panels": { backgroundColor: "var(--muted)", color: "var(--foreground)" },
  ".cm-textfield, .cm-button": { background: "var(--background)", color: "var(--foreground)", border: "1px solid var(--border)", borderRadius: "var(--radius-sm)" },
  ".cm-searchMatch": { backgroundColor: "var(--warning)", color: "var(--warning-foreground)" },
  ".cm-searchMatch-selected": { outline: "1px solid var(--ring)" },
  ".cm-tooltip": { backgroundColor: "var(--popover)", color: "var(--popover-foreground)", borderColor: "var(--border)" },
});
const highlight = HighlightStyle.define([
  { tag: [tags.keyword, tags.modifier], color: "var(--primary)", fontWeight: "500" },
  { tag: [tags.string, tags.regexp], color: "var(--success-foreground)" },
  { tag: [tags.number, tags.bool, tags.null], color: "var(--warning-foreground)" },
  { tag: [tags.typeName, tags.className], color: "var(--info-foreground)" },
  { tag: tags.comment, color: "var(--muted-foreground)", fontStyle: "italic" },
  { tag: [tags.function(tags.variableName), tags.tagName], color: "var(--info-foreground)" },
  { tag: tags.invalid, color: "var(--destructive-foreground)" },
]);

export function CodeEditor({ path, text, readOnly, onChange, onSave }: {
  path: string; text: string; readOnly: boolean; onChange: (text: string) => void; onSave: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  const handlers = useRef({ onChange, onSave });
  const initial = useRef({ text, readOnly, path });
  const readonly = useRef(new Compartment());
  const language = useRef(new Compartment());
  const accessibility = useRef(new Compartment());
  const [languageError, setLanguageError] = useState("");
  useEffect(() => { handlers.current = { onChange, onSave }; }, [onChange, onSave]);
  useEffect(() => {
    if (!host.current) return;
    const editor = new EditorView({ parent: host.current, state: EditorState.create({
      doc: initial.current.text,
      extensions: [editorTheme, syntaxHighlighting(highlight), lineNumbers(), highlightActiveLineGutter(), highlightActiveLine(), drawSelection(), history(), bracketMatching(), indentOnInput(), search(),
        keymap.of([{ key: "Mod-s", run: () => { handlers.current.onSave(); return true; } }, ...searchKeymap, ...defaultKeymap, ...historyKeymap, indentWithTab]),
        readonly.current.of([EditorState.readOnly.of(initial.current.readOnly), EditorView.editable.of(!initial.current.readOnly)]),
        language.current.of([]),
        accessibility.current.of(EditorView.contentAttributes.of({ "aria-label": `Editor for ${initial.current.path}` })),
        EditorView.updateListener.of((update) => { if (update.docChanged) handlers.current.onChange(update.state.doc.toString()); }),
      ],
    }) });
    view.current = editor;
    return () => { view.current = null; editor.destroy(); };
  }, []);
  useEffect(() => {
    const editor = view.current;
    if (editor && editor.state.doc.toString() !== text) editor.dispatch({ changes: { from: 0, to: editor.state.doc.length, insert: text } });
  }, [text]);
  useEffect(() => { view.current?.dispatch({ effects: readonly.current.reconfigure([EditorState.readOnly.of(readOnly), EditorView.editable.of(!readOnly)]) }); }, [readOnly]);
  useEffect(() => {
    let cancelled = false;
    setLanguageError("");
    view.current?.dispatch({ effects: accessibility.current.reconfigure(EditorView.contentAttributes.of({ "aria-label": `Editor for ${path}` })) });
    languageFor(path).then((extension) => { if (!cancelled) view.current?.dispatch({ effects: language.current.reconfigure(extension) }); }, () => { if (!cancelled) setLanguageError("Syntax highlighting could not load. Plain text editing is still available."); });
    return () => { cancelled = true; };
  }, [path]);
  return <div className="flex h-full min-h-0 flex-col">{languageError && <p role="status" className="px-3 py-1 text-xs text-muted-foreground">{languageError}</p>}<div ref={host} className="min-h-0 flex-1 overflow-hidden" /></div>;
}
