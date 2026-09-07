import { useSuspenseQuery } from "@tanstack/react-query";

import {
  linuxio,
  type ContainerInfo,
  type ContainerInspectInfo,
  type DockerNetwork,
} from "@/api";
import ContainerCard from "@/components/cards/ContainerCard";
import FrostedCard from "@/components/cards/FrostedCard";
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
import DockerResourceDetailsLayout from "./DockerResourceDetailsLayout";

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
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-12)",
        minWidth: 0,
      }}
    >
      <DockerResourceDetailsLayout
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
        <ContainerInfoSections
          container={container}
          sections={["monitoring"]}
        />
      </DockerResourceDetailsLayout>

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
        <InspectCard
          inspect={inspect}
          networks={networks}
          sections={["overview"]}
        />
        <InspectCard
          inspect={inspect}
          networks={networks}
          sections={["configuration"]}
        />
        <InspectCard
          inspect={inspect}
          networks={networks}
          sections={["environment"]}
        />
        <InspectCard
          inspect={inspect}
          networks={networks}
          sections={["labels"]}
        />
        <InspectCard
          inspect={inspect}
          networks={networks}
          sections={["ports", "mounts", "networks"]}
        />
      </div>
    </div>
  );
};

export default ContainerResourceDetails;
