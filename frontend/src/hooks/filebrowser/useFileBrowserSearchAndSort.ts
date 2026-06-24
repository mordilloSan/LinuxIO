import {
  useCallback,
  useState,
  type Dispatch,
  type SetStateAction,
} from "react";

import type { SortField, SortOrder } from "@/types/filebrowser";

interface UseFileBrowserSearchAndSortParams {
  setSortField: Dispatch<SetStateAction<SortField>>;
  setSortOrder: Dispatch<SetStateAction<SortOrder>>;
}

export const useFileBrowserSearchAndSort = ({
  setSortField,
  setSortOrder,
}: UseFileBrowserSearchAndSortParams) => {
  const [searchQuery, setSearchQuery] = useState("");

  const handlePathChange = useCallback(() => {
    setSearchQuery("");
  }, []);

  const handleSearchChange = useCallback((value: string) => {
    setSearchQuery(value);
  }, []);

  const handleSortChange = useCallback(
    (field: SortField) => {
      setSortField((currentField) => {
        if (currentField === field) {
          setSortOrder((currentOrder) =>
            currentOrder === "asc" ? "desc" : "asc",
          );
          return field;
        }

        setSortOrder("asc");
        return field;
      });
    },
    [setSortField, setSortOrder],
  );

  return {
    handlePathChange,
    handleSearchChange,
    handleSortChange,
    searchQuery,
    setSearchQuery,
  };
};
