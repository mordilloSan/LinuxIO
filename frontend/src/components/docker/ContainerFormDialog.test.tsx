import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { type ContainerInspectInfo, type DockerNetwork, linuxio } from "@/api";
import * as core from "@/api/linuxio-core";
import { render, screen, waitFor } from "@/test/render";

import ContainerFormDialog from "./ContainerFormDialog";

const networks: DockerNetwork[] = [
  {
    Attachable: false,
    ConfigOnly: false,
    Driver: "bridge",
    Id: "bridge-id",
    Ingress: false,
    Name: "bridge",
    Scope: "local",
  },
];

const inspect: ContainerInspectInfo = {
  created: "2026-09-03T08:00:00Z",
  environment: [{ name: "APP_ENV", value: "production" }],
  id: "container-id",
  image: "example:latest",
  imageId: "sha256:image",
  name: "example",
  networks: {
    bridge: {
      Aliases: ["example"],
      Gateway: "172.17.0.1",
      IPAddress: "172.17.0.2",
    },
  },
  restartCount: 0,
  restartPolicy: { maximumRetryCount: 0, name: "no" },
  state: {
    dead: false,
    error: "",
    exitCode: 0,
    finishedAt: "",
    oomKilled: false,
    paused: false,
    restarting: false,
    running: true,
    startedAt: "2026-09-03T08:00:01Z",
    status: "running",
  },
  user: "",
  workingDirectory: "",
};

const seededClient = (container?: ContainerInspectInfo) => {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false, staleTime: Infinity },
    },
  });
  queryClient.setQueryData(linuxio.docker.list_images.queryKey, [
    {
      Created: 0,
      Id: "image-id",
      RepoTags: ["example:latest"],
      Size: 1,
    },
  ]);
  queryClient.setQueryData(linuxio.docker.list_networks.queryKey, networks);
  queryClient.setQueryData(linuxio.docker.list_volumes.queryKey, []);
  if (container) {
    queryClient.setQueryData(
      linuxio.docker.inspect_container({ containerId: container.id }).queryKey,
      container,
    );
  }
  return queryClient;
};

describe("ContainerFormDialog", () => {
  it("creates from the shared form without starting by default", async () => {
    const request = vi
      .spyOn(core, "request")
      .mockImplementation(async (_handler, action) => {
        if (action === "create_container") {
          return { containerId: "created-id", name: "web" };
        }
        if (action === "list_networks") return networks;
        return [];
      });
    const onClose = vi.fn();
    const { user } = render(
      <ContainerFormDialog mode="create" onClose={onClose} open />,
      { queryClient: seededClient() },
    );

    await waitFor(() =>
      expect(screen.getByRole("heading", { name: "Basics" })).toBeVisible(),
    );
    expect(
      screen.getByRole("button", { name: "Command and entrypoint" }),
    ).toHaveAttribute("aria-expanded", "false");
    expect(
      screen.getByRole("button", { name: "Networks and aliases" }),
    ).toHaveAttribute("aria-expanded", "true");

    await user.type(screen.getByRole("textbox", { name: /^Name/ }), "web");
    await user.type(
      screen.getByRole("combobox", { name: /^Image/ }),
      "example:latest",
    );
    await user.click(screen.getByRole("button", { name: "Create container" }));

    await waitFor(() =>
      expect(request).toHaveBeenCalledWith(
        "docker",
        "create_container",
        {
          configuration: expect.objectContaining({
            image: "example:latest",
            name: "web",
            networks: [{ aliases: [], name: "bridge" }],
          }),
          start: false,
        },
        { retryPolicy: "none" },
      ),
    );
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("reviews standalone changes before recreation", async () => {
    const { user } = render(
      <ContainerFormDialog
        containerId={inspect.id}
        mode="edit"
        onClose={() => {}}
        open
      />,
      { queryClient: seededClient(inspect) },
    );

    expect(
      screen.getByRole("button", { name: "Environment variables" }),
    ).toHaveAttribute("aria-expanded", "true");
    const name = screen.getByRole("textbox", { name: /^Name/ });
    await user.clear(name);
    await user.type(name, "renamed-example");
    await user.click(screen.getByRole("button", { name: "Review changes" }));

    const review = screen.getByRole("dialog", {
      name: "Review changes to example",
    });
    expect(review).toContainElement(screen.getByText("Basics"));
    expect(review).toHaveTextContent("briefly unavailable");
    expect(review).toHaveTextContent("restored automatically");

    await user.click(screen.getByRole("button", { name: "Back" }));
    expect(screen.getByRole("textbox", { name: /^Name/ })).toHaveValue(
      "renamed-example",
    );
  });
});
