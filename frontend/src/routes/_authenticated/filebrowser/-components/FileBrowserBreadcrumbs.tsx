import { getRouteApi } from "@tanstack/react-router";
import { useCallback } from "react";

import Breadcrumbs from "@/components/filebrowser/Breadcrumbs";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");

const FileBrowserBreadcrumbs = () => {
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
    <Breadcrumbs onNavigate={handleNavigate} path={splat ? `/${splat}` : "/"} />
  );
};

export default FileBrowserBreadcrumbs;
