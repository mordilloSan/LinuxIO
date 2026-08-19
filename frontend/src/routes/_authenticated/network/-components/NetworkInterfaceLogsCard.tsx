import { useQuery } from "@tanstack/react-query";

import { linuxio } from "@/api";
import UnitLogsCard from "@/components/cards/UnitLogsCard";

import { selectNetworkInterfaceLogUnit } from "./networkSelectors";

/**
 * The journal of the unit that owns this interface's configuration, resolved
 * by the bridge. An empty unit means none of the candidate units are installed
 * on this host, so there is no journal to offer and the card stays out of the
 * layout entirely.
 */
const NetworkInterfaceLogsCard = ({ name }: { name: string }) => {
  const { data: logUnit } = useQuery({
    ...linuxio.network.get_network_info,
    refetchOnMount: false,
    select: selectNetworkInterfaceLogUnit(name),
  });

  if (!logUnit) return null;

  // Keyed on the unit so switching stacks opens a new stream instead of
  // appending a second unit's lines to the first one's buffer.
  return (
    <UnitLogsCard
      key={logUnit}
      title={`Network Logs (${logUnit})`}
      unitName={logUnit}
    />
  );
};

export default NetworkInterfaceLogsCard;
