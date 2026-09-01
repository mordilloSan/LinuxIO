import {
  StreamLanguage,
  syntaxHighlighting,
  type StreamParser,
} from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { classHighlighter } from "@lezer/highlight";
import CodeMirror, { type Extension } from "@uiw/react-codemirror";
import {
  useEffect,
  useEffectEvent,
  useImperativeHandle,
  useState,
  type Ref,
} from "react";

import "./file-editor.css";

interface FileEditorProps {
  // Used as the editor container's DOM id, so it must be unique when two
  // editors are mounted at once (compose + env panes).
  editorName?: string;
  // When two editors share a dialog, only the primary one may own the
  // document-level Ctrl+S listener — both would fire on one keydown.
  enableSaveShortcut?: boolean;
  fileName: string;
  filePath: string;
  initialContent: string;
  initialVersion?: string;
  isSaving?: boolean;
  onDirtyChange?: (isDirty: boolean) => void;
  onSave: (
    content: string,
    expectedVersion?: string,
  ) => Promise<boolean | void>;
  readOnly?: boolean;
  ref?: Ref<FileEditorHandle>;
}

export interface FileEditorHandle {
  getContent: () => string;
  isDirty: () => boolean;
  reset: (content: string, version?: string) => void;
  save: () => Promise<boolean>;
}

type LanguageName =
  | "c"
  | "cpp"
  | "css"
  | "diff"
  | "dockerfile"
  | "go"
  | "html"
  | "ini"
  | "java"
  | "javascript"
  | "json"
  | "lua"
  | "perl"
  | "python"
  | "ruby"
  | "rust"
  | "scss"
  | "shell"
  | "sql"
  | "text"
  | "toml"
  | "typescript"
  | "xml"
  | "yaml";

const languageByExtension: Record<string, LanguageName> = {
  bash: "shell",
  c: "c",
  cc: "cpp",
  cfg: "ini",
  cjs: "javascript",
  conf: "ini",
  cpp: "cpp",
  css: "css",
  cts: "typescript",
  cxx: "cpp",
  desktop: "ini",
  diff: "diff",
  dockerfile: "dockerfile",
  env: "shell",
  fish: "shell",
  go: "go",
  h: "c",
  hh: "cpp",
  hpp: "cpp",
  htm: "html",
  html: "html",
  ini: "ini",
  java: "java",
  js: "javascript",
  json: "json",
  link: "ini",
  lua: "lua",
  mjs: "javascript",
  mount: "ini",
  mts: "typescript",
  netdev: "ini",
  network: "ini",
  patch: "diff",
  path: "ini",
  pl: "perl",
  plist: "xml",
  pm: "perl",
  properties: "ini",
  py: "python",
  rb: "ruby",
  rs: "rust",
  scss: "scss",
  service: "ini",
  sh: "shell",
  slice: "ini",
  socket: "ini",
  sql: "sql",
  svg: "xml",
  target: "ini",
  timer: "ini",
  toml: "toml",
  ts: "typescript",
  xml: "xml",
  xsl: "xml",
  yaml: "yaml",
  yml: "yaml",
  zsh: "shell",
};

export const getEditorLanguageName = (fileName: string): LanguageName => {
  const extension = fileName.split(".").pop()?.toLowerCase() ?? "";
  return languageByExtension[extension] ?? "text";
};

function legacy<M>(
  load: () => Promise<M>,
  pick: (mod: M) => StreamParser<unknown>,
) {
  return () => load().then((mod) => StreamLanguage.define(pick(mod)));
}

const clike = () => import("@codemirror/legacy-modes/mode/clike");
const cssModes = () => import("@codemirror/legacy-modes/mode/css");
const jsModes = () => import("@codemirror/legacy-modes/mode/javascript");
const xmlModes = () => import("@codemirror/legacy-modes/mode/xml");

const languageLoaders: Partial<Record<LanguageName, () => Promise<Extension>>> =
  {
    c: legacy(clike, (m) => m.c),
    cpp: legacy(clike, (m) => m.cpp),
    css: legacy(cssModes, (m) => m.css),
    diff: legacy(
      () => import("@codemirror/legacy-modes/mode/diff"),
      (m) => m.diff,
    ),
    dockerfile: legacy(
      () => import("@codemirror/legacy-modes/mode/dockerfile"),
      (m) => m.dockerFile,
    ),
    go: legacy(
      () => import("@codemirror/legacy-modes/mode/go"),
      (m) => m.go,
    ),
    html: legacy(xmlModes, (m) => m.html),
    ini: legacy(
      () => import("@codemirror/legacy-modes/mode/properties"),
      (m) => m.properties,
    ),
    java: legacy(clike, (m) => m.java),
    javascript: legacy(jsModes, (m) => m.javascript),
    json: () => import("@codemirror/lang-json").then(({ json }) => json()),
    lua: legacy(
      () => import("@codemirror/legacy-modes/mode/lua"),
      (m) => m.lua,
    ),
    perl: legacy(
      () => import("@codemirror/legacy-modes/mode/perl"),
      (m) => m.perl,
    ),
    python: legacy(
      () => import("@codemirror/legacy-modes/mode/python"),
      (m) => m.python,
    ),
    ruby: legacy(
      () => import("@codemirror/legacy-modes/mode/ruby"),
      (m) => m.ruby,
    ),
    rust: legacy(
      () => import("@codemirror/legacy-modes/mode/rust"),
      (m) => m.rust,
    ),
    scss: legacy(cssModes, (m) => m.sCSS),
    shell: legacy(
      () => import("@codemirror/legacy-modes/mode/shell"),
      (m) => m.shell,
    ),
    sql: legacy(
      () => import("@codemirror/legacy-modes/mode/sql"),
      (m) => m.standardSQL,
    ),
    toml: legacy(
      () => import("@codemirror/legacy-modes/mode/toml"),
      (m) => m.toml,
    ),
    typescript: legacy(jsModes, (m) => m.typescript),
    xml: legacy(xmlModes, (m) => m.xml),
    yaml: () => import("@codemirror/lang-yaml").then(({ yaml }) => yaml()),
  };

// Selection colour must beat CodeMirror's base theme. A theme extension is
// guaranteed to; a stylesheet rule only wins on selector specificity.
const editorChrome = EditorView.theme({
  ".cm-selectionBackground": {
    background: "var(--app-palette-action-selected)",
  },
  "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground": {
    background: "var(--app-palette-action-selected)",
  },
});

const baseEditorExtensions: Extension[] = [
  EditorView.contentAttributes.of({ "aria-label": "Code editor" }),
  syntaxHighlighting(classHighlighter),
  editorChrome,
];

const editorSetup = { tabSize: 2 };

interface EditorState {
  baseContent: string;
  baseVersion?: string;
  content: string;
  filePath: string;
  isDirty: boolean;
}

const createEditorState = (
  filePath: string,
  baseContent: string,
  baseVersion?: string,
): EditorState => ({
  filePath,
  baseContent,
  baseVersion,
  content: baseContent,
  isDirty: false,
});

const stateForSource = (
  state: EditorState,
  filePath: string,
  initialContent: string,
  initialVersion?: string,
): EditorState => {
  if (state.filePath !== filePath) {
    return createEditorState(filePath, initialContent, initialVersion);
  }
  // Background refetches may update the source while the user is editing.
  // Preserve that draft; a clean editor can safely adopt the new source.
  if (
    !state.isDirty &&
    (state.baseContent !== initialContent ||
      state.baseVersion !== initialVersion)
  ) {
    return createEditorState(filePath, initialContent, initialVersion);
  }
  return state;
};

interface LoadedLanguage {
  extensions: Extension[];
  name: LanguageName;
}

const FileEditor = ({
  editorName = "file-editor",
  enableSaveShortcut = true,
  filePath,
  fileName,
  initialContent,
  initialVersion,
  onSave,
  isSaving = false,
  readOnly = false,
  onDirtyChange,
  ref,
}: FileEditorProps) => {
  const [editorState, setEditorState] = useState<EditorState>(() =>
    createEditorState(filePath, initialContent, initialVersion),
  );
  const normalizedState = stateForSource(
    editorState,
    filePath,
    initialContent,
    initialVersion,
  );
  const { baseVersion, content, isDirty } = normalizedState;
  const language = getEditorLanguageName(fileName);
  const [loadedLanguage, setLoadedLanguage] = useState<LoadedLanguage>({
    extensions: baseEditorExtensions,
    name: "text",
  });
  const extensions =
    loadedLanguage.name === language
      ? loadedLanguage.extensions
      : baseEditorExtensions;

  const updateEditorState = (updater: (state: EditorState) => EditorState) => {
    setEditorState((previous) => {
      const current = stateForSource(
        previous,
        filePath,
        initialContent,
        initialVersion,
      );
      return updater(current);
    });
  };

  const handleSave = async () => {
    if (readOnly) return false;
    try {
      const saved = await onSave(content, baseVersion);
      if (saved === false) return false;

      updateEditorState((state) => ({
        ...state,
        baseContent: state.content,
        isDirty: false,
      }));
      if (isDirty) {
        onDirtyChange?.(false);
      }
      return true;
    } catch {
      // Error is handled by parent component
      return false;
    }
  };

  const handleKeyDown = useEffectEvent((event: KeyboardEvent) => {
    if ((event.ctrlKey || event.metaKey) && event.key === "s") {
      event.preventDefault();
      if (!isSaving && !readOnly) {
        void handleSave();
      }
    }
  });

  useEffect(() => {
    if (!enableSaveShortcut) return;
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [enableSaveShortcut]);

  useEffect(() => {
    const loadLanguage = languageLoaders[language];
    if (!loadLanguage) return;

    let cancelled = false;
    void loadLanguage()
      .then((languageExtension) => {
        if (!cancelled) {
          setLoadedLanguage({
            extensions: [...baseEditorExtensions, languageExtension],
            name: language,
          });
        }
      })
      .catch((error: unknown) => {
        console.error(
          `Failed to load CodeMirror language "${language}":`,
          error,
        );
      });

    return () => {
      cancelled = true;
    };
  }, [language]);

  const handleContentChange = (newValue: string) => {
    const nextDirty = newValue !== normalizedState.baseContent;
    if (nextDirty !== isDirty) {
      onDirtyChange?.(nextDirty);
    }
    updateEditorState((state) => ({
      ...state,
      content: newValue,
      isDirty: nextDirty,
    }));
  };

  useImperativeHandle(ref, () => ({
    reset: (nextContent: string, nextVersion?: string) => {
      setEditorState(createEditorState(filePath, nextContent, nextVersion));
      onDirtyChange?.(false);
    },
    save: handleSave,
    getContent: () => content,
    isDirty: () => isDirty,
  }));

  return (
    <CodeMirror
      basicSetup={editorSetup}
      className="file-editor"
      extensions={extensions}
      height="100%"
      id={editorName}
      indentWithTab={false}
      onChange={handleContentChange}
      readOnly={isSaving || readOnly}
      theme="none"
      value={content}
      width="100%"
    />
  );
};

export default FileEditor;
