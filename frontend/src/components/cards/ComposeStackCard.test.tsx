import { describe, expect, it, vi } from "vitest";

import type { ComposeProject, ContainerInfo } from "@/api";

const frostedCardRender = vi.hoisted(() => vi.fn());

vi.mock("@/components/cards/FrostedCard", () => ({
  default: ({ children }: { children: React.ReactNode }) => {
    frostedCardRender();
    return <section>{children}</section>;
  },
}));

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ alt }: { alt: string }) => <span>{alt}</span>,
}));

const ComposeStackCard = (await import("@/components/cards/ComposeStackCard"))
  .default;
const { render, screen } = await import("@/test/render");

const container: ContainerInfo = {
  Created: 1,
  Id: "container-one",
  Image: "demo:latest",
  Names: ["/demo"],
  State: "running",
  Status: "Up 1 minute",
};

const project: ComposeProject = {
  config_files: ["/srv/demo/compose.yaml"],
  containers: [container],
  name: "demo",
  services: {
    web: {
      container_count: 1,
      container_ids: [container.Id],
      image: container.Image,
      name: "web",
      ports: [],
      state: "running",
      status: "Up 1 minute",
    },
  },
  status: "running",
  update_available: false,
  working_dir: "/srv/demo",
};

const noopProject = (_project: ComposeProject) => {};
const noopProjectName = (_projectName: string) => {};

function Card({ value }: { value: ComposeProject }) {
  return (
    <ComposeStackCard
      onDelete={noopProject}
      onRestart={noopProjectName}
      onStart={noopProjectName}
      onStop={noopProjectName}
      project={value}
    />
  );
}

describe("ComposeStackCard", () => {
  it("ignores container-detail churn but updates for displayed summaries", () => {
    const view = render(<Card value={project} />);

    expect(frostedCardRender).toHaveBeenCalledTimes(1);

    view.rerender(
      <Card
        value={{
          ...project,
          containers: [{ ...container, Status: "Up 2 minutes" }],
        }}
      />,
    );

    expect(frostedCardRender).toHaveBeenCalledTimes(1);

    view.rerender(<Card value={{ ...project, status: "stopped" }} />);

    expect(screen.getByText("stopped")).toBeInTheDocument();
    expect(frostedCardRender).toHaveBeenCalledTimes(2);
  });
});
