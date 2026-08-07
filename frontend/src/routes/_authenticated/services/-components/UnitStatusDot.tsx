import StatusDot from "@/components/ui/StatusDot";
import { getServiceStatusColor } from "@/constants/statusColors";

export default function UnitStatusDot({
  activeState,
}: {
  activeState: string;
}) {
  return (
    <StatusDot
      color={getServiceStatusColor(activeState)}
      style={{ marginRight: 8 }}
    />
  );
}
