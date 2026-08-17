import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerImageCard, { type DockerImageRow } from "./DockerImageCard";

const image: DockerImageRow = {
  containers: 1,
  created: "7/29/2026, 1:00:57 AM",
  id: "sha256:10a08318f473",
  repo: "lscr.io/linuxserver/speedtest-tracker",
  size: "339.13",
  tags: ["latest"],
};

describe("DockerImageCard", () => {
  it("does not present image update status", () => {
    const imageWithUpdateStatus = {
      ...image,
      updateAvailable: true,
      updateCheckState: "uncheckable",
    } as DockerImageRow;

    render(
      <DockerImageCard
        image={imageWithUpdateStatus}
        onOpen={vi.fn()}
        selected={false}
      />,
    );

    expect(screen.queryByText("Update available")).toBeNull();
    expect(screen.queryByText("Cannot check")).toBeNull();
  });

  it("shows only the full image ID", () => {
    render(
      <DockerImageCard image={image} onOpen={vi.fn()} selected={false} />,
    );

    const fullId = screen.getByText(image.id);
    expect(screen.getByText("Full ID:")).toHaveStyle("font-weight: 700");
    expect(fullId).toHaveStyle("font-family: var(--app-font-mono)");
    expect(fullId).toHaveStyle("font-size: 12px");
    expect(fullId.parentElement).toHaveClass("app-typo--nowrap");
    expect(screen.queryByText(/^ID:/)).toBeNull();
  });

  it("shows the container count in the header with an explanatory tooltip", async () => {
    const { user } = render(
      <DockerImageCard image={image} onOpen={vi.fn()} selected={false} />,
    );

    const usageChip = screen.getByText(String(image.containers), {
      selector: ".app-chip__label",
    });
    expect(screen.queryByText(`Used by ${image.containers}`)).toBeNull();

    await user.hover(usageChip);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Used by 1 container",
    );
  });

  it("opens on one click and supports keyboard activation", async () => {
    const onOpen = vi.fn();
    const { container, user } = render(
      <DockerImageCard image={image} onOpen={onOpen} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Open image ${image.repo} details`,
    });
    expect(card.parentElement).toBe(container.firstElementChild);
    expect(card).toHaveClass("selectable-card-button");
    expect(card.firstElementChild).not.toHaveClass(
      "docker-resource-card--selected",
    );
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(
      screen.getByRole("heading", { name: image.repo }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(`${image.size} MB · ${image.created}`),
    ).toBeInTheDocument();

    await user.click(card);
    expect(onOpen).toHaveBeenCalledTimes(1);
    await user.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledTimes(2);
  });

  it("keeps action controls separate from the card opener", async () => {
    const onDelete = vi.fn();
    const { user } = render(
      <DockerImageCard
        actions={<button onClick={onDelete}>Delete</button>}
        image={image}
        selected
      />,
    );

    await user.click(screen.getByRole("button", { name: "Delete" }));
    expect(onDelete).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", {
        name: `Open image ${image.repo} details`,
      }),
    ).toBeNull();
  });
});
