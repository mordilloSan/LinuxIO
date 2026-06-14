import React from "react";

import FileBrowserContent from "./FileBrowserContent";
import FileBrowserDialogs from "./FileBrowserDialogs";
import { useFileBrowserController } from "./useFileBrowserController";

const FileBrowser: React.FC = () => {
  const { contentProps, dialogsProps } = useFileBrowserController();

  return (
    <>
      <FileBrowserContent {...contentProps} />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

export type { ViewMode } from "@/types/filebrowser";
export default FileBrowser;
