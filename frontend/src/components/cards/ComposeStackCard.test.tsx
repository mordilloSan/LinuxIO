import { describe, expect, it, vi } from "vitest";

import type { ComposeProject, ContainerInfo } from "@/api";

const frostedCardRender = vi.hoisted(() => vi.fn());
const actionIconButtonRender = vi.hoisted(() => vi.fn());

vi.mock("@/components/cards/FrostedCard", () => ({
  default: ({ children }: { children: React.ReactNode }) => {
    frostedCardRender();
    return <section>{children}</section>;
  },
}));

vi.mock("@/components/docker/DockerIcon", () => ({
  default: ({ alt }: { alt: string }) => <span>{alt}</span>,
}));

vi.mock("@/components/ui/AppActionIconButton", () => ({
  default: ({
    icon,
    label,
    onClick,
  }: {
    icon: string;
    label: string;
    onClick: () => void;
  }) => {
    actionIconButtonRender(icon);
    return (
      <button data-icon={icon} onClick={onClick} type="button">
        {label}
      </button>
    );
  },
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

function Card({
  onOpen,
  value,
}: {
  onOpen?: () => void;
  value: ComposeProject;
}) {
  return (
    <ComposeStackCard
      onDelete={noopProject}
      onOpen={onOpen}
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

  it("opens details from the summary and uses the container stop icon", async () => {
    const onOpen = vi.fn();
    const { user } = render(<Card onOpen={onOpen} value={project} />);

    await user.click(
      screen.getByRole("button", { name: "Open stack demo details" }),
    );

    expect(onOpen).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "Stop" })).toHaveAttribute(
      "data-icon",
      "mdi:stop",
    );
  });
});
