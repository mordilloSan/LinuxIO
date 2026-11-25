import { useQuery } from "@tanstack/react-query";

import { linuxio, type SambaUser } from "@/api";
import useAuth from "@/hooks/useAuth";
import { useCapability } from "@/hooks/useCapabilities";

/** The checkbox that switches it on, in Create user and Change password. */
export const SMB_SIGN_IN_LABEL = "Can open shared folders from Windows and Mac";

/**
 * Who can sign in to shared folders from Windows and a Mac. Samba keeps a
 * password of its own for each such user, and only an administrator can
 * read its list: `offered` is false for anyone else and without Samba, and
 * `users` is undefined until the list has arrived.
 */
export const useSambaSignIn = (
  enabled = true,
): { offered: boolean; users: SambaUser[] | undefined } => {
  const { privileged } = useAuth();
  const { isEnabled } = useCapability("sambaServerAvailable");
  const offered = privileged && isEnabled;
  const { data: users } = useQuery({
    ...linuxio.shares.list_samba_users,
    enabled: offered && enabled,
  });
  return { offered, users };
};
