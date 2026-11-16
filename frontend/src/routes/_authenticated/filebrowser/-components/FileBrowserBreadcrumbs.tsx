import { getRouteApi } from "@tanstack/react-router";

import type { TrashDragItem } from "../-internalDrag";
import Breadcrumbs, { type BreadcrumbPlace } from "./Breadcrumbs";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");

interface FileBrowserBreadcrumbsProps {
  onDropItems?: (
    paths: string[],
    destinationDir: string,
    copy: boolean,
  ) => void;
  onRestoreItems?: (items: TrashDragItem[], destinationDir: string) => void;
  place?: BreadcrumbPlace;
}

const FileBrowserBreadcrumbs = ({
  onDropItems,
  onRestoreItems,
  place,
}: FileBrowserBreadcrumbsProps) => {
  const splat = fileBrowserRouteApi.useParams({
    select: (params) => params._splat,
  });
  const navigate = fileBrowserRouteApi.useNavigate();
  // A segment opens its folder, which also leaves a place such as the Trash.
  const handleNavigate = (path: string) => {
    const nextSplat = path.split("/").filter(Boolean).join("/");
    void navigate({
      to: "/filebrowser/$",
      params: { _splat: nextSplat },
      search: {},
    });
  };

  return (
    <Breadcrumbs
      onDropItems={onDropItems}
      onNavigate={handleNavigate}
      onRestoreItems={onRestoreItems}
      path={splat ? `/${splat}` : "/"}
      place={place}
    />
  );
};

export default FileBrowserBreadcrumbs;
