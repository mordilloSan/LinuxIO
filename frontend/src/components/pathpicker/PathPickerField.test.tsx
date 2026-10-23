import { useState, type ComponentProps } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen, waitFor } from "@/test/render";

import PathPickerField from "./PathPickerField";

const mocks = vi.hoisted(() => ({
  directoryChildren: vi.fn(),
  listDirectory: vi.fn(),
}));

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();

  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      filebrowser: {
        ...actual.linuxio.filebrowser,
        directory_children: Object.assign(
          (request: { path: string; includeFiles: boolean }) => ({
            queryKey: ["test", "directory-children", request],
            queryFn: () => mocks.directoryChildren(request),
          }),
          { route: actual.linuxio.filebrowser.directory_children.route },
        ),
        list_directory: Object.assign(
          (request: { path: string }) => {
            mocks.listDirectory(request);
            throw new Error("PathPickerField must not fetch a full listing");
          },
          { route: actual.linuxio.filebrowser.list_directory.route },
        ),
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
    vi.clearAllMocks();
    mocks.directoryChildren.mockImplementation(
      ({ path, includeFiles }: { path: string; includeFiles: boolean }) => {
        if (path === "/") {
          return Promise.resolve({ folders: ["media"], files: [] });
        }
        if (path === "/media") {
          return Promise.resolve({
            files: includeFiles ? ["debian.iso", "notes.txt"] : [],
            folders: ["nested"],
          });
        }
        return Promise.resolve({ files: [], folders: [] });
      },
    );
  });

  it("selects a folder and closes after expanding with the chevron", async () => {
    const { user } = render(<PickerHarness />);

    await user.click(screen.getByLabelText("Directory Path"));
    await user.click(screen.getByRole("button", { name: "Expand /" }));
    await user.click(await screen.findByText("media"));

    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/",
      includeFiles: false,
    });
    expect(mocks.listDirectory).not.toHaveBeenCalled();
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

    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/",
      includeFiles: true,
    });
    expect(mocks.directoryChildren).toHaveBeenCalledWith({
      path: "/media",
      includeFiles: true,
    });
    expect(mocks.listDirectory).not.toHaveBeenCalled();
    expect(screen.queryByText("notes.txt")).not.toBeInTheDocument();
    expect(screen.getByLabelText("ISO path")).toHaveValue("/media/debian.iso");
    expect(screen.queryByRole("tree")).not.toBeInTheDocument();
  });

  it("clears the loading state and stays collapsed when expanding fails", async () => {
    mocks.directoryChildren.mockRejectedValueOnce(
      new Error("resource unavailable"),
    );
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
    let resolveChildren: (resource: unknown) => void = () => undefined;
    const pendingChildren = new Promise((resolve) => {
      resolveChildren = resolve;
    });
    mocks.directoryChildren.mockReturnValueOnce(pendingChildren);
    const { user } = render(<PickerHarness selectableTypes={[]} />);

    await user.click(screen.getByLabelText("Directory Path"));
    const root = screen.getByRole("treeitem", { name: "/" });
    await user.click(root);
    await user.click(root);

    expect(mocks.directoryChildren).toHaveBeenCalledTimes(1);

    resolveChildren({ folders: ["media"], files: [] });
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
