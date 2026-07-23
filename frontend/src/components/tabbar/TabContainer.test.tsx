import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { renderWithTanStackRouter } from "@/test/render";

import TabContainer from "./TabContainer";

const tabs = [
  { value: "users", label: "Users", component: <div>Users content</div> },
  {
    value: "groups",
    label: "Groups",
    component: <div>Groups content</div>,
  },
];

describe("TabContainer", () => {
  it("only mounts the active tab while switching", async () => {
    const { user } = renderWithTanStackRouter(
      <TabContainer defaultTab="users" tabs={tabs} urlParam="accountsTab" />,
    );

    expect(await screen.findByText("Users content")).toBeInTheDocument();
    expect(screen.queryByText("Groups content")).not.toBeInTheDocument();

    await user.click(await screen.findByRole("tab", { name: "Groups" }));

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(screen.getByText("Groups content")).toBeInTheDocument();
  });

  it("mounts a tab selected in the URL", async () => {
    renderWithTanStackRouter(
      <TabContainer defaultTab="users" tabs={tabs} urlParam="accountsTab" />,
      {
        tanstackRouter: {
          initialEntries: ["/accounts?accountsTab=groups"],
        },
      },
    );

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(await screen.findByText("Groups content")).toBeInTheDocument();
  });
});
