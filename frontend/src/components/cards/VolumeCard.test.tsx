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
  it("opens on one click and keyboard activation", async () => {
    const onOpen = vi.fn();
    const { user } = render(
      <VolumeCard volume={volume} onOpen={onOpen} selected={false} />,
    );

    const card = screen.getByRole("button", {
      name: `Open volume ${volume.Name} details`,
    });
    expect(screen.queryByRole("checkbox")).toBeNull();
    expect(
      screen.getByRole("heading", { name: volume.Name }),
    ).toBeInTheDocument();
    expect(screen.getByText("local · local")).toBeInTheDocument();

    await user.click(card);
    expect(onOpen).toHaveBeenCalledTimes(1);
    await user.keyboard("{Enter}");
    expect(onOpen).toHaveBeenCalledTimes(2);
  });
});
