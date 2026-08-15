import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerImageCard, { type DockerImageRow } from "./DockerImageCard";

const image: DockerImageRow = {
  containers: 1,
  created: "7/29/2026, 1:00:57 AM",
  id: "sha256:10a08318f473",
  repo: "lscr.io/linuxserver/speedtest-tracker",
  shortId: "10a08318f473",
  size: "339.13",
  tags: ["latest"],
};

describe("DockerImageCard", () => {
  it("keeps the dashboard-style accent while its image is selected", async () => {
    const onSelect = vi.fn();
    const { container, rerender, user } = render(
      <DockerImageCard image={image} onSelect={onSelect} selected={false} />,
    );

    expect(container.firstElementChild).toHaveClass("docker-image-card");
    expect(container.firstElementChild).not.toHaveClass(
      "docker-image-card--selected",
    );

    await user.click(screen.getByRole("checkbox"));
    expect(onSelect).toHaveBeenCalledWith(true);

    rerender(<DockerImageCard image={image} onSelect={onSelect} selected />);
    expect(container.firstElementChild).toHaveClass(
      "docker-image-card--selected",
    );
  });
});
