import { beforeEach, describe, expect, it, vi } from "vitest";

import { render, screen } from "@/test/render";

import {
  useDockerUpdateCheck,
  useDockerUpdateStatusRefresh,
} from "./useDockerUpdateCheck";

const mocks = vi.hoisted(() => ({
  checkQuery: vi.fn(),
  mutate: vi.fn(),
  invalidateQueries: vi.fn(),
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
  },
  useCallMutation: vi.fn(),
  useQuery: vi.fn(),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: mocks.useQuery,
    useQueryClient: () => ({ invalidateQueries: mocks.invalidateQueries }),
  };
});

vi.mock("@/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@/api")>();
  return {
    ...actual,
    linuxio: {
      ...actual.linuxio,
      docker: {
        ...actual.linuxio.docker,
        check_updates: {
          ...actual.linuxio.docker.check_updates,
          queryFn: mocks.checkQuery,
        },
      },
    },
    useCallMutation: mocks.useCallMutation,
  };
});

vi.mock("@/hooks/useCapabilities", () => ({
  useCapability: () => ({
    isEnabled: true,
    reason: "",
  }),
}));

vi.mock("@/hooks/useScopedToast", () => ({
  useScopedToast: () => mocks.toast,
}));

function CheckButton() {
  return useDockerUpdateCheck().button;
}

function RefreshProbe() {
  const query = useDockerUpdateStatusRefresh();
  return (
    <output data-testid="refresh-state">{String(query.isFetching)}</output>
  );
}

describe("useDockerUpdateCheck feedback", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.checkQuery.mockResolvedValue({
      checked: 2,
      errors: 0,
      uncheckable: 0,
      updates: 1,
    });
    mocks.useQuery.mockReturnValue({ isFetching: false });
    mocks.useCallMutation.mockImplementation((_endpoint, config) => ({
      isPending: false,
      mutate: (variables: unknown) => {
        mocks.mutate(variables);
        config?.success?.({
          checked: 2,
          errors: 0,
          uncheckable: 0,
          updates: 0,
        });
      },
    }));
  });

  it("refreshes the availability scan on entry and every five minutes", async () => {
    render(<RefreshProbe />);

    const options = mocks.useQuery.mock.calls[0]?.[0];
    expect(options).toEqual(
      expect.objectContaining({
        enabled: true,
        staleTime: 5 * 60 * 1000,
        refetchInterval: 5 * 60 * 1000,
        refetchOnMount: "always",
        refetchOnWindowFocus: true,
        retry: false,
        meta: { silent: true },
      }),
    );

    await options.queryFn({ signal: new AbortController().signal });

    expect(mocks.checkQuery).toHaveBeenCalledOnce();
    expect(mocks.invalidateQueries).toHaveBeenCalledTimes(3);
  });

  it("shows success feedback after a full successful scan", async () => {
    const { user } = render(<CheckButton />);

    await user.click(
      screen.getByRole("button", { name: "Check container updates" }),
    );

    expect(mocks.toast.success).toHaveBeenCalledWith(
      "Checked 2 container(s), found 0 update(s)",
    );
    expect(mocks.toast.warning).not.toHaveBeenCalled();
  });

  it("shows warning feedback with the error count for a partial scan", async () => {
    mocks.useCallMutation.mockImplementation((_endpoint, config) => ({
      isPending: false,
      mutate: () =>
        config?.success?.({
          checked: 3,
          errors: 1,
          uncheckable: 0,
          updates: 1,
        }),
    }));
    const { user } = render(<CheckButton />);

    await user.click(
      screen.getByRole("button", { name: "Check container updates" }),
    );

    expect(mocks.toast.warning).toHaveBeenCalledWith(
      "Checked 3 container(s), 1 check error(s), found 1 update(s)",
    );
    expect(mocks.toast.success).not.toHaveBeenCalled();
  });

  it("preserves the non-error wording for uncheckable-only scans", async () => {
    mocks.useCallMutation.mockImplementation((_endpoint, config) => ({
      isPending: false,
      mutate: () =>
        config?.success?.({
          checked: 2,
          errors: 0,
          uncheckable: 1,
          updates: 0,
        }),
    }));
    const { user } = render(<CheckButton />);

    await user.click(
      screen.getByRole("button", { name: "Check container updates" }),
    );

    expect(mocks.toast.warning).toHaveBeenCalledWith(
      "Checked 2 container(s), 1 cannot be checked, found 0 update(s)",
    );
    expect(mocks.toast.success).not.toHaveBeenCalled();
  });
});
