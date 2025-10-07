export interface CollapsibleColumn {
  field: string;
  headerName: string;
  align?: "left" | "center" | "right";
}

export interface CollapsibleTableProps<T> {
  rows: T[];
  columns: CollapsibleColumn[];
  renderCollapseContent: (row: T) => React.ReactNode;
}
