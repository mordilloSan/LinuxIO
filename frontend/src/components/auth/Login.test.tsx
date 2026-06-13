import { Route, Routes } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import Login from "@/components/auth/Login";
import { render, screen, waitFor } from "@/test/render";

function getUsername(container: HTMLElement) {
  return container.querySelector(
    'input[autocomplete="username"]',
  ) as HTMLInputElement;
}

function getPassword(container: HTMLElement) {
  return container.querySelector(
    'input[autocomplete="current-password"]',
  ) as HTMLInputElement;
}

describe("Login form", () => {
  it("validates required credentials", async () => {
    const { user } = render(<Login />);

    await user.click(screen.getByRole("button", { name: /sign in/i }));

    expect(
      await screen.findByText("Username and password are required."),
    ).toBeInTheDocument();
  });

  it("toggles password visibility", async () => {
    const { container, user } = render(<Login />);
    const password = getPassword(container);

    expect(password.type).toBe("password");
    await user.click(screen.getAllByRole("button")[0]);
    expect(password.type).toBe("text");
  });

  it("signs in and redirects to the requested page", async () => {
    const signIn = vi.fn().mockResolvedValue(undefined);
    const { container, user } = render(
      <Routes>
        <Route path="/sign-in" element={<Login />} />
        <Route path="/target" element={<div>Target page</div>} />
      </Routes>,
      {
        auth: { signIn },
        memoryRouter: {
          initialEntries: ["/sign-in?redirect=/target"],
        },
      },
    );

    await user.type(getUsername(container), "miguel");
    await user.type(getPassword(container), "secret");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    await waitFor(() =>
      expect(signIn).toHaveBeenCalledWith("miguel", "secret"),
    );
    expect(await screen.findByText("Target page")).toBeInTheDocument();
  });

  it("shows failed sign-in errors", async () => {
    const signIn = vi.fn().mockRejectedValue(new Error("Nope"));
    const { container, user } = render(<Login />, { auth: { signIn } });

    await user.type(getUsername(container), "miguel");
    await user.type(getPassword(container), "bad");
    await user.click(screen.getByRole("button", { name: /sign in/i }));

    expect(await screen.findByText("Nope")).toBeInTheDocument();
  });
});
