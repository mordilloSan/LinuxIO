import { useCallback, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

interface UseFileBrowserNavigationParams {
  onPathChange: () => void;
}

export const useFileBrowserNavigation = ({
  onPathChange,
}: UseFileBrowserNavigationParams) => {
  const location = useLocation();
  const navigate = useNavigate();

  const urlPath = location.pathname
    .replace(/^\/filebrowser\/?/, "")
    .split("/")
    .map((segment) => decodeURIComponent(segment))
    .join("/");
  const normalizedPath = urlPath ? `/${urlPath}` : "/";

  const [prevNormalizedPath, setPrevNormalizedPath] = useState(normalizedPath);
  if (normalizedPath !== prevNormalizedPath) {
    setPrevNormalizedPath(normalizedPath);
    onPathChange();
  }

  const handleOpenDirectory = useCallback(
    (path: string) => {
      if (path === "/") {
        navigate("/filebrowser");
        return;
      }

      const urlPath = path
        .split("/")
        .filter(Boolean)
        .map((segment) => encodeURIComponent(segment))
        .join("/");
      navigate(`/filebrowser/${urlPath}`);
    },
    [navigate],
  );

  return {
    handleOpenDirectory,
    normalizedPath,
  };
};
