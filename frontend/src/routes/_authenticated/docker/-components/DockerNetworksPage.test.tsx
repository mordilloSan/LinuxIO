import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerNetworksPage from "./DockerNetworksPage";

const mocks = vi.hoisted(() => ({ systemPrune: vi.fn() }));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ mutate: mocks.systemPrune, isPending: false }),
    linuxio: { ...actual.linuxio, docker: { ...actual.linuxio.docker, system_prune: { route: "docker.system_prune" } } },
  };
});

vi.mock("./NetworkList", () => ({ default: () => <div>Network list</div> }));

describe("DockerNetworksPage", () => {
  beforeEach(() => mocks.systemPrune.mockReset());

  it("prunes only unused networks by default", async () => {
    const { user } = render(<DockerNetworksPage />);
    await user.click(screen.getByRole("button", { name: "Prune All" }));
    expect(screen.getByRole("button", { name: "Prune Selected (1)" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Prune Selected (1)" }));
    expect(mocks.systemPrune).toHaveBeenCalledWith({
      buildCache: false,
      containers: false,
      images: false,
      networks: true,
      volumes: false,
    });
  });
});
