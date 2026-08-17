import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import ImageList from "./ImageList";

const mocks = vi.hoisted(() => ({
  images: [
    {
      Containers: 0,
      Created: 1_722_211_257,
      Id: "sha256:10a08318f473",
      Labels: { maintainer: "LinuxServer.io" },
      RepoDigests: ["example/image@sha256:digest"],
      RepoTags: ["example/image:latest"],
      Size: 1024 * 1024,
    },
  ],
  navigate: vi.fn(),
  search: {} as { image?: string },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useSuspenseQuery: () => ({ data: mocks.images }),
  };
});

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@tanstack/react-router")>();
  return {
    ...actual,
    getRouteApi: () => ({
      useNavigate: () => mocks.navigate,
      useSearch: () => mocks.search,
    }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    useCallMutation: () => ({ isPending: false, mutateAsync: vi.fn() }),
  };
});

describe("ImageList focused details", () => {
  beforeEach(() => {
    mocks.search = {};
    mocks.navigate.mockReset();
    mocks.navigate.mockImplementation(
      ({ search }: { search: (current: { image?: string }) => object }) => {
        mocks.search = search(mocks.search);
        return Promise.resolve();
      },
    );
  });

  it("opens rich details on one click and closes only route focus", async () => {
    const { rerender, user } = render(<ImageList viewMode="card" />);

    await user.click(
      screen.getByRole("button", {
        name: "Open image example/image details",
      }),
    );
    rerender(<ImageList viewMode="card" />);

    expect(
      await screen.findByRole("button", { name: "Close image details" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Full Image ID:")).toBeInTheDocument();
    expect(screen.getByText("example/image@sha256:digest")).toBeInTheDocument();
    expect(
      screen.getByRole("button", {
        name: "Delete image example/image",
      }),
    ).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: "Close image details" }),
    );
    expect(mocks.search.image).toBeUndefined();
  });
});
