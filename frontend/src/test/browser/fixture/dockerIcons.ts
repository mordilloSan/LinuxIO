import type { QueryClient } from "@tanstack/react-query";

import { linuxio } from "@/api";

export function seedDockerIcons(queryClient: QueryClient) {
  // Deterministic, local icon artwork keeps this browser fixture independent of
  // external icon servers. Production uses the same DockerIcon query path.
  const marks: Record<string, { color: string; path: string }> = {
    nextcloud: {
      color: "#0082c9",
      path: '<circle cx="32" cy="32" r="9"/><circle cx="13" cy="32" r="6"/><circle cx="51" cy="32" r="6"/>',
    },
    postgres: {
      color: "#336791",
      path: '<path d="M20 44V22q12-12 24 0v20q-4 12-10 0V30M20 24l-6 12 8 4m20-16 8 12-8 4"/>',
    },
    redis: {
      color: "#c6302b",
      path: '<path d="m10 26 22-10 22 10-22 10Zm0 10 22 10 22-10M10 44l22 10 22-10"/>',
    },
    jellyfin: {
      color: "#8055cd",
      path: '<path d="M32 12 10 50h44ZM32 28 21 46h22Z"/>',
    },
    backup: {
      color: "#526579",
      path: '<path d="M16 20h32v30H16ZM12 14h40v8H12Zm14 18h12"/>',
    },
  };
  for (const [identifier, mark] of Object.entries(marks)) {
    queryClient.setQueryData(
      linuxio.docker.get_icon_uri({ identifier }).queryKey,
      {
        uri: `data:image/svg+xml,${encodeURIComponent(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="${mark.color}"/><g fill="none" stroke="white" stroke-width="3" stroke-linejoin="round">${mark.path}</g></svg>`)}`,
      },
    );
  }
}
