import {
  useCallback,
  useEffect,
  useEffectEvent,
  useImperativeHandle,
  useRef,
  useState,
  type Ref,
} from "react";
import ReactAce from "react-ace";

import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useAppTheme } from "@/theme";

// react-ace CJS exports the component as .default — Rolldown may not unwrap it
const AceEditor =
  typeof (ReactAce as any).default === "function"
    ? (ReactAce as any).default
    : ReactAce;

interface FileEditorProps {
  // Ace uses this as the editor container's DOM id, so it must be unique when
  // two editors are mounted at once (compose + env panes).
  editorName?: string;
  // When two editors share a dialog, only the primary one may own the
  // document-level Ctrl+S listener — both would fire on one keydown.
  enableSaveShortcut?: boolean;
  fileName: string;
  filePath: string;
  initialContent: string;
  isSaving?: boolean;
  onDirtyChange?: (isDirty: boolean) => void;
  onSave: (content: string) => Promise<boolean | void>;
  readOnly?: boolean;
  // Taken as a plain prop rather than through forwardRef: useEffectEvent does
  // not track props inside a forwardRef render function, which silently froze
  // the Ctrl+S handler on the content this editor mounted with.
  ref?: Ref<FileEditorHandle>;
}

export interface FileEditorHandle {
  getContent: () => string;
  isDirty: () => boolean;
  save: () => Promise<boolean>;
}

const getLanguageMode = (fileName: string): string => {
  const ext = fileName.split(".").pop()?.toLowerCase() || "";
  const modeMap: Record<string, string> = {
    js: "javascript",
    ts: "javascript",
    tsx: "javascript",
    jsx: "javascript",
    py: "python",
    java: "java",
    c: "c_cpp",
    cpp: "c_cpp",
    h: "c_cpp",
    hpp: "c_cpp",
    html: "html",
    htm: "html",
    css: "css",
    sql: "sql",
    json: "json",
    xml: "xml",
    yml: "yaml",
    yaml: "yaml",
    sh: "sh",
    env: "sh",
  };
  return modeMap[ext] || "text";
};

const aceModeLoaders: Record<string, () => Promise<unknown>> = {
  javascript: () => import("ace-builds/src-noconflict/mode-javascript"),
  python: () => import("ace-builds/src-noconflict/mode-python"),
  java: () => import("ace-builds/src-noconflict/mode-java"),
  c_cpp: () => import("ace-builds/src-noconflict/mode-c_cpp"),
  html: () => import("ace-builds/src-noconflict/mode-html"),
  css: () => import("ace-builds/src-noconflict/mode-css"),
  sql: () => import("ace-builds/src-noconflict/mode-sql"),
  json: () => import("ace-builds/src-noconflict/mode-json"),
  xml: () => import("ace-builds/src-noconflict/mode-xml"),
  yaml: () => import("ace-builds/src-noconflict/mode-yaml"),
  sh: () => import("ace-builds/src-noconflict/mode-sh"),
};

const aceThemeLoaders: Record<string, () => Promise<unknown>> = {
  github: () => import("ace-builds/src-noconflict/theme-github"),
  monokai: () => import("ace-builds/src-noconflict/theme-monokai"),
};

const loadedAceModes = new Set<string>();
const loadedAceThemes = new Set<string>();
const failedAceModes = new Set<string>();
const failedAceThemes = new Set<string>();
const loadingAceModes = new Map<string, Promise<void>>();
const loadingAceThemes = new Map<string, Promise<void>>();

interface EditorState {
  baseContent: string;
  content: string;
  filePath: string;
  isDirty: boolean;
}

const createEditorState = (
  filePath: string,
  baseContent: string,
): EditorState => ({
  filePath,
  baseContent,
  content: baseContent,
  isDirty: false,
});

const stateForSource = (
  state: EditorState,
  filePath: string,
  initialContent: string,
): EditorState => {
  if (state.filePath !== filePath) {
    return createEditorState(filePath, initialContent);
  }
  // Background refetches may update the source while the user is editing.
  // Preserve that draft; a clean editor can safely adopt the new source.
  if (!state.isDirty && state.baseContent !== initialContent) {
    return createEditorState(filePath, initialContent);
  }
  return state;
};

const FileEditor = ({
  editorName = "file-editor",
  enableSaveShortcut = true,
  filePath,
  fileName,
  initialContent,
  onSave,
  isSaving = false,
  readOnly = false,
  onDirtyChange,
  ref,
}: FileEditorProps) => {
  const [editorState, setEditorState] = useState<EditorState>(() =>
    createEditorState(filePath, initialContent),
  );
  const normalizedState = stateForSource(editorState, filePath, initialContent);
  const { content, isDirty } = normalizedState;
  const editorRef = useRef<InstanceType<typeof ReactAce>>(null);
  const theme = useAppTheme();
  const isDarkMode = theme.palette.mode === "dark";
  const language = getLanguageMode(fileName);
  const aceTheme = isDarkMode ? "monokai" : "github";
  const [, forceAssetRefresh] = useState(0);
  const isEditorAssetsReady =
    (!aceModeLoaders[language] ||
      loadedAceModes.has(language) ||
      failedAceModes.has(language)) &&
    (!aceThemeLoaders[aceTheme] ||
      loadedAceThemes.has(aceTheme) ||
      failedAceThemes.has(aceTheme));

  const updateEditorState = useCallback(
    (updater: (state: EditorState) => EditorState) => {
      setEditorState((prev) => {
        const current = stateForSource(prev, filePath, initialContent);
        return updater(current);
      });
    },
    [filePath, initialContent],
  );

  const handleSave = useCallback(async () => {
    try {
      const saved = await onSave(content);
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
  }, [onSave, content, isDirty, onDirtyChange, updateEditorState]);

  // Add Ctrl+S keyboard shortcut
  const handleKeyDown = useEffectEvent((e: KeyboardEvent) => {
    if ((e.ctrlKey || e.metaKey) && e.key === "s") {
      e.preventDefault();
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
    let isCancelled = false;
    const pendingLoads: Promise<unknown>[] = [];

    const loadMode = aceModeLoaders[language];
    if (
      loadMode &&
      !loadedAceModes.has(language) &&
      !failedAceModes.has(language)
    ) {
      const existingModeLoad = loadingAceModes.get(language);
      if (existingModeLoad) {
        pendingLoads.push(existingModeLoad);
      } else {
        const modeLoad = loadMode()
          .then(() => {
            loadedAceModes.add(language);
            return;
          })
          .catch((error) => {
            failedAceModes.add(language);
            console.error(`Failed to load Ace mode "${language}":`, error);
          })
          .finally(() => {
            loadingAceModes.delete(language);
          });
        loadingAceModes.set(language, modeLoad);
        pendingLoads.push(modeLoad);
      }
    }

    const loadTheme = aceThemeLoaders[aceTheme];
    if (
      loadTheme &&
      !loadedAceThemes.has(aceTheme) &&
      !failedAceThemes.has(aceTheme)
    ) {
      const existingThemeLoad = loadingAceThemes.get(aceTheme);
      if (existingThemeLoad) {
        pendingLoads.push(existingThemeLoad);
      } else {
        const themeLoad = loadTheme()
          .then(() => {
            loadedAceThemes.add(aceTheme);
            return;
          })
          .catch((error) => {
            failedAceThemes.add(aceTheme);
            console.error(`Failed to load Ace theme "${aceTheme}":`, error);
          })
          .finally(() => {
            loadingAceThemes.delete(aceTheme);
          });
        loadingAceThemes.set(aceTheme, themeLoad);
        pendingLoads.push(themeLoad);
      }
    }

    if (pendingLoads.length === 0) {
      return;
    }

    Promise.allSettled(pendingLoads)
      .finally(() => {
        if (!isCancelled) {
          forceAssetRefresh((version) => version + 1);
        }
      })
      .catch(() => {});

    return () => {
      isCancelled = true;
    };
  }, [language, aceTheme]);

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
    save: handleSave,
    getContent: () => content,
    isDirty: () => isDirty,
  }));

  if (!isEditorAssetsReady) {
    return (
      <div
        style={{
          width: "100%",
          height: "100%",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <ComponentLoader />
      </div>
    );
  }

  return (
    <AceEditor
      editorProps={{
        $blockScrolling: true,
      }}
      fontSize={14}
      mode={language}
      name={editorName}
      onChange={handleContentChange}
      readOnly={isSaving || readOnly}
      ref={editorRef}
      setOptions={{
        useWorker: true,
        enableBasicAutocompletion: true,
        enableLiveAutocompletion: true,
        enableSnippets: true,
        showLineNumbers: true,
        tabSize: 2,
      }}
      showPrintMargin={false}
      style={{ width: "100%", height: "100%" }}
      theme={aceTheme}
      value={content}
    />
  );
};

export default FileEditor;
