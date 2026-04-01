const linuxioManagedComposeProjects = new Set([
  "linuxio-watchtower",
  "linuxio-monitoring",
]);

export const isLinuxIOManagedComposeProject = (
  projectName?: string | null,
): boolean => {
  return !!projectName && linuxioManagedComposeProjects.has(projectName);
};

export const isLinuxIOManagedContainer = (
  labels?: Record<string, string> | null,
): boolean => {
  return isLinuxIOManagedComposeProject(labels?.["com.docker.compose.project"]);
};
