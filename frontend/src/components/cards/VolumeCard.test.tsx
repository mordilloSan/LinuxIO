import { describe, expect, it, vi } from "vitest";

import type { DockerVolume } from "@/api";
import { render, screen } from "@/test/render";

import VolumeCard from "./VolumeCard";

const volume: DockerVolume = {
  Driver: "local",
  Mountpoint: "/var/lib/docker/volumes/example/_data",
  Name: "example-volume",
  Scope: "local",
};

describe("VolumeCard", () => {
  it("uses double click selection without a checkbox", async () => {
    const onSelect = vi.fn();
    const { rerender, user } = render(
      <VolumeCard volume={volume} onSelect={onSelect} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Select volume ${volume.Name}`,
    });
    expect(card).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(
      screen.getByRole("heading", { name: volume.Name }),
    ).toBeInTheDocument();
    expect(screen.getByText("local · local")).toBeInTheDocument();

    await user.click(card);
    expect(onSelect).not.toHaveBeenCalled();

    await user.dblClick(card);
    expect(onSelect).toHaveBeenCalledWith(true);
    expect(card).not.toHaveFocus();

    rerender(<VolumeCard volume={volume} onSelect={onSelect} selected />);
    const selectedCard = screen.getByRole("button", {
      name: `Deselect volume ${volume.Name}`,
    });
    expect(selectedCard).toHaveAttribute("aria-pressed", "true");

    selectedCard.focus();
    await user.keyboard("{Enter}");
    expect(onSelect).toHaveBeenLastCalledWith(false);
    expect(selectedCard).toHaveFocus();
  });
});
