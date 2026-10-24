import { describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import InfoRow, { DetailRow } from "./InfoRow";

describe("InfoRow", () => {
  it("renders the inline row by default", () => {
    const { container } = render(<InfoRow label="Mode">0644</InfoRow>);

    expect(container.querySelector(".info-row")).toBeInTheDocument();
    expect(screen.getByText("Mode")).toBeInTheDocument();
    expect(screen.getByText("0644")).toBeInTheDocument();
  });

  it("renders the detail variant with its split and border modifiers", () => {
    const { container } = render(
      <DetailRow label="State" noBorder split>
        running
      </DetailRow>,
    );

    const row = container.querySelector(".svc-detail-row");
    expect(row).toHaveClass(
      "info-row-detail",
      "info-row-detail--split",
      "info-row-detail--no-border",
    );
    expect(screen.getByText("running")).toBeInTheDocument();
  });

  it("renders list rows as an edit button when onEdit is set", async () => {
    const onEdit = vi.fn();
    const { user } = render(
      <InfoRow label="Hostname" onEdit={onEdit} variant="list">
        box
      </InfoRow>,
    );

    await user.click(screen.getByRole("button", { name: "Edit Hostname" }));

    expect(onEdit).toHaveBeenCalledTimes(1);
  });

  it("hands raw list values their own layout", () => {
    render(
      <InfoRow label="Tags" raw variant="list">
        <span data-testid="chips">chips</span>
      </InfoRow>,
    );

    expect(screen.getByTestId("chips").parentElement).toHaveClass(
      "info-row__value",
    );
  });
});
