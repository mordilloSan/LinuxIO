import { describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  result: {} as {
    data?: { changelog: string; homepage?: string };
    isError?: boolean;
    isLoading?: boolean;
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return { ...actual, useQuery: () => mocks.result };
});

const { default: UpdateChangelog } = await import("./UpdateChangelog");
const { render, screen } = await import("@/test/render");

describe("UpdateChangelog", () => {
  it("shows the changelog PackageKit sends", () => {
    mocks.result = { data: { changelog: "Fixed the thing" } };
    render(<UpdateChangelog packageId="demo;1.0;amd64;repo" />);

    expect(screen.getByText("Fixed the thing")).toBeInTheDocument();
    expect(screen.queryByRole("link")).not.toBeInTheDocument();
  });

  it("links to the project when the update has no changelog", () => {
    mocks.result = {
      data: { changelog: "", homepage: "https://www.docker.com" },
    };
    render(<UpdateChangelog packageId="demo;1.0;amd64;repo" />);

    expect(
      screen.getByText("No changelog is available for this update"),
    ).toBeInTheDocument();
    const link = screen.getByRole("link", { name: "Project page" });
    expect(link).toHaveAttribute("href", "https://www.docker.com");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("says when the changelog could not be loaded", () => {
    mocks.result = { isError: true };
    render(<UpdateChangelog packageId="demo;1.0;amd64;repo" />);

    expect(screen.getByText("Failed to load changelog")).toBeInTheDocument();
  });
});
