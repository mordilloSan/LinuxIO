import { getRouteApi } from "@tanstack/react-router";

import FileBrowserContent from "@/components/filebrowser/FileBrowserContent";
import FileBrowserDialogs from "@/components/filebrowser/FileBrowserDialogs";
import { useFileBrowserController } from "@/hooks/filebrowser/useFileBrowserController";

import FileBrowserBreadcrumbs from "./FileBrowserBreadcrumbs";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");
// Keep the header slot stable. This leaf subscribes to `_splat` itself, so a
// directory navigation updates the trail without re-rendering the header.
const fileBrowserBreadcrumbs = <FileBrowserBreadcrumbs />;

const FileBrowserPage = () => {
  const fileBrowserListingQueryOptions = fileBrowserRouteApi.useRouteContext({
    select: (context) => context.fileBrowserListingQueryOptions,
  });
  const { contentProps, dialogsProps } = useFileBrowserController(
    fileBrowserListingQueryOptions,
  );

  return (
    <>
      <FileBrowserContent
        {...contentProps}
        breadcrumbs={fileBrowserBreadcrumbs}
      />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

export type { ViewMode } from "@/types/filebrowser";
export default FileBrowserPage;
