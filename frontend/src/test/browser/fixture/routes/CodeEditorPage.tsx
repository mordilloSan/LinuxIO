import { useState } from "react";

import FileEditor from "@/components/filebrowser/FileEditor";
import AppButton from "@/components/ui/AppButton";

const INITIAL_CONTENT = '{\n  "enabled": true\n}';

export default function CodeEditorPage() {
  const [savedContent, setSavedContent] = useState("Not saved");

  return (
    <main style={{ height: "100vh", padding: "var(--app-space-16)" }}>
      <h1>Code editor fixture</h1>
      <div style={{ height: 320 }}>
        <FileEditor
          fileName="fixture.json"
          filePath="/fixture.json"
          initialContent={INITIAL_CONTENT}
          onSave={async (content) => {
            setSavedContent(content);
            return true;
          }}
        />
      </div>
      <AppButton>After editor</AppButton>
      <output data-testid="saved-content">{savedContent}</output>
    </main>
  );
}
