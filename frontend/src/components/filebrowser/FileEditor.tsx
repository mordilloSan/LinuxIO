import { useTheme } from "@mui/material";
import {
  useState,
  useEffect,
  useImperativeHandle,
  forwardRef,
  useRef,
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
    const [content, setContent] = useState(initialContent);
    const [isDirty, setIsDirty] = useState(false);
    const editorRef = useRef<AceEditor>(null);
    const theme = useTheme();
    const isDarkMode = theme.palette.mode === "dark";

    useEffect(() => {
      setContent(initialContent);
      setIsDirty(false);
    }, [filePath, initialContent]);

    useEffect(() => {
      onDirtyChange?.(isDirty);
    }, [isDirty, onDirtyChange]);

    const handleContentChange = (newValue: string) => {
      setContent(newValue);
      setIsDirty(newValue !== initialContent);
    };

    const handleSave = async () => {
      try {
        await onSave(content);
        setIsDirty(false);
      } catch {
        // Error is handled by parent component
      }
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
