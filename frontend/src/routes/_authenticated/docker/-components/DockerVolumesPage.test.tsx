import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerVolumesPage from "./DockerVolumesPage";

const mocks = vi.hoisted(() => ({ systemPrune: vi.fn() }));

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

vi.mock("./VolumeList", () => ({ default: () => <div>Volume list</div> }));

describe("DockerVolumesPage", () => {
  beforeEach(() => mocks.systemPrune.mockReset());

  it("prunes only unused volumes by default", async () => {
    const { user } = render(<DockerVolumesPage />);
    await user.click(screen.getByRole("button", { name: "Prune All" }));
    expect(
      screen.getByRole("button", { name: "Prune Selected (1)" }),
    ).toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: "Prune Selected (1)" }),
    );
    expect(mocks.systemPrune).toHaveBeenCalledWith({
      buildCache: false,
      containers: false,
      images: false,
      networks: false,
      volumes: true,
    });
  });
});
