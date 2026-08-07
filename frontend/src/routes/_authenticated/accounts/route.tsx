import { createFileRoute } from "@tanstack/react-router";

import { makeTabLayout } from "@/components/tabbar";
import { UsersIcon } from "@/icons/svg";

import { ACCOUNTS_TABS } from "./-components/accountsTabs";

const AccountsLayout = makeTabLayout(ACCOUNTS_TABS);

export const Route = createFileRoute("/_authenticated/accounts")({
  component: AccountsLayout,
  staticData: {
    navigation: {
      icon: UsersIcon,
      position: 60,
      title: "Accounts",
    },
  },
});
