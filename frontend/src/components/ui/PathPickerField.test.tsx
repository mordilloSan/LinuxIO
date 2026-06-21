import React, { useState } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

import PathPickerField from "./PathPickerField";

import { render, screen } from "@/test/render";

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
        resource_get: mocks.resourceGet,
      },
    },
  };
});

function PickerHarness(
  props: Omit<
    React.ComponentProps<typeof PathPickerField>,
    "onChange" | "value"
  >,
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
