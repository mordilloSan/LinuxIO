import { Box, Skeleton } from "@mui/material";
import React from "react";

import { useDockerIcon } from "@/hooks/useDockerIcon";

interface DockerIconProps {
  identifier?: string;
  size?: number;
  alt?: string;
}

/**
 * Component to display Docker container/stack icons
 * Automatically fetches and caches icons from the backend
 */
const DockerIcon: React.FC<DockerIconProps> = ({
  identifier,
  size = 24,
  alt = "icon",
}) => {
  const { iconUri, isLoading, isError } = useDockerIcon(identifier);

  // No identifier provided
  if (!identifier) {
    return null;
  }

  // Loading state
  if (isLoading) {
    return (
      <Skeleton
        variant="circular"
        width={size}
        height={size}
        sx={{ flexShrink: 0 }}
      />
    );
  }

  // Error state or no icon found - show placeholder
  if (isError || !iconUri) {
    return (
      <Box
        sx={{
          width: size,
          height: size,
          borderRadius: "50%",
          bgcolor: "action.hover",
          flexShrink: 0,
        }}
      />
    );
  }

  // Success - show the icon
  return (
    <Box
      component="img"
      src={iconUri}
      alt={alt}
      sx={{
        width: size,
        height: size,
        objectFit: "contain",
        flexShrink: 0,
      }}
    />
  );
};

export default DockerIcon;
