import { useSuspenseQuery } from "@tanstack/react-query";
import { memo, Suspense, useCallback, useRef, useState } from "react";

import {
  call,
  linuxio,
  LinuxIOError,
  uploadContent,
  type ComposeProject,
  useCallMutation,
} from "@/api";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ComposeEditorDialog from "@/components/docker/ComposeEditorDialog";
import ComposeOperationDialog from "@/components/docker/ComposeOperationDialog";
import ComposePostSaveDialog from "@/components/docker/ComposePostSaveDialog";
import type { ValidationResult } from "@/components/docker/ComposeValidationFeedback";
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
import { markTerminalFeedbackEmitted } from "@/hooks/backgroundTasks/terminalTaskFeedback";
import { useDockerSettings } from "@/hooks/useConfig";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useUploadChunkSize } from "@/hooks/useUploadChunkSize";
import { withPromiseCleanup } from "@/utils/withPromiseCleanup";

import ComposeList from "./ComposeList";

interface ComposeStacksPageProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}

const parentDirOf = (filePath: string): string => {
  const idx = filePath.lastIndexOf("/");
  return idx > 0 ? filePath.slice(0, idx) : "";
};

const envPathFor = (composeFilePath: string): string => {
  const dir = parentDirOf(composeFilePath);
  return dir ? `${dir}/.env` : "";
};

// Returns the env file's content, or null when it doesn't exist (or can't be
// read) — the editor uses null to mean "will be created on save".
const loadEnvContent = async (envPath: string): Promise<string | null> => {
  if (!envPath) return null;
  try {
    const result = await call(linuxio.filebrowser.read_text.route, {
      path: envPath,
    });
    return result?.content ?? "";
  } catch {
    return null;
  }
};

const ComposeStacksPage = ({
  onMountCreateHandler,
  viewMode = "table",
}: ComposeStacksPageProps) => {
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  const dockerSettings = useDockerSettings();
  const chunkSize = useUploadChunkSize();

  // Setup dialog state
  const [setupDialogOpen, setSetupDialogOpen] = useState(false);

  // Editor state
  const [editorOpen, setEditorOpen] = useState(false);
  const [editorMode, setEditorMode] = useState<"create" | "edit">("create");
  const [editingStackName, setEditingStackName] = useState("");
  const [editingFilePath, setEditingFilePath] = useState("");
  const [editingContent, setEditingContent] = useState("");
  const [editingEnvPath, setEditingEnvPath] = useState("");
  const [editingEnvContent, setEditingEnvContent] = useState<string | null>(
    null,
  );

  // Post-save dialog state
  const [postSaveDialogOpen, setPostSaveDialogOpen] = useState(false);
  const [postSaveStackName, setPostSaveStackName] = useState("");
  const postSaveFilePathRef = useRef("");
  const [postSaveStackState, setPostSaveStackState] = useState<
    "new" | "running" | "stopped"
  >("new");

  // Overwrite confirmation dialog state
  const [overwriteDialogOpen, setOverwriteDialogOpen] = useState(false);
  const [pendingSaveData, setPendingSaveData] = useState<{
    content: string;
    stackName: string;
    filePath: string;
    envContent?: string;
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

  const { data: rawProjects, refetch } = useSuspenseQuery({
    ...linuxio.docker.list_compose_projects,
    refetchInterval: 5000,
  });
  const projects = rawProjects;

  // Success/error toasts are composed per delete option in handleDeleteConfirm.
  const { mutateAsync: deleteStack } = useCallMutation(
    linuxio.docker.delete_stack,
  );

  // Loader-style read: the file content feeds the editor, fetched fresh on
  // every open (no cache entry wanted).
  // Event-driven commands behind the editor flow; error handling stays with
  // the calling workflow, so no declarative toast config here.
  const { mutateAsync: validateCompose } = useCallMutation(
    linuxio.docker.validate_compose,
  );
  const { mutateAsync: resolveComposeFilePath } = useCallMutation(
    linuxio.docker.get_compose_file_path,
  );

  // Handle operation dialog close
  const handleOperationDialogClose = useCallback(() => {
    setOperationDialogOpen(false);
    // Refresh projects after operation completes
    void refetch();
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

      return withPromiseCleanup(
        (async () => {
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
          }
        })(),
        () => {
          setDeleteLoading(false);
        },
      );
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
      const envPath = `${workingDir}/.env`;
      // The chosen directory may already hold a .env; surface it for editing.
      void loadEnvContent(envPath).then((envContent) => {
        setEditorMode("create");
        setEditingStackName(stackName);
        setEditingFilePath(`${workingDir}/docker-compose.yml`);
        setEditingContent("");
        setEditingEnvPath(envPath);
        setEditingEnvContent(envContent);
        setEditorOpen(true);
      });
    },
    [],
  );

  useRegisterCreateHandler(onMountCreateHandler, handleCreateStack);

  // Open the editor with a stack's compose file (and sibling .env) loaded.
  const openStackEditor = useCallback(
    async (projectName: string, configPath: string) => {
      try {
        const envPath = envPathFor(configPath);
        const [result, envContent] = await Promise.all([
          call(linuxio.filebrowser.read_text.route, { path: configPath }),
          loadEnvContent(envPath),
        ]);

        if (result && result.content) {
          setEditorMode("edit");
          setEditingStackName(projectName);
          setEditingFilePath(configPath);
          setEditingContent(result.content);
          setEditingEnvPath(envPath);
          setEditingEnvContent(envContent);
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
    [toast],
  );

  const handleEditStack = useCallback(
    (projectName: string, configPath: string) =>
      openStackEditor(projectName, configPath),
    [openStackEditor],
  );

  // Validate compose file against the stack's real directory and the env
  // buffer currently in the editor, so ${VAR} interpolation sees the same
  // values a deploy would.
  const handleValidate = useCallback(
    async (content: string, envContent: string): Promise<ValidationResult> => {
      try {
        return await validateCompose({
          content,
          envContent: envContent || undefined,
          workingDir: parentDirOf(editingFilePath) || undefined,
        });
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
    [editingFilePath, validateCompose],
  );

  // Internal save function that performs the actual save
  const performSave = useCallback(
    async (
      content: string,
      stackName: string,
      filePath: string,
      override: boolean = false,
      envContent?: string,
    ) => {
      const encoder = new TextEncoder();
      const contentBytes = encoder.encode(content);
      await uploadContent(filePath, contentBytes, {
        chunkSize,
        // handleSave owns the outcome (overwrite dialog on 409, toasts
        // otherwise), so the global background-tasks watcher must not also
        // report this task's failure.
        onTaskStart: (task) => markTerminalFeedbackEmitted(task.id),
        overwrite: override || undefined,
      });

      // Persist the sibling .env when its buffer changed. The editor is
      // authoritative for it (loaded fresh on open), so overwrite is safe.
      if (envContent !== undefined) {
        const envPath = envPathFor(filePath);
        if (envPath) {
          await uploadContent(envPath, encoder.encode(envContent), {
            chunkSize,
            onTaskStart: (task) => markTerminalFeedbackEmitted(task.id),
            overwrite: true,
          });
        }
      }

      toast.success(
        envContent !== undefined
          ? "Compose and .env files saved successfully"
          : "Compose file saved successfully",
      );

      // Invalidate queries
      void refetch();

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
      postSaveFilePathRef.current = filePath;
      setPostSaveStackState(state);
      setPostSaveDialogOpen(true);
    },
    [chunkSize, projects, refetch, toast],
  );

  // Save compose file with overwrite protection
  const handleSave = useCallback(
    async (
      content: string,
      stackName: string,
      existingFilePath: string,
      envContent?: string,
    ) => {
      let filePath = existingFilePath;

      try {
        // Get the file path (either from existing file or build new one)
        if (editorMode === "create") {
          const pathInfo = await resolveComposeFilePath({ stackName });
          filePath = pathInfo.path;
        }

        // Try to save without override first
        await performSave(content, stackName, filePath, false, envContent);
        return true;
      } catch (error) {
        // The upload task reports an existing destination as a structured 409.
        if (error instanceof LinuxIOError && error.code === 409) {
          // Store pending save data and show confirmation dialog
          setPendingSaveData({ content, stackName, filePath, envContent });
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
    return withPromiseCleanup(
      (async () => {
        try {
          await performSave(
            pendingSaveData.content,
            pendingSaveData.stackName,
            pendingSaveData.filePath,
            true, // override = true
            pendingSaveData.envContent,
          );
        } catch (error) {
          toast.error(
            `Failed to save file: ${error instanceof Error ? error.message : "Unknown error"}`,
          );
          throw error;
        }
      })(),
      () => {
        setPendingSaveData(null);
      },
    );
  }, [pendingSaveData, performSave, toast]);

  // Handle overwrite cancellation
  const handleOverwriteCancel = useCallback(() => {
    setOverwriteDialogOpen(false);
    setPendingSaveData(null);
  }, []);

  // Post-save action handlers
  const handlePostSaveStart = () => {
    startProject(postSaveStackName, postSaveFilePathRef.current);
    setPostSaveDialogOpen(false);
  };

  const handlePostSaveRestart = () => {
    restartProject(postSaveStackName, postSaveFilePathRef.current);
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
          onRestart={restartProject}
          onStart={startProject}
          onStop={stopProject}
          projects={projects}
          viewMode={viewMode}
        />

        <ComposeEditorDialog
          envFilePath={editingEnvPath}
          filePath={editingFilePath}
          initialContent={editingContent}
          initialEnvContent={editingEnvContent}
          mode={editorMode}
          onClose={() => setEditorOpen(false)}
          onSave={handleSave}
          onValidate={handleValidate}
          open={editorOpen}
          readOnly={false}
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
          defaultWorkingDir={dockerSettings.folders?.[0]}
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
