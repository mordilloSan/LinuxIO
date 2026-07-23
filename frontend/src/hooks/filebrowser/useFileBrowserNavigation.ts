import { useLocation, useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef } from "react";

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

  const previousPathRef = useRef(normalizedPath);
  useEffect(() => {
    if (normalizedPath === previousPathRef.current) return;
    previousPathRef.current = normalizedPath;
    onPathChange();
  }, [normalizedPath, onPathChange]);

  const handleOpenDirectory = useCallback(
    (path: string) => {
      if (path === "/") {
        navigate({ to: "/filebrowser/$", params: { _splat: "" }, search: {} });
        return;
      }

      const urlPath = path.split("/").filter(Boolean).join("/");
      navigate({
        to: "/filebrowser/$",
        params: { _splat: urlPath },
        search: {},
      });
    },
    [navigate],
  );

  return {
    handleOpenDirectory,
    normalizedPath,
  };
};
