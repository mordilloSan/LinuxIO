import type { DockerContainerUpdateRequest } from "@/api";

export const createDockerContainerUpdateRequest = (
  containerId: string,
): DockerContainerUpdateRequest => ({
  containerId,
  runId: crypto.randomUUID(),
});
