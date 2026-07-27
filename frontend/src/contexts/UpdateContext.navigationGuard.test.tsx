import { useLocation, useNavigate } from "@tanstack/react-router";
import { describe, expect, it } from "vitest";

import { useUpdateNavigationGuard } from "@/contexts/UpdateContext";
import { renderWithTanStackRouter, screen, waitFor } from "@/test/render";

function Harness({ isUpdating }: { isUpdating: boolean }) {
  useUpdateNavigationGuard(isUpdating);
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <span data-testid="loc">
        {[location.pathname, location.searchStr, location.hash].join("|")}
      </span>
      <button onClick={() => navigate({ to: "/docker/images" })}>go</button>
    </>
  );
}

describe("useUpdateNavigationGuard", () => {
  it("blocks navigation before the URL changes during an update", async () => {
    const { user, rerender } = renderWithTanStackRouter(
      <Harness isUpdating={false} />,
      {
        tanstackRouter: { initialEntries: ["/network?iface=eth0#status"] },
      },
    );
    expect((await screen.findByTestId("loc")).textContent).toBe(
      "/network|?iface=eth0|status",
    );

    // Update starts, then the user attempts to navigate away.
    rerender(<Harness isUpdating={true} />);
    await user.click(screen.getByRole("button", { name: "go" }));

    await waitFor(() =>
      expect(screen.getByTestId("loc").textContent).toBe(
        "/network|?iface=eth0|status",
      ),
    );
  });

  it("allows navigation when no update is in progress", async () => {
    const { user } = renderWithTanStackRouter(<Harness isUpdating={false} />, {
      tanstackRouter: { initialEntries: ["/network?iface=eth0#status"] },
    });

    await user.click(await screen.findByRole("button", { name: "go" }));

    await waitFor(() =>
      expect(screen.getByTestId("loc").textContent).toBe("/docker/images||"),
    );
  });
});
