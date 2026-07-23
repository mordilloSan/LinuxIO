import { useLocation, useNavigate } from "react-router-dom";
import { describe, expect, it } from "vitest";

import { useUpdateNavigationGuard } from "@/contexts/UpdateContext";
import { render, screen } from "@/test/render";

function Harness({ isUpdating }: { isUpdating: boolean }) {
  useUpdateNavigationGuard(isUpdating);
  const location = useLocation();
  const navigate = useNavigate();
  return (
    <>
      <span data-testid="loc">
        {`${location.pathname}${location.search}${location.hash}`}
      </span>
      <button onClick={() => navigate("/docker?dockerTab=images")}>go</button>
    </>
  );
}

describe("useUpdateNavigationGuard", () => {
  it("snaps back to the full pre-update URL (path + search + hash) mid-update", async () => {
    const { user, rerender } = render(<Harness isUpdating={false} />, {
      memoryRouter: { initialEntries: ["/network?iface=eth0#status"] },
    });
    expect(screen.getByTestId("loc").textContent).toBe(
      "/network?iface=eth0#status",
    );

    // Update starts, then the user attempts to navigate away.
    rerender(<Harness isUpdating={true} />);
    await user.click(screen.getByRole("button", { name: "go" }));

    expect(screen.getByTestId("loc").textContent).toBe(
      "/network?iface=eth0#status",
    );
  });

  it("allows navigation when no update is in progress", async () => {
    const { user } = render(<Harness isUpdating={false} />, {
      memoryRouter: { initialEntries: ["/network?iface=eth0#status"] },
    });

    await user.click(screen.getByRole("button", { name: "go" }));

    expect(screen.getByTestId("loc").textContent).toBe(
      "/docker?dockerTab=images",
    );
  });
});
