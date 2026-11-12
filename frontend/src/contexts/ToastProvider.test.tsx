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

  // An open dialog blurs #root and stacks above everything inside it, so a
  // toast must not live there.
  it("mounts the toaster on the body, outside the app root", () => {
    const { container } = render(
      <ToastProvider>
        <p>child</p>
      </ToastProvider>,
    );

    const toaster = screen.getByTestId("toaster");
    expect(toaster.parentElement).toBe(document.body);
    expect(container).not.toContainElement(toaster);
  });
});
