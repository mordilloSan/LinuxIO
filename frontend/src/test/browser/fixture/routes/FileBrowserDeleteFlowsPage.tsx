import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useState, type MouseEvent } from "react";

import { linuxio } from "@/api";
import AppButton from "@/components/ui/AppButton";
import ContextMenu from "@/routes/_authenticated/filebrowser/-components/ContextMenu";
import DirectoryListing from "@/routes/_authenticated/filebrowser/-components/DirectoryListing";
import {
  FileBrowserConflictDialog,
  FileBrowserDeleteDialog,
} from "@/routes/_authenticated/filebrowser/-components/FileBrowserPanels";
import type {
  ConflictDecision,
  ConflictPrompt,
} from "@/routes/_authenticated/filebrowser/-hooks/useFileConflicts";
import {
  type DeleteMode,
  useFileDialogs,
} from "@/routes/_authenticated/filebrowser/-hooks/useFileDialogs";
import type { FileResource } from "@/routes/_authenticated/filebrowser/-types";

// The real listing, context menu and dialogs, wired the way the page wires
// them, so the delete, trash and merge flows run through real key and mouse
// events instead of hook calls.
const resource: FileResource = {
  hidden: false,
  isRegularFile: false,
  items: [
    { name: "photos", path: "/fixture/photos/", type: "directory" },
    { name: "a.txt", path: "/fixture/a.txt", size: 12, type: "file" },
    { name: "b.txt", path: "/fixture/b.txt", size: 34, type: "file" },
  ],
  name: "fixture",
  path: "/fixture",
  size: 0,
  symlink: false,
  type: "directory",
};

const conflictPromptFixture: ConflictPrompt = {
  conflicts: [
    {
      existing: { modified: "", size: 0 },
      isDir: true,
      name: "photos",
      path: "/dst/photos",
    },
    {
      existing: { modified: "", size: 12 },
      isDir: false,
      name: "a.txt",
      path: "/dst/a.txt",
    },
  ],
  destination: "/dst",
};

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false, staleTime: Infinity },
  },
});
queryClient.setQueryData(
  linuxio.filebrowser.subfolders({ path: resource.path }).queryKey,
  { subfolders: [] },
);

const noop = () => {};

function DeleteFlowsFixture() {
  const dialogs = useFileDialogs();
  const [selectedPaths, setSelectedPaths] = useState<Set<string>>(
    () => new Set(),
  );
  const [menuPosition, setMenuPosition] = useState<{
    left: number;
    top: number;
  } | null>(null);
  const [deleteResult, setDeleteResult] = useState("none");
  const [conflictPrompt, setConflictPrompt] = useState<ConflictPrompt | null>(
    null,
  );
  const [conflictResult, setConflictResult] = useState("none");
  const [dropResult, setDropResult] = useState("none");
  const [openResult, setOpenResult] = useState("none");
  const [upResult, setUpResult] = useState("none");

  const requestDelete = (mode: DeleteMode) => {
    setMenuPosition(null);
    if (selectedPaths.size > 0) {
      dialogs.actions.requestDelete(Array.from(selectedPaths), mode);
    }
  };
  const handleContextMenu = (event: MouseEvent) => {
    event.preventDefault();
    setMenuPosition({ left: event.clientX, top: event.clientY });
  };
  const handleResolve = (decisions: Record<string, ConflictDecision>) => {
    setConflictResult(JSON.stringify(decisions));
    setConflictPrompt(null);
  };

  return (
    <main style={{ boxSizing: "border-box", height: "100dvh", padding: 24 }}>
      <h1>File-browser delete flows fixture</h1>
      <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
        <AppButton onClick={() => setConflictPrompt(conflictPromptFixture)}>
          Open conflicts
        </AppButton>
        <output data-testid="delete-result">{deleteResult}</output>
        <output data-testid="conflict-result">{conflictResult}</output>
        <output data-testid="drop-result">{dropResult}</output>
        <output data-testid="open-result">{openResult}</output>
        <output data-testid="up-result">{upResult}</output>
      </div>
      <div
        onContextMenu={handleContextMenu}
        style={{ height: "calc(100% - 120px)", minHeight: 0 }}
      >
        <DirectoryListing
          cutPaths={new Set()}
          isContextMenuOpen={menuPosition !== null}
          onCancelRename={noop}
          onConfirmRename={noop}
          onDelete={() => requestDelete("trash")}
          onDeletePermanently={() => requestDelete("permanent")}
          onDownloadFile={noop}
          onDropItems={(paths, destinationDir, copy) =>
            setDropResult(
              `${copy ? "copy" : "move"}:${paths.join(",")}->${destinationDir}`,
            )
          }
          onNavigateUp={() => setUpResult("up")}
          onOpenDirectory={setOpenResult}
          onSelectedPathsChange={setSelectedPaths}
          onStartRename={noop}
          renamingPath={null}
          resource={resource}
          selectedPaths={selectedPaths}
          showHiddenFiles={false}
          sortField="name"
          sortOrder="asc"
          viewMode="list"
        />
      </div>
      <ContextMenu
        anchorPosition={menuPosition}
        hasSelection={selectedPaths.size > 0}
        onChangePermissions={noop}
        onClose={() => setMenuPosition(null)}
        onCopy={noop}
        onCreateFile={noop}
        onCreateFolder={noop}
        onCut={noop}
        onDelete={() => requestDelete("trash")}
        onDeletePermanently={() => requestDelete("permanent")}
        onDownload={noop}
        onPaste={noop}
        onRename={noop}
        onUpload={noop}
      />
      <FileBrowserDeleteDialog
        mode={dialogs.deleteMode}
        onClose={dialogs.actions.closeDelete}
        onConfirm={() => {
          setDeleteResult(
            `${dialogs.deleteMode}:${dialogs.pendingDeletePaths.join(",")}`,
          );
          dialogs.actions.closeDelete();
        }}
        open={dialogs.deleteDialog}
        pendingDeletePaths={dialogs.pendingDeletePaths}
      />
      <FileBrowserConflictDialog
        onCancel={() => setConflictPrompt(null)}
        onResolve={handleResolve}
        prompt={conflictPrompt}
      />
    </main>
  );
}

export default function FileBrowserDeleteFlowsPage() {
  return (
    <QueryClientProvider client={queryClient}>
      <DeleteFlowsFixture />
    </QueryClientProvider>
  );
}
