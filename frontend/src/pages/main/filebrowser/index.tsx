import React from "react";

import FileBrowserContent from "@/components/filebrowser/FileBrowserContent";
import FileBrowserDialogs from "@/components/filebrowser/FileBrowserDialogs";
import { useFileBrowserController } from "@/hooks/filebrowser/useFileBrowserController";

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
