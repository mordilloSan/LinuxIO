import { useSuspenseQuery } from "@tanstack/react-query";

import {
  linuxio,
  type ContainerInfo,
  type ContainerInspectInfo,
  type DockerNetwork,
} from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import {
  cardHeight,
  CARD_PADDING_LG,
  DASHBOARD_CARD_GAP,
} from "@/theme/constants";
import { getContainerName } from "@/utils/dockerContainer";

import ContainerInfoSections from "./ContainerInfoSections";
import ContainerInspectSections, {
  type ContainerInspectSection,
} from "./ContainerInspectSections";
import ContainerCard from "../cards/ContainerCard";

interface ContainerResourceDetailsProps {
  container: ContainerInfo;
  onClose: () => void;
  stopping?: boolean;
}

const InspectCard = ({
  inspect,
  networks,
  sections,
}: {
  inspect: ContainerInspectInfo;
  networks: DockerNetwork[];
  sections: ContainerInspectSection[];
}) => (
  <FrostedCard
    style={{
      height: cardHeight,
      minWidth: 0,
      overflowY: "auto",
      padding: CARD_PADDING_LG,
    }}
  >
    <ContainerInspectSections
      inspect={inspect}
      networks={networks}
      sections={sections}
    />
  </FrostedCard>
);

const INSPECT_CARD_SECTIONS: ContainerInspectSection[][] = [
  ["overview"],
  ["configuration"],
  ["environment"],
  ["labels"],
  ["ports", "mounts", "networks"],
];

const ContainerResourceDetails = ({
  container,
  onClose,
  stopping = false,
}: ContainerResourceDetailsProps) => {
  const { data: inspect } = useSuspenseQuery({
    ...linuxio.docker.inspect_container({ containerId: container.Id }),
  });
  const { data: networks } = useSuspenseQuery({
    ...linuxio.docker.list_networks,
  });

  return (
    <ResourceDetailsLayout
      below={
        <div
          style={{
            alignItems: "start",
            display: "grid",
            gap: DASHBOARD_CARD_GAP,
            gridTemplateColumns:
              "repeat(auto-fit, minmax(min(100%, 320px), 1fr))",
            minWidth: 0,
          }}
        >
          {INSPECT_CARD_SECTIONS.map((sections) => (
            <InspectCard
              inspect={inspect}
              key={sections.join("-")}
              networks={networks}
              sections={sections}
            />
          ))}
        </div>
      }
      onClose={onClose}
      resourceLabel="container"
      subtitle="Runtime state and live metrics"
      summary={
        <ContainerCard
          actionPending={stopping}
          containerId={container.Id}
          onSelect={onClose}
          selected
        />
      }
      title={getContainerName(container)}
    >
      <ContainerInfoSections container={container} sections={["monitoring"]} />
    </ResourceDetailsLayout>
  );
};

export default ContainerResourceDetails;
