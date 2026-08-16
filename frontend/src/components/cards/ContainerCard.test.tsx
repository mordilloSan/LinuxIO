import { describe, expect, it, vi } from "vitest";

import type { ContainerInfo } from "@/api";
import { render, screen } from "@/test/render";

import ContainerCard from "./ContainerCard";

const container: ContainerInfo = {
  Id: "container-1",
  Image: "example:latest",
  Names: ["/example"],
  State: "running",
  Status: "Up 2 hours",
  icon: undefined,
  metrics: undefined,
  updateAvailable: true,
} as ContainerInfo;

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: ({
      select,
    }: {
      select: (items: ContainerInfo[]) => ContainerInfo;
    }) => ({ data: select([container]) }),
  };
});

vi.mock("@/api", async () => {
  const actual = await vi.importActual<typeof import("@/api")>("@/api");
  const noOpAction = () => ({ mutate: vi.fn(), isPending: false });

  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        update_container: {
          ...actual.linuxio.docker.update_container,
          useTaskAction: noOpAction,
        },
      },
    },
    useCallMutation: noOpAction,
  };
});

vi.mock("@/components/docker/ContainerInfoSections", () => ({
  default: () => null,
}));

vi.mock("@/components/docker/DockerIcon", () => ({
  default: () => <span aria-hidden="true" />,
}));

vi.mock("@/components/gauge/MetricBar", () => ({
  default: () => null,
}));

describe("ContainerCard", () => {
  it("shows the update-available indicator in compact view", () => {
    const { rerender } = render(
      <ContainerCard containerId={container.Id} selected={false} />,
    );

    expect(screen.getByText("Update available")).toBeInTheDocument();

    container.updateAvailable = false;
    rerender(<ContainerCard containerId={container.Id} selected={false} />);

    expect(screen.queryByText("Update available")).not.toBeInTheDocument();
  });
});
