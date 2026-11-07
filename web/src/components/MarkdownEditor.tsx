import { useEffect, useRef } from "react";
import { EditorView, keymap, placeholder as cmPlaceholder } from "@codemirror/view";
import { EditorState } from "@codemirror/state";
import { basicSetup } from "codemirror";
import { markdown } from "@codemirror/lang-markdown";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags as t } from "@lezer/highlight";
import { api, ApiError } from "@/lib/api";

const theme = EditorView.theme(
  {
    "&": { height: "100%", backgroundColor: "transparent", color: "var(--color-fg)", fontSize: "14px" },
    ".cm-scroller": { fontFamily: "var(--font-mono)", lineHeight: "1.65" },
    ".cm-content": { padding: "16px 0", caretColor: "var(--color-accent)" },
    ".cm-line": { padding: "0 20px" },
    ".cm-gutters": { backgroundColor: "transparent", border: "none", color: "var(--color-faint)" },
    ".cm-activeLine, .cm-activeLineGutter": { backgroundColor: "rgba(255,255,255,0.025)" },
    "&.cm-focused": { outline: "none" },
    "&.cm-focused .cm-cursor": { borderLeftColor: "var(--color-accent)" },
    ".cm-selectionBackground, &.cm-focused .cm-selectionBackground, ::selection": { backgroundColor: "rgba(91,140,255,0.28) !important" },
    ".cm-placeholder": { color: "var(--color-faint)" },
    ".cm-foldGutter span": { color: "var(--color-faint)" },
    ".cm-tooltip": { backgroundColor: "var(--color-panel-2)", border: "1px solid var(--color-line-2)" },
  },
  { dark: true },
);

const highlight = HighlightStyle.define([
  { tag: t.heading1, fontSize: "1.35em", fontWeight: "700", color: "#e8ecf5" },
  { tag: t.heading2, fontSize: "1.18em", fontWeight: "650", color: "#e8ecf5" },
  { tag: [t.heading3, t.heading4, t.heading5, t.heading6], fontWeight: "600", color: "#e8ecf5" },
  { tag: t.strong, fontWeight: "700" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: [t.link, t.url], color: "#7aa2ff" },
  { tag: t.monospace, color: "#f5b843" },
  { tag: t.quote, color: "#8b94a8", fontStyle: "italic" },
  { tag: [t.processingInstruction, t.meta, t.contentSeparator], color: "#5d6579" },
  { tag: t.list, color: "#a78bfa" },
  { tag: t.comment, color: "#5d6579" },
]);

export interface MarkdownEditorHandle {
  focus: () => void;
}

/**
 * CodeMirror 6 markdown editor. Pasting or dropping files uploads them as
 * note attachments and inserts a markdown link/image at the cursor.
 */
export function MarkdownEditor({
  value,
  onChange,
  onBlur,
  onSave,
  noteId,
  placeholder,
}: {
  value: string;
  onChange: (v: string) => void;
  onBlur?: () => void;
  onSave?: () => void;
  noteId: number;
  placeholder?: string;
}) {
  const host = useRef<HTMLDivElement>(null);
  const view = useRef<EditorView | null>(null);
  // Keep callbacks fresh without recreating the editor.
  const cb = useRef({ onChange, onBlur, onSave, noteId });
  cb.current = { onChange, onBlur, onSave, noteId };

  useEffect(() => {
    if (!host.current) return;
    const upload = async (v: EditorView, files: File[], pos: number) => {
      for (const file of files) {
        const tag = `uploading-${Math.random().toString(36).slice(2, 8)}`;
        const placeholderText = `![${tag}](…)`;
        v.dispatch({ changes: { from: pos, insert: placeholderText + "\n" } });
        const fd = new FormData();
        fd.append("file", file);
        let replacement: string;
        try {
          const r = await api.post<{ url: string; filename: string; isImage: boolean }>(`/api/notes/${cb.current.noteId}/attachments`, fd);
          const label = r.filename.replace(/[[\]]/g, "");
          replacement = r.isImage ? `![${label}](${r.url})` : `[📎 ${label}](${r.url})`;
        } catch (e) {
          replacement = `<!-- upload failed: ${e instanceof ApiError ? e.message : "error"} -->`;
        }
        const doc = v.state.doc.toString();
        const at = doc.indexOf(placeholderText);
        if (at >= 0) v.dispatch({ changes: { from: at, to: at + placeholderText.length, insert: replacement } });
      }
    };

    const state = EditorState.create({
      doc: value,
      extensions: [
        basicSetup,
        markdown(),
        theme,
        syntaxHighlighting(highlight),
        EditorView.lineWrapping,
        cmPlaceholder(placeholder ?? ""),
        keymap.of([
          {
            key: "Mod-s",
            preventDefault: true,
            run: () => {
              cb.current.onSave?.();
              return true;
            },
          },
        ]),
        EditorView.updateListener.of((u) => {
          if (u.docChanged) cb.current.onChange(u.state.doc.toString());
          if (u.focusChanged && !u.view.hasFocus) cb.current.onBlur?.();
        }),
        EditorView.domEventHandlers({
          paste(e, v) {
            const files = [...(e.clipboardData?.files ?? [])];
            if (!files.length) return false;
            e.preventDefault();
            upload(v, files, v.state.selection.main.head);
            return true;
          },
          drop(e, v) {
            const files = [...(e.dataTransfer?.files ?? [])];
            if (!files.length) return false;
            e.preventDefault();
            const pos = v.posAtCoords({ x: e.clientX, y: e.clientY }) ?? v.state.selection.main.head;
            upload(v, files, pos);
            return true;
          },
        }),
      ],
    });
    view.current = new EditorView({ state, parent: host.current });
    return () => {
      view.current?.destroy();
      view.current = null;
    };
    // The editor is created once per note (the parent keys it by note id).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // External replacements (e.g. restoring a version) update the doc.
  useEffect(() => {
    const v = view.current;
    if (v && v.state.doc.toString() !== value) {
      v.dispatch({ changes: { from: 0, to: v.state.doc.length, insert: value } });
    }
  }, [value]);

  return <div ref={host} className="h-full min-h-0 overflow-hidden [&_.cm-editor]:h-full" />;
}
