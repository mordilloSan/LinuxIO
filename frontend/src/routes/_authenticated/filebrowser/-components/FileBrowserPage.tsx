import FileBrowserContent from "@/components/filebrowser/FileBrowserContent";
import FileBrowserDialogs from "@/components/filebrowser/FileBrowserDialogs";
import { useFileBrowserController } from "@/hooks/filebrowser/useFileBrowserController";

const FileBrowserPage = () => {
  const { contentProps, dialogsProps } = useFileBrowserController();

  return (
    <>
      <FileBrowserContent {...contentProps} />
      <FileBrowserDialogs {...dialogsProps} />
    </>
  );
};

export type { ViewMode } from "@/types/filebrowser";
export default FileBrowserPage;
