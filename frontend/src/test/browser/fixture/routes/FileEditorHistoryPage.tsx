import AppButton from "@/components/ui/AppButton";
import { useFileEditor } from "@/hooks/filebrowser/useFileEditor";

export default function FileEditorHistoryPage() {
  const { actions, closeEditorDialog, editingPath } = useFileEditor();

  return (
    <main style={{ display: "grid", gap: 8, padding: 16 }}>
      <h1>File editor history fixture</h1>
      <output data-testid="editing-path">{editingPath ?? "none"}</output>
      <div style={{ display: "flex", gap: 8 }}>
        <AppButton onClick={() => actions.openFile("/note.txt")}>
          Open note
        </AppButton>
        <AppButton onClick={() => actions.setDirty(true)}>Make dirty</AppButton>
        <AppButton onClick={actions.close}>Close editor</AppButton>
      </div>
      {closeEditorDialog && (
        <div role="alertdialog" aria-label="Unsaved changes">
          <AppButton onClick={actions.dismissClosePrompt}>
            Keep editing
          </AppButton>
          <AppButton onClick={actions.confirmClose}>Discard and exit</AppButton>
        </div>
      )}
    </main>
  );
}
