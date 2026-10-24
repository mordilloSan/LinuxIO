import AppButton from "@/components/ui/AppButton";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";

interface FileBrowserSearchProps {
  caseSensitive: boolean;
  onCaseSensitiveChange: (value: boolean) => void;
  onSearchChange: (value: string) => void;
  searchQuery: string;
}

const FileBrowserSearch = ({
  caseSensitive,
  onCaseSensitiveChange,
  onSearchChange,
  searchQuery,
}: FileBrowserSearchProps) => (
  <div style={{ alignItems: "center", display: "flex", gap: 4, width: "100%" }}>
    <div style={{ flex: 1, minWidth: 0 }}>
      <AppHeaderSearch
        onChange={onSearchChange}
        placeholder="Search files and folders..."
        value={searchQuery}
      />
    </div>
    <AppButton
      aria-pressed={caseSensitive}
      onClick={() => onCaseSensitiveChange(!caseSensitive)}
      size="small"
      style={{ whiteSpace: "nowrap" }}
      variant={caseSensitive ? "contained" : "outlined"}
    >
      Match case
    </AppButton>
  </div>
);

export default FileBrowserSearch;
