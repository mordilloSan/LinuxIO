import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useMemo, useRef, useState } from "react";

import { emptyCapabilityState } from "@/api/capabilities";
import type { CapabilitiesResponse } from "@/api/capabilities";
import VirtualDirectoryItems from "@/components/filebrowser/VirtualDirectoryItems";
import AppButton from "@/components/ui/AppButton";
import { AuthContext } from "@/contexts/AuthContext";
import type { AuthContextType } from "@/types/auth";
import type { FileItem, ViewMode } from "@/types/filebrowser";

const makeItems = (count: number, offset = 0): FileItem[] =>
  Array.from({ length: count }, (_, index) => ({
    name: `fixture-${offset + index}.txt`,
    path: `/fixture/fixture-${offset + index}.txt`,
    size: (offset + index) * 10,
    type: "file",
  }));

const callbacks = {
  onCancelRename: () => {},
  onConfirmRename: () => {},
  onContainerMouseDown: () => {},
  onDownloadFile: () => {},
  onFileClick: () => {},
  onFileContextMenu: () => {},
  onFolderClick: () => {},
  onFolderContextMenu: () => {},
  onMarqueeMouseDown: () => {},
  onOpenDirectory: () => {},
};

const authValue: AuthContextType = {
  ...emptyCapabilityState,
  isAuthenticated: false,
  isInitialized: true,
  method: "session",
  privileged: false,
  refreshCapabilities: async () => ({}) as CapabilitiesResponse,
  sessionExpired: () => {},
  signIn: async () => {},
  signOut: async () => {},
  user: null,
};
const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: false },
  },
});

export default function VirtualFileBrowserPage() {
  const containerRef = useRef<HTMLDivElement>(null);
  const [viewMode, setViewMode] = useState<ViewMode>("list");
  const [count, setCount] = useState(240);
  const files = useMemo(() => makeItems(count), [count]);

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <main
          data-testid="virtual-filebrowser-fixture"
          style={{ boxSizing: "border-box", height: "100dvh", padding: 24 }}
        >
          <h1>Virtual file browser fixture</h1>
          <div style={{ display: "flex", gap: 8, marginBottom: 12 }}>
            <AppButton onClick={() => setViewMode("list")}>List view</AppButton>
            <AppButton onClick={() => setViewMode("card")}>Card view</AppButton>
            <AppButton onClick={() => setCount((current) => current + 40)}>
              Add items
            </AppButton>
            <AppButton
              onClick={() => setCount((current) => Math.max(20, current - 40))}
            >
              Remove items
            </AppButton>
          </div>
          <div data-testid="virtual-filebrowser-status">
            items: {files.length}; view: {viewMode}
          </div>
          <div
            data-testid="virtual-filebrowser-scrollport"
            style={{
              border: "1px solid #555",
              height: "calc(100% - 96px)",
              minHeight: 0,
            }}
          >
            <VirtualDirectoryItems
              containerRef={containerRef}
              cutPaths={new Set()}
              files={files}
              folders={[]}
              isLoadingSubfolders={false}
              isMarqueeSelecting={false}
              onCancelRename={callbacks.onCancelRename}
              onConfirmRename={callbacks.onConfirmRename}
              onContainerMouseDown={callbacks.onContainerMouseDown}
              onDownloadFile={callbacks.onDownloadFile}
              onFileClick={callbacks.onFileClick}
              onFileContextMenu={callbacks.onFileContextMenu}
              onFolderClick={callbacks.onFolderClick}
              onFolderContextMenu={callbacks.onFolderContextMenu}
              onMarqueeMouseDown={callbacks.onMarqueeMouseDown}
              onOpenDirectory={callbacks.onOpenDirectory}
              renamingPath={null}
              revealIndex={-1}
              selectedPaths={new Set()}
              selectionBox={null}
              subfoldersMap={new Map()}
              viewMode={viewMode}
            />
          </div>
        </main>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}
