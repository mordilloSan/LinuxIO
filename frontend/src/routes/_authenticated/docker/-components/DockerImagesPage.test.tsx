import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import DockerImagesPage from "./DockerImagesPage";

const mocks = vi.hoisted(() => ({
  systemPrune: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ mutate: mocks.systemPrune, isPending: false }),
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        system_prune: { route: "docker.system_prune" },
      },
    },
  };
});

vi.mock("./ImageList", () => ({
  default: () => <div>Image list</div>,
}));

describe("DockerImagesPage", () => {
  beforeEach(() => {
    mocks.systemPrune.mockReset();
  });

  it("offers the dashboard prune action for unused Docker resources", async () => {
    const { user } = render(<DockerImagesPage />);

    await user.click(screen.getByRole("button", { name: "Prune All" }));
    // The dialog fades in from opacity 0; wait for the first animation
    // frame instead of racing it.
    await waitFor(() =>
      expect(
        screen.getByRole("heading", { name: "Prune System" }),
      ).toBeVisible(),
    );

    await user.click(
      screen.getByRole("button", { name: "Prune Selected (1)" }),
    );

    expect(mocks.systemPrune).toHaveBeenCalledWith({
      buildCache: false,
      containers: false,
      images: true,
      networks: false,
      volumes: false,
    });
  });
});
