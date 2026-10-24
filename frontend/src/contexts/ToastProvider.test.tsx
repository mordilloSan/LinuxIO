import { describe, expect, it, vi } from "vitest";

vi.mock("sonner", () => ({
  Toaster: () => <div data-testid="toaster" />,
}));

const { ToastProvider } = await import("@/contexts/ToastProvider");
const { render, screen } = await import("@/test/render");

describe("ToastProvider", () => {
  it("renders its children next to the toaster", () => {
    render(
      <ToastProvider>
        <p>child</p>
      </ToastProvider>,
    );

    expect(screen.getByText("child")).toBeInTheDocument();
    expect(screen.getByTestId("toaster")).toBeInTheDocument();
  });
});
