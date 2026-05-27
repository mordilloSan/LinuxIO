export interface CollapsibleColumn {
  align?: "left" | "center" | "right";
  field: string;
  headerName: string;
}

export interface CollapsibleTableProps<T> {
  columns: CollapsibleColumn[];
  renderCollapseContent: (row: T) => React.ReactNode;
  rows: T[];
}
