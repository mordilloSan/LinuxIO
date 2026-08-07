import { useState, type ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import PathPickerField from "./PathPickerField";

const mocks = vi.hoisted(() => ({
  resourceGet: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();

  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        resource_get: Object.assign(mocks.resourceGet, {
          useFetcher: () => (request: { path: string }) =>
            mocks.resourceGet(request),
        }),
      },
    },
  };
});

function PickerHarness(
  props: Omit<ComponentProps<typeof PathPickerField>, "onChange" | "value">,
) {
  const [value, setValue] = useState("");

  return <PathPickerField {...props} onChange={setValue} value={value} />;
}

describe("PathPickerField", () => {
  beforeEach(() => {
    mocks.resourceGet.mockImplementation(({ path }: { path: string }) => {
      if (path === "/") {
        return Promise.resolve({
          folders: [{ name: "media", type: "directory" }],
        });
      }
      if (path === "/media/") {
        return Promise.resolve({
          files: [
            { name: "debian.iso", type: "file" },
            { name: "notes.txt", type: "file" },
          ],
          folders: [{ name: "nested", type: "directory" }],
        });
      }
      return Promise.resolve({ files: [], folders: [] });
    });
  });

  it("selects a folder and closes after expanding with the chevron", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(screen.getByLabelText("Directory Path"));
    await user.click(screen.getByRole("button", { name: "Expand /" }));
    await user.click(await screen.findByText("media"));

    expect(screen.getByLabelText("Directory Path")).toHaveValue("/media/");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("selects a filtered file and closes while folders stay navigable", async () => {
    const { user } = render(
      <PickerHarness
        browseLabel="Browse ISO files"
        editable
        fileFilter={(path) => path.toLowerCase().endsWith(".iso")}
        includeFiles
        label="ISO path"
        selectableTypes={["file"]}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Browse ISO files" }));
    await user.click(await screen.findByText("/"));
    await user.click(await screen.findByText("media"));
    await user.click(await screen.findByText("debian.iso"));

    expect(mocks.resourceGet).toHaveBeenCalledWith({ path: "/" });
    expect(mocks.resourceGet).toHaveBeenCalledWith({ path: "/media/" });
    expect(screen.queryByText("notes.txt")).not.toBeInTheDocument();
    expect(screen.getByLabelText("ISO path")).toHaveValue("/media/debian.iso");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("clears the loading state and stays collapsed when expanding fails", async () => {
    mocks.resourceGet.mockRejectedValueOnce(new Error("resource unavailable"));
    const { user } = render(<PickerHarness />);

    await user.click(screen.getByLabelText("Directory Path"));
    const expandButton = screen.getByRole("button", { name: "Expand /" });
    await user.click(expandButton);

    await waitFor(() => expect(expandButton).not.toBeDisabled());
    expect(screen.getByRole("treeitem", { name: "/" })).toHaveAttribute(
      "aria-expanded",
      "false",
    );
  });

  it("ignores repeated row activation while loading", async () => {
    let resolveResource: (resource: unknown) => void = () => undefined;
    const pendingResource = new Promise((resolve) => {
      resolveResource = resolve;
    });
    mocks.resourceGet.mockReturnValueOnce(pendingResource);
    const { user } = render(<PickerHarness selectableTypes={[]} />);

    await user.click(screen.getByLabelText("Directory Path"));
    const root = screen.getByRole("treeitem", { name: "/" });
    await user.click(root);
    await user.click(root);

    expect(mocks.resourceGet).toHaveBeenCalledTimes(1);

    resolveResource({ folders: [{ name: "media", type: "directory" }] });
    await waitFor(() =>
      expect(screen.getByRole("treeitem", { name: "/" })).toHaveAttribute(
        "aria-expanded",
        "true",
      ),
    );
  });

  it("closes with Escape", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(screen.getByLabelText("Directory Path"));
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.keyboard("{Escape}");

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("closes when clicking outside", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(screen.getByLabelText("Directory Path"));
    expect(screen.getByRole("tree")).toBeInTheDocument();

    await user.click(document.body);

    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });
});
