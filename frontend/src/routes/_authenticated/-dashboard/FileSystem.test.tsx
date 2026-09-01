import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import FsInfoCard from "./FileSystem";

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({
      data: Array.from({ length: 6 }, (_, index) => ({
        device: `/dev/test${index}`,
        free: 500,
        fstype: "ext4",
        mountpoint: `/mnt/test${index}`,
        total: 1000,
        used: 500,
        usedPercent: 50,
      })),
    }),
  };
});

describe("FsInfoCard", () => {
  it("caps overflowing filesystem rows in the custom scrollport", () => {
    render(<FsInfoCard />);

    const scrollport = screen.getByRole("region", { name: "File systems" });
    expect(scrollport).toHaveStyle({
      maxHeight: "110px",
      overflowX: "hidden",
      overflowY: "auto",
    });
    expect(screen.getByText("/mnt/test5")).toBeInTheDocument();
  });
});
