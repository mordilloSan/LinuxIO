import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
} from "@mui/material";
import React, { Suspense, useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import ComposeList, { type ComposeProject } from "./ComposeList";

import {
  encodeString,
  getStreamMux,
  STREAM_CHUNK_SIZE,
  type ResultFrame,
} from "@/api/linuxio";
import linuxio from "@/api/react-query";
import ComposeEditorDialog from "@/components/docker/ComposeEditorDialog";
import ComposeOperationDialog from "@/components/docker/ComposeOperationDialog";
import ComposePostSaveDialog from "@/components/docker/ComposePostSaveDialog";
import { ValidationResult } from "@/components/docker/ComposeValidationFeedback";
import DeleteStackDialog, {
  type DeleteOption,
} from "@/components/docker/DeleteStackDialog";
import ReindexDialog from "@/components/docker/ReindexDialog";
import StackSetupDialog from "@/components/docker/StackSetupDialog";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import { useConfig } from "@/hooks/useConfig";

interface ComposeStacksPageProps {
  onMountCreateHandler?: (handler: () => void) => void;
  onMountReindexHandler?: (handler: () => void) => void;
}

const ComposeStacksPage: React.FC<ComposeStacksPageProps> = ({
  onMountCreateHandler,
  onMountReindexHandler,
}) => {
  const { config } = useConfig();

  // Setup dialog state
  const [setupDialogOpen, setSetupDialogOpen] = useState(false);

  // Editor state
  const [editorOpen, setEditorOpen] = useState(false);
  const [editorMode, setEditorMode] = useState<"create" | "edit">("create");
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

  // Reindex dialog state
  const [reindexDialogOpen, setReindexDialogOpen] = useState(false);

  // Delete stack dialog state
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [deleteDialogProject, setDeleteDialogProject] =
    useState<ComposeProject | null>(null);
  const [deleteLoading, setDeleteLoading] = useState(false);

  const {
    data: projects = [],
    isPending,
    refetch,
  } = linuxio.docker.list_compose_projects.useQuery({
    refetchInterval: 5000,
  });

  const { mutateAsync: deleteStack } =
    linuxio.docker.delete_stack.useMutation();
  const { mutateAsync: getResource } =
    linuxio.filebrowser.resource_get.useMutation();
  const { mutateAsync: validateCompose } =
    linuxio.docker.validate_compose.useMutation();
  const { mutateAsync: getComposeFilePath } =
    linuxio.docker.get_compose_file_path.useMutation();

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

          await deleteStack([
            projectName,
            deleteFile ? "true" : "false",
            deleteDirectory ? "true" : "false",
          ]);

          const successMsg =
            option === "directory"
              ? `Stack ${projectName} and its directory deleted successfully`
              : `Stack ${projectName} and compose file deleted successfully`;
          toast.success(successMsg);

          refetch();

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
    [deleteDialogProject, deleteStack, refetch],
  );

  const handleDeleteDialogClose = useCallback(() => {
    if (!deleteLoading) {
      setDeleteDialogOpen(false);
      setDeleteDialogProject(null);
    }
  }, [deleteLoading]);

  const handleReindex = useCallback(() => {
    setReindexDialogOpen(true);
  }, []);

  const handleReindexComplete = useCallback(() => {
    refetch();
    toast.success("Docker folder reindexed successfully");
  }, [refetch]);

  const isLoading = operationDialogOpen || reindexDialogOpen;

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

  // Mount handlers to parent
  useEffect(() => {
    if (onMountCreateHandler) {
      onMountCreateHandler(handleCreateStack);
    }
  }, [onMountCreateHandler, handleCreateStack]);

  useEffect(() => {
    if (onMountReindexHandler) {
      onMountReindexHandler(handleReindex);
    }
  }, [onMountReindexHandler, handleReindex]);

  // Edit stack handler
  const handleEditStack = useCallback(
    async (projectName: string, configPath: string) => {
      try {
        // Fetch file content
        const result = await getResource([configPath, "", "true"]);

        if (result && result.content) {
          setEditorMode("edit");
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
    [getResource],
  );

  // Validate compose file
  const handleValidate = useCallback(
    async (content: string): Promise<ValidationResult> => {
      try {
        const result = await validateCompose([content]);
        return result;
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
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        toast.error("Stream connection not ready");
        throw new Error("Stream connection not ready");
      }

      const encoder = new TextEncoder();
      const contentBytes = encoder.encode(content);
      const contentSize = contentBytes.length;

      // Build payload with optional override flag
      const args = [filePath, contentSize.toString()];
      if (override) {
        args.push("true");
      }
      const payload = encodeString(`fb-upload\0${args.join("\0")}`);
      const stream = mux.openStream("fb-upload", payload);

      if (!stream) {
        toast.error("Failed to open save stream");
        throw new Error("Failed to open save stream");
      }

      await new Promise<void>((resolve, reject) => {
        stream.onResult = (result: ResultFrame) => {
          if (result.status === "ok") {
            resolve();
          } else {
            reject(new Error(result.error || "Save failed"));
          }
        };

        stream.onClose = () => {
          reject(new Error("Stream closed unexpectedly"));
        };

        // Send content in chunks
        let offset = 0;
        const sendNextChunk = () => {
          if (stream.status !== "open") return;

          if (offset >= contentSize) {
            stream.close();
            return;
          }

          const chunk = contentBytes.slice(offset, offset + STREAM_CHUNK_SIZE);
          stream.write(chunk);
          offset += chunk.length;

          // Continue sending
          if (offset < contentSize) {
            setTimeout(sendNextChunk, 0);
          } else {
            stream.close();
          }
        };

        sendNextChunk();
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
    [projects, refetch],
  );

  // Save compose file with overwrite protection
  const handleSave = useCallback(
    async (content: string, stackName: string, existingFilePath: string) => {
      let filePath = existingFilePath;

      try {
        // Get the file path (either from existing file or build new one)
        if (editorMode === "create") {
          const pathInfo = await getComposeFilePath([stackName]);
          filePath = pathInfo.path;
        }

        // Try to save without override first
        await performSave(content, stackName, filePath, false);
      } catch (error) {
        // Check if error is due to file already existing
        if (
          error instanceof Error &&
          error.message.includes("file already exists")
        ) {
          // Store pending save data and show confirmation dialog
          setPendingSaveData({ content, stackName, filePath });
          setOverwriteDialogOpen(true);
        } else {
          // Re-throw other errors
          toast.error(
            `Failed to save file: ${error instanceof Error ? error.message : "Unknown error"}`,
          );
          throw error;
        }
      }
    },
    [editorMode, getComposeFilePath, performSave],
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
  }, [pendingSaveData, performSave]);

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
    <Suspense fallback={<ComponentLoader />}>
      <Box>
        {isPending ? (
          <ComponentLoader />
        ) : (
          <ComposeList
            projects={projects}
            onStart={startProject}
            onStop={stopProject}
            onRestart={restartProject}
            onDelete={handleOpenDeleteDialog}
            onEdit={handleEditStack}
            isLoading={isLoading}
          />
        )}

        <ComposeEditorDialog
          open={editorOpen}
          mode={editorMode}
          stackName={editingStackName}
          filePath={editingFilePath}
          initialContent={editingContent}
          onClose={() => setEditorOpen(false)}
          onSave={handleSave}
          onValidate={handleValidate}
        />

        <ComposePostSaveDialog
          open={postSaveDialogOpen}
          stackName={postSaveStackName}
          stackState={postSaveStackState}
          onStart={handlePostSaveStart}
          onRestart={handlePostSaveRestart}
          onDoNothing={handlePostSaveDoNothing}
          isExecuting={operationDialogOpen}
        />

        <StackSetupDialog
          open={setupDialogOpen}
          onClose={() => setSetupDialogOpen(false)}
          onConfirm={handleSetupConfirm}
          defaultWorkingDir={config.dockerFolder}
        />

        <ComposeOperationDialog
          open={operationDialogOpen}
          onClose={handleOperationDialogClose}
          action={operationAction}
          projectName={operationProjectName}
          composePath={operationComposePath}
        />

        <ReindexDialog
          open={reindexDialogOpen}
          onClose={() => setReindexDialogOpen(false)}
          onComplete={handleReindexComplete}
        />

        <DeleteStackDialog
          open={deleteDialogOpen}
          onClose={handleDeleteDialogClose}
          onConfirm={handleDeleteConfirm}
          projectName={deleteDialogProject?.name || ""}
          configFiles={deleteDialogProject?.config_files || []}
          workingDir={deleteDialogProject?.working_dir || ""}
          isLoading={deleteLoading}
        />

        <Dialog
          open={overwriteDialogOpen}
          onClose={handleOverwriteCancel}
          maxWidth="sm"
          fullWidth
        >
          <DialogTitle>File Already Exists</DialogTitle>
          <DialogContent>
            <DialogContentText>
              The file <strong>{pendingSaveData?.filePath}</strong> already
              exists. Do you want to overwrite it?
            </DialogContentText>
          </DialogContent>
          <DialogActions>
            <Button onClick={handleOverwriteCancel} color="inherit">
              Cancel
            </Button>
            <Button
              onClick={handleOverwriteConfirm}
              color="warning"
              variant="contained"
            >
              Overwrite
            </Button>
          </DialogActions>
        </Dialog>
      </Box>
    </Suspense>
  );
};

export default ComposeStacksPage;
