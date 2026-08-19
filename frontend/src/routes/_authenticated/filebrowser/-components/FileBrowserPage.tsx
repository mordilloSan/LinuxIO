import { getRouteApi } from "@tanstack/react-router";

import FileBrowserContent from "@/components/filebrowser/FileBrowserContent";
import FileBrowserDialogs from "@/components/filebrowser/FileBrowserDialogs";
import { useFileBrowserController } from "@/hooks/filebrowser/useFileBrowserController";

const fileBrowserRouteApi = getRouteApi("/_authenticated/filebrowser/$");

const FileBrowserPage = () => {
  const fileBrowserListingQueryOptions = fileBrowserRouteApi.useRouteContext({
    select: (context) => context.fileBrowserListingQueryOptions,
  });
  const { contentProps, dialogsProps } = useFileBrowserController(
    fileBrowserListingQueryOptions,
  );

  return (
    <>
      <FileBrowserContent {...contentProps} />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

export type { ViewMode } from "@/types/filebrowser";
export default FileBrowserPage;
