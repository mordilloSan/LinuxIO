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
  it("shows the container count in the header with an explanatory tooltip", async () => {
    const { user } = render(
      <DockerImageCard image={image} onSelect={vi.fn()} selected={false} />,
    );

    const usageChip = screen.getByText(String(image.containers), {
      selector: ".app-chip",
    });
    expect(screen.queryByText(`Used by ${image.containers}`)).toBeNull();

    await user.hover(usageChip);
    expect(await screen.findByRole("tooltip")).toHaveTextContent(
      "Used by 1 container",
    );
  });

  it("selects on double click and keeps the dashboard-style accent", async () => {
    const onSelect = vi.fn();
    const { container, rerender, user } = render(
      <DockerImageCard image={image} onSelect={onSelect} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Select image ${image.repo}`,
    });
    expect(card).toBe(container.firstElementChild);
    expect(card).toHaveAttribute("aria-pressed", "false");
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
    expect(onSelect).not.toHaveBeenCalled();

    await user.dblClick(card);
    expect(onSelect).toHaveBeenCalledWith(true);
    expect(card).not.toHaveFocus();

    rerender(<DockerImageCard image={image} onSelect={onSelect} selected />);
    const selectedCard = screen.getByRole("button", {
      name: `Deselect image ${image.repo}`,
    });
    expect(selectedCard).toHaveAttribute("aria-pressed", "true");
    expect(selectedCard.firstElementChild).toHaveClass(
      "docker-resource-card--selected",
    );

    selectedCard.focus();
    await user.keyboard("{Enter}");
    expect(onSelect).toHaveBeenLastCalledWith(false);
    expect(selectedCard).toHaveFocus();
  });
});
