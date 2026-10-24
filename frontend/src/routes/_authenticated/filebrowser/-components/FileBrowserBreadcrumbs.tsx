import { getRouteApi } from "@tanstack/react-router";
import { useCallback } from "react";

import Breadcrumbs from "./Breadcrumbs";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");

interface FileBrowserBreadcrumbsProps {
  onDropItems: (paths: string[], destinationDir: string, copy: boolean) => void;
}

const FileBrowserBreadcrumbs = ({
  onDropItems,
}: FileBrowserBreadcrumbsProps) => {
  const splat = fileBrowserRouteApi.useParams({
    select: (params) => params._splat,
  });
  const navigate = fileBrowserRouteApi.useNavigate();
  const handleNavigate = useCallback(
    (path: string) => {
      const nextSplat = path.split("/").filter(Boolean).join("/");
      void navigate({
        to: "/filebrowser/$",
        params: { _splat: nextSplat },
        search: {},
      });
    },
    [navigate],
  );

  return (
    <Breadcrumbs
      onDropItems={onDropItems}
      onNavigate={handleNavigate}
      path={splat ? `/${splat}` : "/"}
    />
  );
};

export default FileBrowserBreadcrumbs;
