// pam_linuxio_probe.so - hermetic test PAM module for the Tier-1 launcher
// suite (make test-auth-pam). It is never installed on a host; the test
// Makefile compiles it into a throwaway directory and references it from a
// pam_wrapper service file.
//
// Behavior is driven by two environment variables read at call time (the
// module runs inside the test process, so the test can change them between
// scenarios):
//   LINUXIO_PAM_PROBE_TRACE   - file the module appends one line per PAM
//                               entry-point call to
//   LINUXIO_PAM_PROBE_CONTROL - key=value file selecting failure injection
//                               and PAM_USER remapping
//
// Control keys (all optional):
//   remap_from=<name>  remap_to=<name>   during authenticate, if PAM_USER
//                                        equals remap_from it is replaced
//                                        with remap_to (canonicalization)
//   fail_auth=<rc>                       pam_sm_authenticate returns rc
//   fail_acct=<rc>                       pam_sm_acct_mgmt returns rc
//   fail_setcred_establish=<rc>          pam_sm_setcred(ESTABLISH) returns rc
//   fail_setcred_reinit=<rc>             pam_sm_setcred(REINITIALIZE) returns rc
//   fail_open_session=<rc>               pam_sm_open_session returns rc
// where <rc> is one of: auth_err, new_authtok_reqd, acct_expired, cred_err,
// session_err, perm_denied, maxtries, user_unknown.
#include <security/pam_modules.h>
#include <security/pam_appl.h>
#include <fcntl.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define CONTROL_MAX 4096

struct probe_control
{
  char remap_from[128];
  char remap_to[128];
  int fail_auth;
  int fail_acct;
  int fail_setcred_establish;
  int fail_setcred_reinit;
  int fail_open_session;
};

static int rc_from_name(const char *name)
{
  static const struct
  {
    const char *name;
    int rc;
  } map[] = {
      {"auth_err", PAM_AUTH_ERR},
      {"new_authtok_reqd", PAM_NEW_AUTHTOK_REQD},
      {"acct_expired", PAM_ACCT_EXPIRED},
      {"cred_err", PAM_CRED_ERR},
      {"session_err", PAM_SESSION_ERR},
      {"perm_denied", PAM_PERM_DENIED},
      {"maxtries", PAM_MAXTRIES},
      {"user_unknown", PAM_USER_UNKNOWN},
  };

  for (size_t i = 0; i < sizeof(map) / sizeof(map[0]); i++)
  {
    if (strcmp(map[i].name, name) == 0)
      return map[i].rc;
  }
  return PAM_SUCCESS;
}

static void load_control(struct probe_control *ctl)
{
  char buf[CONTROL_MAX];
  const char *path = getenv("LINUXIO_PAM_PROBE_CONTROL");
  ssize_t n = -1;
  int fd;

  memset(ctl, 0, sizeof(*ctl));
  if (!path || !*path)
    return;

  fd = open(path, O_RDONLY | O_CLOEXEC);
  if (fd < 0)
    return;
  n = read(fd, buf, sizeof(buf) - 1);
  close(fd);
  if (n <= 0)
    return;
  buf[n] = '\0';

  char *saveptr = NULL;
  for (char *line = strtok_r(buf, "\n", &saveptr); line;
       line = strtok_r(NULL, "\n", &saveptr))
  {
    char *eq = strchr(line, '=');
    if (!eq)
      continue;
    *eq = '\0';
    const char *key = line;
    const char *value = eq + 1;

    if (strcmp(key, "remap_from") == 0)
      snprintf(ctl->remap_from, sizeof(ctl->remap_from), "%s", value);
    else if (strcmp(key, "remap_to") == 0)
      snprintf(ctl->remap_to, sizeof(ctl->remap_to), "%s", value);
    else if (strcmp(key, "fail_auth") == 0)
      ctl->fail_auth = rc_from_name(value);
    else if (strcmp(key, "fail_acct") == 0)
      ctl->fail_acct = rc_from_name(value);
    else if (strcmp(key, "fail_setcred_establish") == 0)
      ctl->fail_setcred_establish = rc_from_name(value);
    else if (strcmp(key, "fail_setcred_reinit") == 0)
      ctl->fail_setcred_reinit = rc_from_name(value);
    else if (strcmp(key, "fail_open_session") == 0)
      ctl->fail_open_session = rc_from_name(value);
  }
}

static void trace_line(const char *fmt, ...)
{
  char line[512];
  const char *path = getenv("LINUXIO_PAM_PROBE_TRACE");
  va_list ap;
  int len;
  int fd;

  if (!path || !*path)
    return;

  va_start(ap, fmt);
  len = vsnprintf(line, sizeof(line) - 1, fmt, ap);
  va_end(ap);
  if (len < 0)
    return;
  if ((size_t)len >= sizeof(line) - 1)
    len = (int)sizeof(line) - 2;
  line[len] = '\n';

  fd = open(path, O_WRONLY | O_APPEND | O_CREAT | O_CLOEXEC, 0600);
  if (fd < 0)
    return;
  (void)!write(fd, line, (size_t)len + 1);
  close(fd);
}

static const char *item_or_dash(pam_handle_t *pamh, int item_type)
{
  const void *value = NULL;

  if (pam_get_item(pamh, item_type, &value) != PAM_SUCCESS || !value ||
      !((const char *)value)[0])
    return "-";
  return (const char *)value;
}

int pam_sm_authenticate(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  struct probe_control ctl;

  (void)flags;
  (void)argc;
  (void)argv;
  load_control(&ctl);

  const char *user = item_or_dash(pamh, PAM_USER);
  trace_line("authenticate user=%s rhost=%s tty=%s", user,
             item_or_dash(pamh, PAM_RHOST), item_or_dash(pamh, PAM_TTY));

  if (ctl.remap_from[0] && ctl.remap_to[0] && strcmp(user, ctl.remap_from) == 0)
  {
    int rc = pam_set_item(pamh, PAM_USER, ctl.remap_to);
    if (rc != PAM_SUCCESS)
      return rc;
    trace_line("remap from=%s to=%s", ctl.remap_from, ctl.remap_to);
  }

  if (ctl.fail_auth != PAM_SUCCESS)
    return ctl.fail_auth;
  return PAM_SUCCESS;
}

int pam_sm_setcred(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  struct probe_control ctl;
  int op = flags & ~PAM_SILENT;
  const char *op_name = "unknown";

  (void)argc;
  (void)argv;
  load_control(&ctl);

  if (op & PAM_ESTABLISH_CRED)
    op_name = "establish";
  else if (op & PAM_REINITIALIZE_CRED)
    op_name = "reinitialize";
  else if (op & PAM_DELETE_CRED)
    op_name = "delete";
  else if (op & PAM_REFRESH_CRED)
    op_name = "refresh";

  trace_line("setcred %s user=%s", op_name, item_or_dash(pamh, PAM_USER));

  if ((op & PAM_ESTABLISH_CRED) && ctl.fail_setcred_establish != PAM_SUCCESS)
    return ctl.fail_setcred_establish;
  if ((op & PAM_REINITIALIZE_CRED) && ctl.fail_setcred_reinit != PAM_SUCCESS)
    return ctl.fail_setcred_reinit;
  return PAM_SUCCESS;
}

int pam_sm_acct_mgmt(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  struct probe_control ctl;

  (void)flags;
  (void)argc;
  (void)argv;
  load_control(&ctl);

  trace_line("acct_mgmt user=%s", item_or_dash(pamh, PAM_USER));

  if (ctl.fail_acct != PAM_SUCCESS)
    return ctl.fail_acct;
  return PAM_SUCCESS;
}

int pam_sm_open_session(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  struct probe_control ctl;

  (void)flags;
  (void)argc;
  (void)argv;
  load_control(&ctl);

  trace_line("open_session user=%s", item_or_dash(pamh, PAM_USER));

  if (ctl.fail_open_session != PAM_SUCCESS)
    return ctl.fail_open_session;
  return PAM_SUCCESS;
}

int pam_sm_close_session(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  (void)flags;
  (void)argc;
  (void)argv;

  trace_line("close_session user=%s", item_or_dash(pamh, PAM_USER));
  return PAM_SUCCESS;
}

int pam_sm_chauthtok(pam_handle_t *pamh, int flags, int argc, const char **argv)
{
  (void)pamh;
  (void)flags;
  (void)argc;
  (void)argv;
  return PAM_SERVICE_ERR;
}
