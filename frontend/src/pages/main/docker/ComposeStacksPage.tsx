import { Box } from "@mui/material";
import { useQueryClient } from "@tanstack/react-query";
import React, { Suspense, useCallback, useEffect, useState } from "react";
import { toast } from "sonner";

import ComposeList from "./ComposeList";

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
  const queryClient = useQueryClient();
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

  // Compose operation dialog state
  const [operationDialogOpen, setOperationDialogOpen] = useState(false);
  const [operationAction, setOperationAction] = useState<
    "up" | "down" | "stop" | "restart"
  >("up");
  const [operationProjectName, setOperationProjectName] = useState("");
  const [operationComposePath, setOperationComposePath] = useState<
    string | undefined
  >(undefined);

  const { data: projects = [], isPending } =
    linuxio.docker.list_compose_projects.useQuery({
      refetchInterval: 5000,
    });

  // Handle operation dialog close
  const handleOperationDialogClose = useCallback(() => {
    setOperationDialogOpen(false);
    // Refresh projects after operation completes
    queryClient.invalidateQueries({
      queryKey: ["docker", "list_compose_projects"],
    });
  }, [queryClient]);

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

  const restartProject = useCallback((projectName: string) => {
    setOperationAction("restart");
    setOperationProjectName(projectName);
    setOperationComposePath(undefined);
    setOperationDialogOpen(true);
  }, []);

  const downProject = useCallback((projectName: string) => {
    setOperationAction("down");
    setOperationProjectName(projectName);
    setOperationComposePath(undefined);
    setOperationDialogOpen(true);
  }, []);

  const [isReindexing, setIsReindexing] = useState(false);

  const handleReindex = useCallback(async () => {
    setIsReindexing(true);
    try {
      await linuxio.call<{ message: string; status: string }>(
        "docker",
        "reindex_docker_folder",
        [],
      );
      toast.success("Docker folder reindexed successfully");
      queryClient.invalidateQueries({
        queryKey: ["docker", "list_compose_projects"],
      });
    } catch (error) {
      toast.error(
        `Failed to reindex: ${error instanceof Error ? error.message : "Unknown error"}`,
      );
    } finally {
      setIsReindexing(false);
    }
  }, [queryClient]);

  const isLoading = isReindexing || operationDialogOpen;

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
        const result = await linuxio.call<{ content?: string }>(
          "filebrowser",
          "resource_get",
          [configPath, "", "true"],
        );

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
    [],
  );

  // Validate compose file
  const handleValidate = useCallback(
    async (content: string): Promise<ValidationResult> => {
      try {
        const result = await linuxio.call<ValidationResult>(
          "docker",
          "validate_compose",
          [content],
        );
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
    [],
  );

  // Save compose file
  const handleSave = useCallback(
    async (content: string, stackName: string, existingFilePath: string) => {
      const mux = getStreamMux();
      if (!mux || mux.status !== "open") {
        toast.error("Stream connection not ready");
        throw new Error("Stream connection not ready");
      }

      try {
        // Get the file path (either from existing file or build new one)
        let filePath = existingFilePath;

        if (editorMode === "create") {
          const pathInfo = await linuxio.call<{
            path: string;
            exists: boolean;
            directory: string;
          }>("docker", "get_compose_file_path", [stackName]);
          filePath = pathInfo.path;
        }

        const encoder = new TextEncoder();
        const contentBytes = encoder.encode(content);
        const contentSize = contentBytes.length;

        // Open stream with fb-upload type
        const payload = encodeString(`fb-upload\0${filePath}\0${contentSize}`);
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

            const chunk = contentBytes.slice(
              offset,
              offset + STREAM_CHUNK_SIZE,
            );
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
        queryClient.invalidateQueries({
          queryKey: ["docker", "list_compose_projects"],
        });

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
      } catch (error) {
        toast.error(
          `Failed to save file: ${error instanceof Error ? error.message : "Unknown error"}`,
        );
        throw error;
      }
    },
    [editorMode, projects, queryClient],
  );

  // Post-save action handlers
  const handlePostSaveStart = () => {
    startProject(postSaveStackName, postSaveFilePath);
    setPostSaveDialogOpen(false);
  };

  const handlePostSaveRestart = () => {
    restartProject(postSaveStackName);
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
            onDown={downProject}
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
      </Box>
    </Suspense>
  );
};

export default ComposeStacksPage;
