import { useContext } from "react";

import { FileTransferContext } from "@/contexts/FileTransferContext";

export const useFileTransfers = () => {
  const context = useContext(FileTransferContext);
  if (!context) {
    throw new Error(
      "useFileTransfers must be used within FileTransferProvider",
    );
  }
  return context;
};

export default useFileTransfers;
