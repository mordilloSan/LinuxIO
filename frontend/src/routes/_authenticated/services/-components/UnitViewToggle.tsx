import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useViewMode } from "@/hooks/useViewMode";

interface UnitViewToggleProps {
  viewModeKey: string;
}

const UnitViewToggle = ({ viewModeKey }: UnitViewToggleProps) => {
  const [viewMode, setViewMode] = useViewMode(viewModeKey, "table");

  return (
    <ViewModeToggle
      alternateMode="table"
      onViewModeChange={setViewMode}
      viewMode={viewMode}
    />
  );
};

export default UnitViewToggle;
