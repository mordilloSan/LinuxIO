import { useSuspenseQuery } from "@tanstack/react-query";
import { memo, Suspense, useCallback, useState } from "react";

import {
  CACHE_TTL_MS,
  linuxio,
  LinuxIOError,
  uploadContent,
  type ComposeProject,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComposeEditorDialog from "@/components/docker/ComposeEditorDialog";
import ComposeOperationDialog from "@/components/docker/ComposeOperationDialog";
import ComposePostSaveDialog from "@/components/docker/ComposePostSaveDialog";
import { ValidationResult } from "@/components/docker/ComposeValidationFeedback";
import DeleteStackDialog, {
  type DeleteOption,
} from "@/components/docker/DeleteStackDialog";
import StackSetupDialog from "@/components/docker/StackSetupDialog";
import PageLoader from "@/components/loaders/PageLoader";
import AppButton from "@/components/ui/AppButton";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import { useConfig } from "@/hooks/useConfig";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useUploadChunkSize } from "@/hooks/useUploadChunkSize";

import ComposeList from "./ComposeList";

interface ComposeStacksPageProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const ComposeStacksPage = ({
  onMountCreateHandler,
  viewMode = "table",
}: ComposeStacksPageProps) => {
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const { config } = useConfig();
  const chunkSize = useUploadChunkSize();

  // Setup dialog state
  const [setupDialogOpen, setSetupDialogOpen] = useState(false);

  // Editor state
  const [editorOpen, setEditorOpen] = useState(false);
  const [editorMode, setEditorMode] = useState<"create" | "edit">("create");
  const [editorReadOnly, setEditorReadOnly] = useState(false);
  const [editingStackName, setEditingStackName] = useState("");
  const [editingFilePath, setEditingFilePath] = useState("");
  const [editingContent, setEditingContent] = useState("");

  // Post-save dialog state
  const [postSaveDialogOpen, setPostSaveDialogOpen] = useState(false);
  const [postSaveStackName, setPostSaveStackName] = useState("");
  const [postSaveFilePath, setPostSaveFilePath] = useState("");
  const [postSaveStackState, setPostSaveStackState] = useState<
    "new" | "running" | "stopped"
  >("new");

  // Overwrite confirmation dialog state
  const [overwriteDialogOpen, setOverwriteDialogOpen] = useState(false);
  const [pendingSaveData, setPendingSaveData] = useState<{
    content: string;
    stackName: string;
    filePath: string;
  } | null>(null);

  // Compose operation dialog state
  const [operationDialogOpen, setOperationDialogOpen] = useState(false);
  const [operationAction, setOperationAction] = useState<
    "up" | "down" | "stop" | "restart"
  >("up");
  const [operationProjectName, setOperationProjectName] = useState("");
  const [operationComposePath, setOperationComposePath] = useState<
    string | undefined
  >(undefined);

  // Delete stack dialog state
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteDialogProject, setDeleteDialogProject] =
    useState<ComposeProject | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const { data: rawProjects, refetch } = useSuspenseQuery(
    linuxio.docker.list_compose_projects.queryOptions({
      refetchInterval: 5000,
    }),
  );
  const projects = rawProjects;

  // Success/error toasts are composed per delete option in handleDeleteConfirm.
  const { mutateAsync: deleteStack } =
    linuxio.docker.delete_stack.useJobAction();

  // Loader-style read: the file content feeds the editor, fetched fresh on
  // every open (no cache entry wanted).
  const fetchComposeFile = linuxio.filebrowser.resource_get.useFetcher();
  // Event-driven commands behind the editor flow; error handling stays with
  // the calling workflow, so no declarative toast config here.
  const { mutateAsync: validateCompose } =
    linuxio.docker.validate_compose.useAction();
  const { mutateAsync: resolveComposeFilePath } =
    linuxio.docker.get_compose_file_path.useAction();

  // Handle operation dialog close
  const handleOperationDialogClose = useCallback(() => {
    setOperationDialogOpen(false);
    // Refresh projects after operation completes
    refetch();
  }, [refetch]);

  const startProject = useCallback((projectName: string, filePath?: string) => {
    setOperationAction("up");
    setOperationProjectName(projectName);
    setOperationComposePath(filePath);
    setOperationDialogOpen(true);
  }, []);

  const stopProject = useCallback((projectName: string) => {
    setOperationAction("stop");
    setOperationProjectName(projectName);
    setOperationComposePath(undefined);
    setOperationDialogOpen(true);
  }, []);

  const restartProject = useCallback(
    (projectName: string, filePath?: string) => {
      setOperationAction("restart");
      setOperationProjectName(projectName);
      setOperationComposePath(filePath);
      setOperationDialogOpen(true);
    },
    [],
  );

  // Open delete dialog with project info
  const handleOpenDeleteDialog = useCallback((project: ComposeProject) => {
    setDeleteDialogProject(project);
    setDeleteDialogOpen(true);
  }, []);

  // Handle delete confirmation based on selected option
  const handleDeleteConfirm = useCallback(
    async (option: DeleteOption) => {
      if (!deleteDialogProject) return;

      const projectName = deleteDialogProject.name;
      setDeleteLoading(true);

      try {
        if (option === "containers") {
          // Just run docker compose down via operation dialog
          setDeleteDialogOpen(false);
          setDeleteDialogProject(null);
          setDeleteLoading(false);
          setOperationAction("down");
          setOperationProjectName(projectName);
          setOperationComposePath(undefined);
          setOperationDialogOpen(true);
        } else {
          // Use the delete_stack endpoint with options
          const deleteFile = option === "file" || option === "directory";
          const deleteDirectory = option === "directory";

          await deleteStack({
            projectName,
            deleteFile,
            deleteDirectory,
          });

          const successMsg =
            option === "directory"
              ? `Stack ${projectName} and its directory deleted successfully`
              : `Stack ${projectName} and compose file deleted successfully`;
          toast.success(successMsg);

          setDeleteDialogOpen(false);
          setDeleteDialogProject(null);
        }
      } catch (error) {
        toast.error(
          `Failed to delete stack: ${error instanceof Error ? error.message : "Unknown error"}`,
        );
      } finally {
        setDeleteLoading(false);
      }
    },
    [deleteDialogProject, deleteStack, toast],
  );

  const handleDeleteDialogClose = useCallback(() => {
    if (!deleteLoading) {
      setDeleteDialogOpen(false);
      setDeleteDialogProject(null);
    }
  }, [deleteLoading]);

  const isLoading = operationDialogOpen;

  // Create stack handler - open setup dialog first
  const handleCreateStack = useCallback(() => {
    setSetupDialogOpen(true);
  }, []);

  // Setup dialog confirm - open editor with configured values
  const handleSetupConfirm = useCallback(
    (stackName: string, workingDir: string) => {
      setSetupDialogOpen(false);
      setEditorMode("create");
      setEditingStackName(stackName);
      setEditingFilePath(`${workingDir}/docker-compose.yml`);
      setEditingContent("");
      setEditorOpen(true);
    },
    [],
  );

  useRegisterCreateHandler(onMountCreateHandler, handleCreateStack);

  // Open the editor with a stack's compose file loaded (read-only previews).
  const openStackEditor = useCallback(
    async (projectName: string, configPath: string, readOnly: boolean) => {
      try {
        const result = await fetchComposeFile(
          { path: configPath, unused: "", getContent: "true" },
          { staleTime: CACHE_TTL_MS.NONE, gcTime: CACHE_TTL_MS.NONE },
        );

        if (result && result.content) {
          setEditorMode("edit");
          setEditorReadOnly(readOnly);
          setEditingStackName(projectName);
          setEditingFilePath(configPath);
          setEditingContent(result.content);
          setEditorOpen(true);
        } else {
          toast.error("Failed to load compose file content");
        }
      } catch (error) {
        toast.error(
          `Failed to load compose file: ${error instanceof Error ? error.message : "Unknown error"}`,
        );
      }
    },
    [fetchComposeFile, toast],
  );

  const handleEditStack = useCallback(
    (projectName: string, configPath: string) =>
      openStackEditor(projectName, configPath, false),
    [openStackEditor],
  );

  const handlePreviewStack = useCallback(
    (projectName: string, configPath: string) =>
      openStackEditor(projectName, configPath, true),
    [openStackEditor],
  );

  // Validate compose file
  const handleValidate = useCallback(
    async (content: string): Promise<ValidationResult> => {
      try {
        return await validateCompose({ content });
      } catch (error) {
        return {
          valid: false,
          errors: [
            {
              message:
                error instanceof Error ? error.message : "Validation failed",
              type: "error",
            },
          ],
        };
      }
    },
    [validateCompose],
  );

  // Internal save function that performs the actual save
  const performSave = useCallback(
    async (
      content: string,
      stackName: string,
      filePath: string,
      override: boolean = false,
    ) => {
      const encoder = new TextEncoder();
      const contentBytes = encoder.encode(content);
      await uploadContent(filePath, contentBytes, {
        chunkSize,
        overwrite: override || undefined,
      });

      toast.success("Compose file saved successfully");

      // Invalidate queries
      refetch();

      // Close editor
      setEditorOpen(false);

      // Determine stack state
      const existingProject = projects.find((p) => p.name === stackName);
      let state: "new" | "running" | "stopped" = "new";
      if (existingProject) {
        state =
          existingProject.status === "running" ||
          existingProject.status === "partial"
            ? "running"
            : "stopped";
      }

      // Show post-save dialog
      setPostSaveStackName(stackName);
      setPostSaveFilePath(filePath);
      setPostSaveStackState(state);
      setPostSaveDialogOpen(true);
    },
    [chunkSize, projects, refetch, toast],
  );

  // Save compose file with overwrite protection
  const handleSave = useCallback(
    async (content: string, stackName: string, existingFilePath: string) => {
      let filePath = existingFilePath;

      try {
        // Get the file path (either from existing file or build new one)
        if (editorMode === "create") {
          const pathInfo = await resolveComposeFilePath({ stackName });
          filePath = pathInfo.path;
        }

        // Try to save without override first
        await performSave(content, stackName, filePath, false);
        return true;
      } catch (error) {
        // The upload job reports an existing destination as a structured 409.
        if (error instanceof LinuxIOError && error.code === 409) {
          // Store pending save data and show confirmation dialog
          setPendingSaveData({ content, stackName, filePath });
          setOverwriteDialogOpen(true);
          return false;
        } else {
          // Re-throw other errors
          toast.error(
            `Failed to save file: ${error instanceof Error ? error.message : "Unknown error"}`,
          );
          throw error;
        }
      }
    },
    [editorMode, performSave, resolveComposeFilePath, toast],
  );

  // Handle overwrite confirmation
  const handleOverwriteConfirm = useCallback(async () => {
    if (!pendingSaveData) return;

    setOverwriteDialogOpen(false);
    try {
      await performSave(
        pendingSaveData.content,
        pendingSaveData.stackName,
        pendingSaveData.filePath,
        true, // override = true
      );
    } catch (error) {
      toast.error(
        `Failed to save file: ${error instanceof Error ? error.message : "Unknown error"}`,
      );
      throw error;
    } finally {
      setPendingSaveData(null);
    }
  }, [pendingSaveData, performSave, toast]);

  // Handle overwrite cancellation
  const handleOverwriteCancel = useCallback(() => {
    setOverwriteDialogOpen(false);
    setPendingSaveData(null);
  }, []);

  // Post-save action handlers
  const handlePostSaveStart = () => {
    startProject(postSaveStackName, postSaveFilePath);
    setPostSaveDialogOpen(false);
  };

  const handlePostSaveRestart = () => {
    restartProject(postSaveStackName, postSaveFilePath);
    setPostSaveDialogOpen(false);
  };

  const handlePostSaveDoNothing = () => {
    setPostSaveDialogOpen(false);
  };

  return (
    <Suspense fallback={<PageLoader />}>
      <div
        style={{
          display: "flex",
          flexDirection: "column",
          height: "100%",
          minHeight: 0,
        }}
      >
        <ComposeList
          isLoading={isLoading}
          onDelete={handleOpenDeleteDialog}
          onEdit={handleEditStack}
          onPreview={handlePreviewStack}
          onRestart={restartProject}
          onStart={startProject}
          onStop={stopProject}
          projects={projects}
          viewMode={viewMode}
        />

        <ComposeEditorDialog
          filePath={editingFilePath}
          initialContent={editingContent}
          mode={editorMode}
          onClose={() => setEditorOpen(false)}
          onSave={handleSave}
          onValidate={handleValidate}
          open={editorOpen}
          readOnly={editorReadOnly}
          stackName={editingStackName}
        />

        <ComposePostSaveDialog
          isExecuting={operationDialogOpen}
          onDoNothing={handlePostSaveDoNothing}
          onRestart={handlePostSaveRestart}
          onStart={handlePostSaveStart}
          open={postSaveDialogOpen}
          stackName={postSaveStackName}
          stackState={postSaveStackState}
        />

        <StackSetupDialog
          defaultWorkingDir={config.docker.folders?.[0]}
          onClose={() => setSetupDialogOpen(false)}
          onConfirm={handleSetupConfirm}
          open={setupDialogOpen}
        />

        <ComposeOperationDialog
          action={operationAction}
          composePath={operationComposePath}
          onClose={handleOperationDialogClose}
          open={operationDialogOpen}
          projectName={operationProjectName}
        />

        <DeleteStackDialog
          configFiles={deleteDialogProject?.config_files || []}
          isLoading={deleteLoading}
          onClose={handleDeleteDialogClose}
          onConfirm={handleDeleteConfirm}
          open={deleteDialogOpen}
          projectName={deleteDialogProject?.name || ""}
          workingDir={deleteDialogProject?.working_dir || ""}
        />

        <GeneralDialog
          fullWidth
          maxWidth="sm"
          onClose={handleOverwriteCancel}
          open={overwriteDialogOpen}
        >
          <AppDialogTitle>File Already Exists</AppDialogTitle>
          <AppDialogContent>
            <AppDialogContentText>
              The file <strong>{pendingSaveData?.filePath}</strong> already
              exists. Do you want to overwrite it?
            </AppDialogContentText>
          </AppDialogContent>
          <AppDialogActions>
            <AppButton color="inherit" onClick={handleOverwriteCancel}>
              Cancel
            </AppButton>
            <AppButton
              color="warning"
              onClick={handleOverwriteConfirm}
              variant="contained"
            >
              Overwrite
            </AppButton>
          </AppDialogActions>
        </GeneralDialog>
      </div>
    </Suspense>
  );
};

export default memo(ComposeStacksPage);
