import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { render } from "@/test/render";

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
    const { user } = render(
      <TabContainer
        defaultTab="users"
        tabs={tabs}
        urlParam="accountsTab"
      />,
    );

    expect(screen.getByText("Users content")).toBeInTheDocument();
    expect(screen.queryByText("Groups content")).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: "Groups" }));

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(screen.getByText("Groups content")).toBeInTheDocument();
  });

  it("mounts a tab selected in the URL", () => {
    render(
      <TabContainer
        defaultTab="users"
        tabs={tabs}
        urlParam="accountsTab"
      />,
      {
        memoryRouter: {
          initialEntries: ["/accounts?accountsTab=groups"],
        },
      },
    );

    expect(screen.queryByText("Users content")).not.toBeInTheDocument();
    expect(screen.getByText("Groups content")).toBeInTheDocument();
  });
});
