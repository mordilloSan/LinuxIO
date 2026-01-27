import { useTheme } from "@mui/material";
import {
  useState,
  useEffect,
  useEffectEvent,
  useImperativeHandle,
  forwardRef,
  useRef,
  useCallback,
} from "react";
import AceEditor from "react-ace";
import "ace-builds/src-noconflict/mode-javascript";
import "ace-builds/src-noconflict/mode-python";
import "ace-builds/src-noconflict/mode-java";
import "ace-builds/src-noconflict/mode-c_cpp";
import "ace-builds/src-noconflict/mode-html";
import "ace-builds/src-noconflict/mode-css";
import "ace-builds/src-noconflict/mode-sql";
import "ace-builds/src-noconflict/mode-json";
import "ace-builds/src-noconflict/mode-xml";
import "ace-builds/src-noconflict/mode-yaml";
import "ace-builds/src-noconflict/mode-sh";
import "ace-builds/src-noconflict/theme-github";
import "ace-builds/src-noconflict/theme-monokai";

interface FileEditorProps {
  filePath: string;
  fileName: string;
  initialContent: string;
  onSave: (content: string) => Promise<void>;
  isSaving?: boolean;
  onDirtyChange?: (isDirty: boolean) => void;
}

export interface FileEditorHandle {
  save: () => Promise<void>;
  getContent: () => string;
  isDirty: () => boolean;
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
  };
  return modeMap[ext] || "text";
};

type EditorState = {
  filePath: string;
  baseContent: string;
  content: string;
  isDirty: boolean;
};

const createEditorState = (
  filePath: string,
  baseContent: string,
): EditorState => ({
  filePath,
  baseContent,
  content: baseContent,
  isDirty: false,
});

const FileEditor = forwardRef<FileEditorHandle, FileEditorProps>(
  (
    {
      filePath,
      fileName,
      initialContent,
      onSave,
      isSaving = false,
      onDirtyChange,
    },
    ref,
  ) => {
    const [editorState, setEditorState] = useState<EditorState>(() =>
      createEditorState(filePath, initialContent),
    );
    const normalizedState =
      editorState.filePath === filePath &&
      editorState.baseContent === initialContent
        ? editorState
        : createEditorState(filePath, initialContent);
    const { content, isDirty } = normalizedState;
    const editorRef = useRef<AceEditor>(null);
    const theme = useTheme();
    const isDarkMode = theme.palette.mode === "dark";

    const updateEditorState = useCallback(
      (updater: (state: EditorState) => EditorState) => {
        setEditorState((prev) => {
          const current =
            prev.filePath === filePath && prev.baseContent === initialContent
              ? prev
              : createEditorState(filePath, initialContent);
          return updater(current);
        });
      },
      [filePath, initialContent],
    );

    const handleSave = useCallback(async () => {
      try {
        await onSave(content);
        updateEditorState((state) => ({
          ...state,
          baseContent: state.content,
          isDirty: false,
        }));
      } catch {
        // Error is handled by parent component
      }
    }, [onSave, content, updateEditorState]);

    useEffect(() => {
      onDirtyChange?.(isDirty);
    }, [isDirty, onDirtyChange]);

    // Add Ctrl+S keyboard shortcut
    const handleKeyDown = useEffectEvent((e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "s") {
        e.preventDefault();
        if (!isSaving) {
          handleSave();
        }
      }
    });

    useEffect(() => {
      document.addEventListener("keydown", handleKeyDown);
      return () => document.removeEventListener("keydown", handleKeyDown);
    }, []);

    const handleContentChange = (newValue: string) => {
      updateEditorState((state) => ({
        ...state,
        content: newValue,
        isDirty: newValue !== state.baseContent,
      }));
    };

    useImperativeHandle(ref, () => ({
      save: handleSave,
      getContent: () => content,
      isDirty: () => isDirty,
    }));

    const language = getLanguageMode(fileName);
    const aceTheme = isDarkMode ? "monokai" : "github";

    return (
      <AceEditor
        ref={editorRef}
        mode={language}
        theme={aceTheme}
        onChange={handleContentChange}
        value={content}
        name="file-editor"
        readOnly={isSaving}
        style={{ width: "100%", height: "100%" }}
        fontSize={14}
        showPrintMargin={false}
        setOptions={{
          useWorker: true,
          enableBasicAutocompletion: true,
          enableLiveAutocompletion: true,
          enableSnippets: true,
          showLineNumbers: true,
          tabSize: 2,
        }}
        editorProps={{
          $blockScrolling: true,
        }}
      />
    );
  },
);

FileEditor.displayName = "FileEditor";

export default FileEditor;
