import AppSkeleton from "@/components/ui/AppSkeleton";
import { useDockerIcon } from "@/hooks/useDockerIcon";

interface DockerIconProps {
  alt?: string;
  identifier?: string;
  size?: number;
}

/**
 * Component to display Docker container/stack icons
 * Automatically fetches and caches icons from the backend
 */
const DockerIcon = ({
  identifier,
  size = 24,
  alt = "icon",
}: DockerIconProps) => {
  const { iconUri, isLoading, isError } = useDockerIcon(identifier);

  // No identifier provided
  if (!identifier) {
    return null;
  }

  // Loading state
  if (isLoading) {
    return (
      <AppSkeleton
        height={size}
        style={{ flexShrink: 0 }}
        variant="circular"
        width={size}
      />
    );
  }

  // Error state or no icon found - show placeholder
  if (isError || !iconUri) {
    return (
      <div
        style={{
          width: size,
          height: size,
          borderRadius: "50%",
          backgroundColor: "var(--app-palette-action-hover)",
          flexShrink: 0,
        }}
      />
    );
  }

  // Success - show the icon
  return (
    <img
      alt={alt}
      src={iconUri}
      style={{
        width: size,
        height: size,
        objectFit: "contain",
        flexShrink: 0,
      }}
    />
  );
};

export default DockerIcon;
