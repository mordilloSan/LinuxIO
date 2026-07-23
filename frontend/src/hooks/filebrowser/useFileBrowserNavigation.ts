import { useLocation, useNavigate } from "@tanstack/react-router";
import { useCallback, useEffect, useRef } from "react";

interface UseFileBrowserNavigationParams {
  onPathChange: () => void;
}

const FILEBROWSER_PREFIX = /^\/filebrowser\/?/;

export const useFileBrowserNavigation = ({
  onPathChange,
}: UseFileBrowserNavigationParams) => {
  const location = useLocation();
  const navigate = useNavigate();

  // `location.pathname` flips to the target route as soon as a navigation
  // starts, but this page can stay mounted until that route finishes
  // loading (e.g. TanStack Router's pending state). Only re-derive the path
  // while we're actually still under /filebrowser, otherwise keep the last
  // valid value instead of firing a query for whatever page we're leaving to.
  const lastNormalizedPathRef = useRef("/");
  if (FILEBROWSER_PREFIX.test(location.pathname)) {
    const urlPath = location.pathname
      .replace(FILEBROWSER_PREFIX, "")
      .split("/")
      .map((segment) => decodeURIComponent(segment))
      .join("/");
    lastNormalizedPathRef.current = urlPath ? `/${urlPath}` : "/";
  }
  const normalizedPath = lastNormalizedPathRef.current;

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
