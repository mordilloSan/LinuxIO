import { getRouteApi } from "@tanstack/react-router";

import FileBrowserBreadcrumbs from "./FileBrowserBreadcrumbs";
import FileBrowserContent from "./FileBrowserContent";
import FileBrowserDialogs from "./FileBrowserDialogs";
import { useFileBrowserController } from "../-hooks/useFileBrowserController";

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
      <FileBrowserContent
        {...contentProps}
        // The trail subscribes to `_splat` itself, so a directory navigation
        // updates it without re-rendering the header; `onDropItems` is stable.
        breadcrumbs={
          <FileBrowserBreadcrumbs
            onDropItems={contentProps.chrome.onDropItems}
          />
        }
      />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

export type { ViewMode } from "../-types";
export default FileBrowserPage;
