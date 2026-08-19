import { useState } from "react";

import { RoutedTabSearch } from "@/components/tabbar";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";

export default function UsersPage() {
  const [search, setSearch] = useState("");

  return (
    <>
      <RoutedTabSearch active={search !== ""}>
        <AppHeaderSearch
          aria-label="Search users"
          clearOnDocumentEscape
          onChange={setSearch}
          value={search}
        />
      </RoutedTabSearch>
      <h1>Users route content</h1>
    </>
  );
}
