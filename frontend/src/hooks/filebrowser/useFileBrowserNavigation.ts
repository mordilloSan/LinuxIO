import { useLocation, useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef, useState } from "react";

interface UseFileBrowserNavigationParams {
  onPathChange: () => void;
}

const FILEBROWSER_PREFIX = /^\/filebrowser\/?/;

function deriveNormalizedPath(pathname: string): string {
  const urlPath = pathname
    .replace(FILEBROWSER_PREFIX, "")
    .split("/")
    .map((segment) => decodeURIComponent(segment))
    .join("/");
  return urlPath ? `/${urlPath}` : "/";
}

export const useFileBrowserNavigation = ({
  onPathChange,
}: UseFileBrowserNavigationParams) => {
  const location = useLocation();
  const navigate = useNavigate();

  // `location.pathname` flips to the target route as soon as a navigation
  // starts, but this page can stay mounted until that route finishes
  // loading (e.g. TanStack Router's pending state). Only adopt a new path
  // while we're actually still under /filebrowser, otherwise keep the last
  // valid value instead of firing a query for whatever page we're leaving to.
  const [normalizedPath, setNormalizedPath] = useState(() =>
    deriveNormalizedPath(location.pathname),
  );
  if (FILEBROWSER_PREFIX.test(location.pathname)) {
    const next = deriveNormalizedPath(location.pathname);
    if (next !== normalizedPath) {
      setNormalizedPath(next);
    }
  }

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
