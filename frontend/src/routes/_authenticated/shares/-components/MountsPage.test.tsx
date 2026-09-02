import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({ reason: "", status: "available" }),
}));

vi.mock("@/hooks/useViewMode", () => ({
  useViewMode: () => ["table", vi.fn()],
}));

vi.mock("./CIFSMounts", () => ({
  default: () => <div>SMB mounts</div>,
}));

vi.mock("./NFSMounts", () => ({
  default: () => <div>NFS mounts</div>,
}));

const { default: MountsPage } = await import("./MountsPage");

describe("MountsPage", () => {
  it("keeps both mount sections together in a contained scroller", () => {
    render(<MountsPage />);

    const nfsSection = screen.getByText("NFS").parentElement;
    const mountsContent = nfsSection?.parentElement;

    expect(mountsContent).toHaveStyle({
      display: "flex",
      flexDirection: "column",
      height: "100%",
      minHeight: "0",
      minWidth: "0",
      overflow: "auto",
    });
  });
});
