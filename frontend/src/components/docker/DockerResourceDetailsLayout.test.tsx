import { expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import DockerResourceDetailsLayout from "./DockerResourceDetailsLayout";

it("renders a responsive resource summary and closable details panel", async () => {
  const onClose = vi.fn();
  const { user } = render(
    <DockerResourceDetailsLayout
      onClose={onClose}
      resourceLabel="image"
      subtitle="Docker image details"
      summary={<div>Image summary</div>}
      title="example/image:latest"
    >
      <div>Image metadata</div>
    </DockerResourceDetailsLayout>,
  );

  expect(screen.getByText("Image summary")).toBeVisible();
  expect(screen.getByText("example/image:latest")).toBeVisible();
  expect(screen.getByText("Image metadata")).toBeVisible();

  await user.click(screen.getByRole("button", { name: "Close image details" }));
  expect(onClose).toHaveBeenCalledOnce();
});
