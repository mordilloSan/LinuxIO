import { expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import ResourceDetailsLayout from "./ResourceDetailsLayout";

it("renders a responsive resource summary and closable details panel", async () => {
  const onClose = vi.fn();
  const { user } = render(
    <ResourceDetailsLayout
      onClose={onClose}
      resourceLabel="image"
      subtitle="Docker image details"
      summary={<div>Image summary</div>}
      below={<div>Image history</div>}
      title="example/image:latest"
    >
      <div>Image metadata</div>
    </ResourceDetailsLayout>,
  );

  expect(screen.getByText("Image summary")).toBeInTheDocument();
  expect(screen.getByText("example/image:latest")).toBeInTheDocument();
  expect(screen.getByText("Image metadata")).toBeInTheDocument();
  expect(screen.getByText("Image history")).toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: "Close image details" }));
  expect(onClose).toHaveBeenCalledOnce();
});
