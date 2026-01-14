// /usr/local/bin/linuxio-auth  (install 0755 root:root, runs via systemd)
// Single-shot mode: read one JSON auth request from stdin (socket-activated)
#define __STDC_WANT_LIB_EXT1__ 1
#define _GNU_SOURCE
#include <security/pam_appl.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <pwd.h>
#include <grp.h>
#include <errno.h>
#include <signal.h>
#include <sys/wait.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/time.h>
#include <sys/resource.h>
#include <sys/select.h>
#include <sys/syscall.h>
#include <sys/mman.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <strings.h>
#include <syslog.h>
#include <stdarg.h>
#include <limits.h>
#include <sys/prctl.h>
#include <sys/mount.h>
#include <sched.h>
#include <ctype.h>
// Safe argv shim for exec* (drops const only at the API boundary)
#define ARGV_UNCONST(a) \
  ((union { const char *const *in; char *const *out; }){.in = (a)}.out)
#ifndef PR_SET_NO_NEW_PRIVS
#define PR_SET_NO_NEW_PRIVS 38
#endif

#ifdef __has_include
#if __has_include(<systemd/sd-journal.h>)
#include <systemd/sd-journal.h>
#define HAVE_SD_JOURNAL 1
#endif
#endif

// Protocol constants
#include "linuxio_protocol.h"

// Socket timeouts (seconds)
#define SOCKET_READ_TIMEOUT 30
#define SOCKET_WRITE_TIMEOUT 10
#define BRIDGE_START_TIMEOUT_MS 5000

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
extern char **environ;

// ---- forward decls ----
static int write_all(int fd, const void *buf, size_t len);
static int env_get_int(const char *name, int defval, int minv, int maxv);

// Max lengths (use PROTO_MAX_* from linuxio_protocol.h, these are local convenience)
#define MAX_PATH_LEN 4096

// -------- safe formatting helpers --------
static int safe_vsnprintf(char *dst, size_t dstsz, const char *fmt, va_list ap)
{
  if (!dst || dstsz == 0)
    return -1;
#if defined(__STDC_LIB_EXT1__)
  int n = vsnprintf_s(dst, dstsz, _TRUNCATE, fmt, ap);
  if (n < 0)
  {
    dst[0] = '\0';
    return -1;
  }
  return n;
#else
#if defined(__GNUC__) && !defined(__clang_analyzer__)
  int n = __builtin___vsnprintf_chk(dst, dstsz, 0, dstsz, fmt, ap);
#else
  int n = vsnprintf(dst, dstsz, fmt, ap);
#endif
  if (n < 0)
  {
    dst[0] = '\0';
    return -1;
  }
  if ((size_t)n >= dstsz)
    dst[dstsz - 1] = '\0';
  return n;
#endif
}

static int safe_snprintf(char *dst, size_t dstsz, const char *fmt, ...)
{
  va_list ap;
  va_start(ap, fmt);
  int n = safe_vsnprintf(dst, dstsz, fmt, ap);
  va_end(ap);
  return n;
}

// -------- minimal logging  --------
static void journal_errorf(const char *fmt, ...)
{
  char buf[512];
  va_list ap;
  va_start(ap, fmt);
  (void)safe_vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
#ifdef HAVE_SD_JOURNAL
  (void)sd_journal_send("MESSAGE=%s", buf, "PRIORITY=%i", LOG_ERR,
                        "SYSLOG_IDENTIFIER=linuxio-auth", NULL);
#else
  openlog("linuxio-auth", LOG_PID, LOG_AUTHPRIV);
  syslog(LOG_ERR, "%s", buf);
  closelog();
#endif
}

static void journal_infof(const char *fmt, ...)
{
  char buf[512];
  va_list ap;
  va_start(ap, fmt);
  (void)safe_vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
#ifdef HAVE_SD_JOURNAL
  (void)sd_journal_send("MESSAGE=%s", buf, "PRIORITY=%i", LOG_INFO,
                        "SYSLOG_IDENTIFIER=linuxio-auth", NULL);
#else
  openlog("linuxio-auth", LOG_PID, LOG_AUTHPRIV);
  syslog(LOG_INFO, "%s", buf);
  closelog();
#endif
}

static void log_stderrf(const char *fmt, ...)
{
  char buf[1024];
  va_list ap;
  va_start(ap, fmt);
  (void)safe_vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
  (void)write_all(STDERR_FILENO, buf, strlen(buf));
  (void)write_all(STDERR_FILENO, "\n", 1);
}

// -------- secure zero ----------
#ifndef _WIN32
static void secure_bzero(void *p, size_t n)
{
#if defined(__GLIBC__) || defined(__APPLE__)
  if (p && n)
    explicit_bzero(p, n);
#else
  if (!p)
    return;
  volatile unsigned char *vp = (volatile unsigned char *)p;
  while (n--)
    *vp++ = 0;
#endif
}
#endif

// -------- Binary protocol read helpers --------
static int read_all(int fd, void *buf, size_t len)
{
  unsigned char *p = (unsigned char *)buf;
  while (len > 0)
  {
    ssize_t n = read(fd, p, len);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    if (n == 0)
      return -1; // EOF
    p += (size_t)n;
    len -= (size_t)n;
  }
  return 0;
}

static uint16_t read_u16_be(const uint8_t *buf)
{
  return ((uint16_t)buf[0] << 8) | ((uint16_t)buf[1]);
}

// Read a length-prefixed string from fd into buf (max bufsz-1 chars + null).
// Returns 0 on success, -1 on error (oversized fields are rejected).
// Uses a temporary buffer for the read to minimize sensitive data exposure -
// the temp buffer is securely cleared immediately after copying to the caller's buffer.
static int read_lenstr(int fd, char *buf, size_t bufsz)
{
  if (!buf || bufsz == 0)
    return -1;
  buf[0] = '\0';

  uint8_t lenbuf[2];
  if (read_all(fd, lenbuf, 2) != 0)
    return -1;

  uint16_t len = read_u16_be(lenbuf);
  if (len == 0)
    return 0;

  // Reject oversized input to avoid truncation and protocol ambiguity.
  if (len >= bufsz)
    return -1;

  // Read into a temporary buffer first so we can securely wipe it afterwards.
  unsigned char *tmp = (unsigned char *)malloc(len);
  if (!tmp)
    return -1;

  if (read_all(fd, tmp, len) != 0)
  {
    secure_bzero(tmp, len);
    free(tmp);
    return -1;
  }

  // Copy to caller's buffer and null-terminate.
  memcpy(buf, tmp, len);
  buf[len] = '\0';

  // Wipe and free the temporary buffer to avoid leaving sensitive data in memory.
  secure_bzero(tmp, len);
  free(tmp);

  return 0;
}

// -------- PAM conversation ----
struct pam_appdata {
  const char *password;
  char motd[4096];
  size_t motd_len;
};

static int pam_conv_func(int n, const struct pam_message **msg, struct pam_response **resp, void *appdata_ptr)
{
  struct pam_appdata *appdata = (struct pam_appdata *)appdata_ptr;
  if (n <= 0 || n > 32)
    return PAM_CONV_ERR;
  struct pam_response *r = calloc((size_t)n, sizeof(*r));
  if (!r)
    return PAM_CONV_ERR;

  for (int i = 0; i < n; i++)
  {
    switch (msg[i]->msg_style)
    {
    case PAM_PROMPT_ECHO_OFF:
      if (appdata && appdata->password)
      {
        r[i].resp = strdup(appdata->password);
        if (!r[i].resp)
        {
          for (int j = 0; j < i; j++)
            free(r[j].resp);
          free(r);
          return PAM_CONV_ERR;
        }
      }
      break;

    case PAM_TEXT_INFO:
    case PAM_ERROR_MSG:
      // Collect MOTD and informational messages
      if (appdata && msg[i]->msg && *msg[i]->msg)
      {
        size_t msg_len = strlen(msg[i]->msg);
        size_t space_left = sizeof(appdata->motd) - appdata->motd_len - 1;

        if (space_left > 0)
        {
          // Append message
          size_t copy_len = (msg_len < space_left) ? msg_len : space_left;
          memcpy(appdata->motd + appdata->motd_len, msg[i]->msg, copy_len);
          appdata->motd_len += copy_len;

          // Add newline if there's space
          if (appdata->motd_len < sizeof(appdata->motd) - 1)
          {
            appdata->motd[appdata->motd_len++] = '\n';
          }

          appdata->motd[appdata->motd_len] = '\0';
        }
      }
      break;

    default:
      // Ignore other message types
      break;
    }
  }
  *resp = r;
  return PAM_SUCCESS;
}

// -------- privilege drop -------


static int env_get_int(const char *name, int defval, int minv, int maxv)
{
  const char *s = getenv(name);
  if (!s || !*s)
    return defval;
  char *end = NULL;
  long v = strtol(s, &end, 10);
  if (!end || *end)
    return defval;
  if (v < minv)
    v = minv;
  if (v > maxv)
    v = maxv;
  return (int)v;
}

// ---- bridge binary validation ----
static int validate_bridge_via_fd(int fd, uid_t required_owner)
{
  struct stat st;
  if (fstat(fd, &st) != 0)
  {
    perror("fstat bridge");
    return -1;
  }
  if (!S_ISREG(st.st_mode))
    return -1;
  if ((st.st_mode & (S_IWGRP | S_IWOTH)) != 0)
    return -1;
  if (st.st_uid != required_owner)
    return -1;
  if ((st.st_mode & 0111) == 0)
    return -1;
  if (st.st_mode & (S_ISUID | S_ISGID))
    return -1;
  return 0;
}

static int validate_parent_dir_policy(const struct stat *ds, uid_t file_owner, uid_t user_uid)
{
  if (!S_ISDIR(ds->st_mode))
    return -1;
  if (file_owner == 0)
  {
    if (ds->st_uid != 0)
      return -1;
    if (ds->st_mode & (S_IWGRP | S_IWOTH))
      return -1;
    return 0;
  }
  if (file_owner == user_uid)
  {
    if (ds->st_uid != user_uid)
      return -1;
    if (ds->st_mode & (S_IWGRP | S_IWOTH))
      return -1;
    return 0;
  }
  return -1;
}

static int validate_parent_dir_via_fd(int dfd, uid_t file_owner, uid_t user_uid)
{
  struct stat ds;
  if (fstat(dfd, &ds) != 0)
    return -1;
  return validate_parent_dir_policy(&ds, file_owner, user_uid);
}

// Resource limits for the bridge process
static void set_resource_limits(void)
{
  struct rlimit rl;
  rl.rlim_cur = rl.rlim_max = 10UL * 60;
  (void)setrlimit(RLIMIT_CPU, &rl);

  rl.rlim_cur = rl.rlim_max = 2048;
  (void)setrlimit(RLIMIT_NOFILE, &rl);

  int nproc_limit = env_get_int("LINUXIO_RLIMIT_NPROC", 1024, 10, 4096);
  rl.rlim_cur = rl.rlim_max = (rlim_t)nproc_limit;
  (void)setrlimit(RLIMIT_NPROC, &rl);

  rl.rlim_cur = rl.rlim_max = 16UL * 1024 * 1024 * 1024;
  (void)setrlimit(RLIMIT_AS, &rl);
}

static int open_and_validate_bridge(const char *bridge_path, uid_t required_owner, int *out_fd)
{
  int fd = open(bridge_path, O_PATH | O_CLOEXEC | O_NOFOLLOW);
  if (fd < 0)
  {
    perror("open bridge");
    return -1;
  }
  struct stat st;
  if (fstat(fd, &st) != 0)
  {
    perror("fstat bridge");
    close(fd);
    return -1;
  }
  if (validate_bridge_via_fd(fd, required_owner) != 0)
  {
    close(fd);
    return -1;
  }

  char linkbuf[PATH_MAX], fdlink[64];
  safe_snprintf(fdlink, sizeof(fdlink), "/proc/self/fd/%d", fd);
  ssize_t n = readlink(fdlink, linkbuf, sizeof(linkbuf) - 1);
  if (n < 0)
  {
    close(fd);
    return -1;
  }
  linkbuf[n] = '\0';
  char *slash = strrchr(linkbuf, '/');
  if (!slash || slash == linkbuf)
  {
    close(fd);
    return -1;
  }
  *slash = '\0';
  int dfd = open(linkbuf, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (dfd < 0)
  {
    close(fd);
    return -1;
  }
  int dir_ok = validate_parent_dir_via_fd(dfd, st.st_uid, required_owner);
  close(dfd);
  if (dir_ok != 0)
  {
    close(fd);
    return -1;
  }

  *out_fd = fd;
  return 0;
}

// -------- Binary bootstrap helpers --------
static void write_u32_be(uint8_t *buf, uint32_t v)
{
  buf[0] = (uint8_t)(v >> 24);
  buf[1] = (uint8_t)(v >> 16);
  buf[2] = (uint8_t)(v >> 8);
  buf[3] = (uint8_t)(v);
}

static void write_u16_be(uint8_t *buf, uint16_t v)
{
  buf[0] = (uint8_t)(v >> 8);
  buf[1] = (uint8_t)(v);
}

// Write a length-prefixed string (2-byte length + data)
static int write_lenstr(int fd, const char *s)
{
  uint16_t len = 0;
  if (s)
  {
    size_t slen = strlen(s);
    if (slen > 0xFFFF)
      slen = 0xFFFF; // Cap at max uint16
    len = (uint16_t)slen;
  }

  uint8_t lenbuf[2];
  write_u16_be(lenbuf, len);
  if (write_all(fd, lenbuf, 2) != 0)
    return -1;
  if (len > 0 && write_all(fd, s, len) != 0)
    return -1;
  return 0;
}

// Write binary bootstrap to a file descriptor
// Returns 0 on success, -1 on error
static int write_bootstrap_binary(
    int fd,
    const char *session_id,
    const char *username,
    const char *motd,
    uid_t uid,
    gid_t gid,
    int verbose,
    int privileged)
{
  uint8_t header[PROTO_HEADER_SIZE];
  int pos = 0;

  // Magic + version (4 bytes)
  header[pos++] = PROTO_MAGIC_0;
  header[pos++] = PROTO_MAGIC_1;
  header[pos++] = PROTO_MAGIC_2;
  header[pos++] = PROTO_VERSION;

  // UID (4 bytes)
  write_u32_be(header + pos, (uint32_t)uid);
  pos += 4;

  // GID (4 bytes)
  write_u32_be(header + pos, (uint32_t)gid);
  pos += 4;

  // Flags (1 byte)
  uint8_t flags = 0;
  if (verbose)
    flags |= PROTO_FLAG_VERBOSE;
  if (privileged)
    flags |= PROTO_FLAG_PRIVILEGED;
  header[pos++] = flags;

  // Write fixed header
  if (write_all(fd, header, PROTO_HEADER_SIZE) != 0)
    return -1;

  // Write variable-length fields (length-prefixed)
  if (write_lenstr(fd, session_id) != 0)
    return -1;
  if (write_lenstr(fd, username) != 0)
    return -1;
  if (write_lenstr(fd, motd) != 0)
    return -1;

  return 0;
}

// sudo probing
static int run_cmd_as_user_with_input(const struct passwd *pw, const char *const argv[],
                                      const char *stdin_data, int timeout_sec)
{
  int inpipe[2] = {-1, -1};
#if defined(HAVE_PIPE2) || (defined(__linux__) && defined(O_CLOEXEC))
  if (pipe2(inpipe, O_CLOEXEC) != 0)
    return -1;
#else
  if (pipe(inpipe) != 0)
    return -1;
  {
    int fdflags = fcntl(inpipe[0], F_GETFD);
    if (fdflags >= 0)
      (void)fcntl(inpipe[0], F_SETFD, fdflags | FD_CLOEXEC);
    fdflags = fcntl(inpipe[1], F_GETFD);
    if (fdflags >= 0)
      (void)fcntl(inpipe[1], F_SETFD, fdflags | FD_CLOEXEC);
  }
#endif

  pid_t pid = fork();
  if (pid < 0)
  {
    close(inpipe[0]);
    close(inpipe[1]);
    return -1;
  }
  if (pid == 0)
  {
    if (setgroups(0, NULL) != 0)
      _exit(127);
    if (initgroups(pw->pw_name, pw->pw_gid) != 0)
      _exit(127);
    if (setgid(pw->pw_gid) != 0)
      _exit(127);
    if (setuid(pw->pw_uid) != 0)
      _exit(127);

    if (dup2(inpipe[0], STDIN_FILENO) < 0)
      _exit(127);
    close(inpipe[0]);
    close(inpipe[1]);

    clearenv();
    setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", 1);
    setenv("LANG", "C", 1);
    execv("/usr/bin/sudo", ARGV_UNCONST(argv));

    _exit(127);
  }
  close(inpipe[0]);
  if (stdin_data && *stdin_data)
    (void)write_all(inpipe[1], stdin_data, strlen(stdin_data));
  close(inpipe[1]);

  int status = 0;
  int elapsed_ms = 0;
  while (elapsed_ms < timeout_sec * 1000)
  {
    pid_t r = waitpid(pid, &status, WNOHANG);
    if (r == pid)
      break;
    if (r < 0 && errno != EINTR)
      break;
    usleep(100 * 1000);
    elapsed_ms += 100;
  }
  if (elapsed_ms >= timeout_sec * 1000)
  {
    kill(pid, SIGKILL);
    waitpid(pid, &status, 0);
    return -1;
  }

  if (WIFEXITED(status))
    return WEXITSTATUS(status);
  if (WIFSIGNALED(status))
    return 128 + WTERMSIG(status);
  return -1;
}

static int user_has_sudo(const struct passwd *pw, const char *password, int *out_nopasswd)
{
  // We don't currently differentiate NOPASSWD vs PASSWD in the rest of the code,
  // so just clear this and treat "has sudo" as a boolean.
  if (out_nopasswd)
    *out_nopasswd = 0;

  // How long we wait for sudo -S -v to complete
  int to_pw = env_get_int("LINUXIO_SUDO_TIMEOUT_PASSWORD", 4, 1, 30);

  // If we don't have a password, don't even try
  if (!password || !*password)
    return 0;

  // Validate sudo using the same password we used for PAM
  const char *argv_pw[] = {"/usr/bin/sudo", "-S", "-p", "", "-v", NULL};

  // Buffer must accommodate password + newline + null terminator
  char buf[PROTO_MAX_PASSWORD + 2];
  (void)safe_snprintf(buf, sizeof(buf), "%s\n", password);

  int rc = run_cmd_as_user_with_input(pw, argv_pw, buf, to_pw);

  // Wipe the temporary buffer
  secure_bzero(buf, sizeof(buf));

  if (rc == 0)
  {
    // Drop any cached sudo credentials immediately; we just wanted to know
    // whether sudo works, not to keep a ticket open.
    const char *argv_k[] = {"/usr/bin/sudo", "-k", NULL};
    (void)run_cmd_as_user_with_input(pw, argv_k, NULL, 2);
    return 1;
  }

  return 0;
}

static void drop_to_user(const struct passwd *pw)
{
  if (setgroups(0, NULL) != 0)
    _exit(127);
  if (initgroups(pw->pw_name, pw->pw_gid) != 0)
    _exit(127);
  if (setgid(pw->pw_gid) != 0)
    _exit(127);
  if (setuid(pw->pw_uid) != 0)
    _exit(127);
  if (setuid(0) == 0)
    _exit(127);
}
// Locale validation - only allow safe locale strings
static int valid_locale(const char *s)
{
  if (!s || !*s)
    return 0;

  size_t len = strlen(s);
  if (len > 64)  // Reasonable max for locale strings
    return 0;

  // Allow [A-Za-z0-9_.-@] for locale strings like "en_US.UTF-8" or "C.UTF-8"
  for (size_t i = 0; i < len; i++)
  {
    char c = s[i];
    if (!((c >= 'A' && c <= 'Z') ||
          (c >= 'a' && c <= 'z') ||
          (c >= '0' && c <= '9') ||
          c == '_' || c == '-' || c == '.' || c == '@'))
      return 0;
  }

  return 1;
}

// Session ID validation - only allow safe characters
static int valid_session_id(const char *s)
{
  if (!s || !*s)
    return 0;

  size_t len = strlen(s);
  if (len == 0 || len > 64)  // Max 64 chars
    return 0;

  // Only allow [A-Za-z0-9_-]
  for (size_t i = 0; i < len; i++)
  {
    char c = s[i];
    if (!((c >= 'A' && c <= 'Z') ||
          (c >= 'a' && c <= 'z') ||
          (c >= '0' && c <= '9') ||
          c == '_' || c == '-'))
      return 0;
  }

  return 1;
}

// -------- Peer credential check (defense-in-depth) --------
// Verify the connecting process is authorized (root or linuxio-bridge-socket group)
// This mirrors the systemd socket policy but is kernel-enforced.
#define AUTH_SOCKET_GROUP "linuxio-bridge-socket"

// Returns 1 if uid is in target_gid (by configured groups), 0 if not, -1 on error.
static int user_in_group(uid_t uid, gid_t target_gid)
{
  long buflen = sysconf(_SC_GETPW_R_SIZE_MAX);
  if (buflen < 0)
    buflen = 16384;
  char *buf = calloc(1, (size_t)buflen);
  if (!buf)
    return -1;

  struct passwd pw;
  struct passwd *pw_out = NULL;
  int rc = getpwuid_r(uid, &pw, buf, (size_t)buflen, &pw_out);
  if (rc != 0 || !pw_out)
  {
    free(buf);
    return -1;
  }

  int ngroups = 16;
  gid_t *groups = calloc((size_t)ngroups, sizeof(gid_t));
  if (!groups)
  {
    free(buf);
    return -1;
  }

  int gret = getgrouplist(pw_out->pw_name, pw_out->pw_gid, groups, &ngroups);
  if (gret == -1)
  {
    gid_t *tmp = realloc(groups, (size_t)ngroups * sizeof(gid_t));
    if (!tmp)
    {
      free(groups);
      free(buf);
      return -1;
    }
    groups = tmp;
    gret = getgrouplist(pw_out->pw_name, pw_out->pw_gid, groups, &ngroups);
  }

  int found = 0;
  if (gret != -1)
  {
    for (int i = 0; i < ngroups; i++)
    {
      if (groups[i] == target_gid)
      {
        found = 1;
        break;
      }
    }
  }

  free(groups);
  free(buf);
  return gret == -1 ? -1 : found;
}

static int check_peer_creds(int fd)
{
  struct ucred cred;
  socklen_t len = sizeof(cred);

  if (getsockopt(fd, SOL_SOCKET, SO_PEERCRED, &cred, &len) != 0)
  {
    journal_errorf("getsockopt(SO_PEERCRED) failed: %m");
    return -1;
  }

  // Allow root
  if (cred.uid == 0)
    return 0;

  // Allow if peer's primary GID matches linuxio-bridge-socket.
  // Edge case: supplementary group membership won't match here. We use
  // getgrouplist() to check the user's configured groups. Caveat: this reflects
  // the user's configured groups, not necessarily the process's current group
  // set. For strict "current groups," parse /proc/<pid>/status Groups: instead.
  struct group *gr = getgrnam(AUTH_SOCKET_GROUP);
  if (!gr)
  {
    journal_errorf("group '%s' not found", AUTH_SOCKET_GROUP);
    return -1;
  }

  if (cred.gid == gr->gr_gid)
    return 0;

  int in_group = user_in_group(cred.uid, gr->gr_gid);
  if (in_group > 0)
    return 0;
  if (in_group < 0)
    journal_errorf("failed to resolve supplementary groups for uid=%u", (unsigned)cred.uid);

  journal_errorf("peer not authorized: uid=%u gid=%u (expected root or gid=%u)",
                 (unsigned)cred.uid, (unsigned)cred.gid, (unsigned)gr->gr_gid);
  return -1;
}


// ============================================================================
// Single-shot mode - socket-activated worker
// ============================================================================

// Send binary response to client
// Format: [magic:4][status:1][mode:1][reserved:2][len:2][error_or_motd]
static void send_response(int fd, uint8_t status, uint8_t mode, const char *error, const char *motd)
{
  uint8_t header[PROTO_AUTH_RESP_HEADER_SIZE];

  // Magic + version
  header[0] = PROTO_MAGIC_0;
  header[1] = PROTO_MAGIC_1;
  header[2] = PROTO_MAGIC_2;
  header[3] = PROTO_VERSION;

  // Status and mode
  header[4] = status;
  header[5] = mode;

  // Reserved
  header[6] = 0;
  header[7] = 0;

  if (write_all(fd, header, PROTO_AUTH_RESP_HEADER_SIZE) != 0)
    return;

  // Write error or motd string
  if (status == PROTO_STATUS_ERROR && error)
  {
    (void)write_lenstr(fd, error);
  }
  else if (status == PROTO_STATUS_OK)
  {
    (void)write_lenstr(fd, motd ? motd : "");
  }
}

// Fixed FD layout for child process:
// 0 = stdin (bootstrap pipe)
// 1 = stdout (dup from stderr)
// 2 = stderr
// 3 = client connection (CLIENT_CONN_FD)
// 4 = exec_status_fd (CLOEXEC - closed by exec on success)
// 5 = bridge_fd (for execveat)
// Everything >= 6 is closed
#define CLIENT_CONN_FD 3
#define EXEC_STATUS_FD 4
#define BRIDGE_FD      5

static pid_t spawn_bridge_process(
    const struct passwd *pw,
    int want_privileged,
    int bridge_fd,
    int bootstrap_pipe_read,  // Pipe read end for bootstrap binary (will be stdin)
    int client_fd,            // Client connection FD (will be dup'd to FD 3 for Yamux)
    int exec_status_fd)       // Write end of exec-status pipe (CLOEXEC) - write on exec failure
{
  pid_t pid = fork();
  if (pid < 0)
    return -1;
  if (pid > 0)
    return pid;

  // =========================================================================
  // Child: Set up fixed FD layout before closing everything else
  // Order matters to avoid overwriting FDs we still need
  // =========================================================================

  // Step 1: Move FDs to their fixed positions
  // Use dup2 which is atomic and handles fd == newfd correctly

  // Save the original FDs we need (they might be at any position)
  int orig_client = client_fd;
  int orig_bootstrap = bootstrap_pipe_read;
  int orig_exec_status = exec_status_fd;
  int orig_bridge = bridge_fd;

  // First, move exec_status_fd and bridge_fd to high positions to avoid conflicts
  // (in case any of them is already at 0-5)
  int tmp_exec_status = -1, tmp_bridge = -1;

  if (orig_exec_status >= 0 && orig_exec_status <= BRIDGE_FD)
  {
    tmp_exec_status = dup(orig_exec_status);
    if (tmp_exec_status < 0) _exit(127);
    // Preserve CLOEXEC on the new FD
    {
      int fdflags = fcntl(tmp_exec_status, F_GETFD);
      if (fdflags >= 0)
        (void)fcntl(tmp_exec_status, F_SETFD, fdflags | FD_CLOEXEC);
    }
    // Close original to avoid leaking extra copy of pipe write-end
    close(orig_exec_status);
  }
  else
  {
    tmp_exec_status = orig_exec_status;
  }

  if (orig_bridge >= 0 && orig_bridge <= BRIDGE_FD)
  {
    tmp_bridge = dup(orig_bridge);
    if (tmp_bridge < 0) _exit(127);
    // Close original to avoid leaking extra FD
    close(orig_bridge);
  }
  else
  {
    tmp_bridge = orig_bridge;
  }

  // Step 2: Set up stdin (FD 0) from bootstrap pipe
  // IMPORTANT: Do this before dup2'ing to FD 3, in case client_fd == 0
  if (orig_client == STDIN_FILENO)
  {
    // client_fd is stdin - need to save it first
    int saved_client = dup(orig_client);
    if (saved_client < 0) _exit(127);
    orig_client = saved_client;
  }

  if (orig_bootstrap >= 0)
  {
    if (dup2(orig_bootstrap, STDIN_FILENO) < 0) _exit(127);
    if (orig_bootstrap != STDIN_FILENO) close(orig_bootstrap);
  }

  // Step 3: Set up stdout (FD 1) as dup of stderr
  if (dup2(STDERR_FILENO, STDOUT_FILENO) < 0) _exit(127);

  // Step 4: Set up client connection at FD 3
  if (orig_client >= 0 && orig_client != CLIENT_CONN_FD)
  {
    if (dup2(orig_client, CLIENT_CONN_FD) < 0) _exit(127);
    close(orig_client);
  }

  // Step 5: Set up exec_status_fd at FD 4 (keep CLOEXEC)
  if (tmp_exec_status >= 0 && tmp_exec_status != EXEC_STATUS_FD)
  {
    if (dup2(tmp_exec_status, EXEC_STATUS_FD) < 0) _exit(127);
    close(tmp_exec_status);
    // Restore CLOEXEC on the new FD
    {
      int fdflags = fcntl(EXEC_STATUS_FD, F_GETFD);
      if (fdflags >= 0)
        (void)fcntl(EXEC_STATUS_FD, F_SETFD, fdflags | FD_CLOEXEC);
    }
  }
  else if (tmp_exec_status == EXEC_STATUS_FD)
  {
    // Already at right position, just ensure CLOEXEC
    {
      int fdflags = fcntl(EXEC_STATUS_FD, F_GETFD);
      if (fdflags >= 0)
        (void)fcntl(EXEC_STATUS_FD, F_SETFD, fdflags | FD_CLOEXEC);
    }
  }

  // Step 6: Set up bridge_fd at FD 5
  if (tmp_bridge >= 0 && tmp_bridge != BRIDGE_FD)
  {
    if (dup2(tmp_bridge, BRIDGE_FD) < 0) _exit(127);
    close(tmp_bridge);
  }

  // Now we have:
  // 0 = stdin (bootstrap)
  // 1 = stdout (-> stderr)
  // 2 = stderr
  // 3 = client connection
  // 4 = exec_status_fd (CLOEXEC)
  // 5 = bridge_fd

  // Clear socket timeouts on the client connection (FD 3)
  // These were set for the auth request phase but would cause problems
  // for the long-lived Yamux connection (idle timeouts, EAGAIN, etc.)
  {
    struct timeval tv_zero = {.tv_sec = 0, .tv_usec = 0};
    (void)setsockopt(CLIENT_CONN_FD, SOL_SOCKET, SO_RCVTIMEO, &tv_zero, sizeof(tv_zero));
    (void)setsockopt(CLIENT_CONN_FD, SOL_SOCKET, SO_SNDTIMEO, &tv_zero, sizeof(tv_zero));
  }

  umask(077);
  set_resource_limits();

  // Preserve and validate environment variables before clearenv()
  const char *preserve_lang = getenv("LANG");
  const char *preserve_term = getenv("TERM");
  const char *preserve_journal_stream = getenv("JOURNAL_STREAM");

  // Save validated copies
  char safe_lang[128] = "C.UTF-8";  // Default to UTF-8 instead of plain C
  char safe_term[128] = "xterm-256color";

  if (preserve_lang && valid_locale(preserve_lang))
  {
    safe_snprintf(safe_lang, sizeof(safe_lang), "%s", preserve_lang);
  }

  if (preserve_term && *preserve_term)
  {
    // TERM should be simple and safe - just alphanumeric and dash
    int valid = 1;
    for (const char *p = preserve_term; *p && valid; p++)
    {
      char c = *p;
      if (!((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
            (c >= '0' && c <= '9') || c == '-'))
        valid = 0;
    }
    if (valid && strlen(preserve_term) < sizeof(safe_term))
      safe_snprintf(safe_term, sizeof(safe_term), "%s", preserve_term);
  }

  clearenv();
  setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", 1);
  setenv("LANG", safe_lang, 1);
  setenv("LC_ALL", safe_lang, 1);
  setenv("TERM", safe_term, 1);

  // Restore JOURNAL_STREAM if present - needed for proper syslog priority logging
  if (preserve_journal_stream && *preserve_journal_stream)
    setenv("JOURNAL_STREAM", preserve_journal_stream, 1);

  if (want_privileged)
  {
    setenv("HOME", "/root", 1);
    setenv("USER", "root", 1);
    setenv("LOGNAME", "root", 1);
    if (setgroups(0, NULL) != 0)
      _exit(127);
    if (setresgid(0, 0, 0) != 0)
      _exit(127);
    if (setresuid(0, 0, 0) != 0)
      _exit(127);
  }
  else
  {
    drop_to_user(pw);
    if (pw)
    {
      setenv("HOME", pw->pw_dir, 1);
      setenv("USER", pw->pw_name, 1);
      setenv("LOGNAME", pw->pw_name, 1);
      char xdg[64];
      safe_snprintf(xdg, sizeof(xdg), "/run/user/%u", (unsigned)pw->pw_uid);
      setenv("XDG_RUNTIME_DIR", xdg, 1);
      if (chdir(pw->pw_dir) != 0)
        _exit(127);
    }
  }

  // All config is passed via binary bootstrap on stdin - no env vars needed

  // Close all file descriptors >= 6 (keeping 0-5 as set up above)
  // Uses close_range() syscall (Linux 5.9+) with fallback for older kernels
#ifndef __NR_close_range
  #define __NR_close_range 436
#endif

  if (syscall(__NR_close_range, BRIDGE_FD + 1, ~0U, 0) == -1 && errno == ENOSYS)
  {
    // Fallback: manual loop for older kernels without close_range
    struct rlimit rl;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0)
    {
      int max_fd = (rl.rlim_cur < 4096) ? (int)rl.rlim_cur : 4096;
      for (int fd = BRIDGE_FD + 1; fd < max_fd; fd++)
      {
        (void)close(fd);
      }
    }
  }

  // All config passed via binary bootstrap on stdin - minimal argv
  const char *argv_child[] = {"linuxio-bridge", NULL};

  // Mark BRIDGE_FD as close-on-exec so it doesn't leak into the bridge process
  // (prevents "text file busy" on binary updates and avoids unnecessary FD leak)
  // CLOEXEC only closes after successful exec, so execveat() still works.
  {
    int fdflags = fcntl(BRIDGE_FD, F_GETFD);
    if (fdflags >= 0)
      (void)fcntl(BRIDGE_FD, F_SETFD, fdflags | FD_CLOEXEC);
  }

  // Execute the validated bridge binary via fd (prevents TOCTOU)
  // Try execveat first (Linux 3.19+); fallback to execv on ENOSYS
#ifndef __NR_execveat
  #define __NR_execveat 322
#endif
  long ret = syscall(__NR_execveat, BRIDGE_FD, "", ARGV_UNCONST(argv_child), environ, AT_EMPTY_PATH);

  // Fallback for kernels without execveat (< 3.19)
  if (ret == -1 && errno == ENOSYS)
  {
    // Read the real path from /proc/self/fd/BRIDGE_FD
    char fdpath[64], realpath_buf[PATH_MAX];
    safe_snprintf(fdpath, sizeof(fdpath), "/proc/self/fd/%d", BRIDGE_FD);
    ssize_t len = readlink(fdpath, realpath_buf, sizeof(realpath_buf) - 1);
    if (len > 0)
    {
      realpath_buf[len] = '\0';
      // Close bridge_fd before exec (no longer needed)
      close(BRIDGE_FD);
      // Use the real path we validated earlier
      execv(realpath_buf, ARGV_UNCONST(argv_child));
    }
  }

  // Exec failed - write error byte to status pipe before exiting
  // (if exec succeeded, CLOEXEC on FD 4 would have closed it)
  {
    uint8_t err_byte = 1;
    ssize_t wr = write(EXEC_STATUS_FD, &err_byte, 1);
    (void)wr; // Best-effort, we're exiting anyway
  }

  _exit(127);
}

// Handle a single client request
static int handle_client(int input_fd, int output_fd)
{
  // Read binary request header
  uint8_t header[PROTO_AUTH_REQ_HEADER_SIZE];
  if (read_all(input_fd, header, PROTO_AUTH_REQ_HEADER_SIZE) != 0)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to read request header", NULL);
    return 1;
  }

  // Validate magic
  if (header[0] != PROTO_MAGIC_0 || header[1] != PROTO_MAGIC_1 ||
      header[2] != PROTO_MAGIC_2 || header[3] != PROTO_VERSION)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "invalid request magic", NULL);
    return 1;
  }

  // Parse header fields
  uint8_t req_flags = header[4];
  int verbose_flag = (req_flags & PROTO_REQ_FLAG_VERBOSE) != 0;

  // Read variable-length fields
  char user[PROTO_MAX_USERNAME] = "";
  char password[PROTO_MAX_PASSWORD] = "";
  char session_id[PROTO_MAX_SESSION_ID] = "";

  if (read_lenstr(input_fd, user, sizeof(user)) != 0 ||
      read_lenstr(input_fd, password, sizeof(password)) != 0 ||
      read_lenstr(input_fd, session_id, sizeof(session_id)) != 0)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to read request fields", NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Validate required fields
  if (!user[0] || !session_id[0])
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "missing required fields", NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Validate session_id (defense against path injection)
  if (!valid_session_id(session_id))
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "invalid session_id format", NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // PAM authentication
  struct pam_appdata appdata = {.password = password, .motd = {0}, .motd_len = 0};
  struct pam_conv conv = {pam_conv_func, &appdata};
  pam_handle_t *pamh = NULL;
  int rc = pam_start("linuxio", user, &conv, &pamh);
  if (rc != PAM_SUCCESS)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, pam_strerror(NULL, rc), NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  (void)pam_set_item(pamh, PAM_RHOST, "web");
  rc = pam_authenticate(pamh, 0);
  if (rc == PAM_SUCCESS)
    rc = pam_acct_mgmt(pamh, 0);

  // Handle password expiration
  if (rc == PAM_NEW_AUTHTOK_REQD)
  {
    journal_infof("auth: password expired for user '%s'", user);
    send_response(output_fd, PROTO_STATUS_ERROR, 0,
                  "Password has expired. Please change it via SSH or console.", NULL);
    pam_end(pamh, rc);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  if (rc == PAM_SUCCESS)
    rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);

  if (rc != PAM_SUCCESS)
  {
    const char *err = pam_strerror(pamh, rc);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, err, NULL);
    pam_end(pamh, rc);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Get user info
  struct passwd *pw = getpwnam(user);
  if (!pw)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "user lookup failed", NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  journal_infof("auth: PAM auth success for user '%s' (uid=%u)", user, (unsigned)pw->pw_uid);

  // Check sudo capability
  int nopasswd = 0;
  int want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  // Clear password from memory
  secure_bzero(password, sizeof(password));

  uint8_t mode = want_privileged ? PROTO_MODE_PRIVILEGED : PROTO_MODE_UNPRIVILEGED;

  // Validate bridge binary and keep fd open (prevents TOCTOU)
  int bridge_fd = -1;
  if (open_and_validate_bridge("/usr/local/bin/linuxio-bridge", 0, &bridge_fd) != 0)
  {
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "bridge validation failed", NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }
  // Keep bridge_fd open - we'll exec it directly to prevent TOCTOU

  // Create pipe for bootstrap data (secrets never touch filesystem)
  int bootstrap_pipe[2];
  if (pipe(bootstrap_pipe) != 0)
  {
    journal_errorf("failed to create bootstrap pipe: %m");
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to prepare bootstrap", NULL);
    close(bridge_fd);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  rc = pam_open_session(pamh, 0);
  if (rc != PAM_SUCCESS)
  {
    const char *err = pam_strerror(pamh, rc);
    close(bootstrap_pipe[0]);
    close(bootstrap_pipe[1]);
    close(bridge_fd);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, err, NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Create exec-status pipe with CLOEXEC on write end
  // - On successful exec, CLOEXEC closes the write end -> parent sees EOF
  // - On exec failure, child writes error byte -> parent sees data
  int exec_status_pipe[2] = {-1, -1};
#if defined(HAVE_PIPE2) || (defined(__linux__) && defined(O_CLOEXEC))
  if (pipe2(exec_status_pipe, O_CLOEXEC) != 0)
  {
    journal_errorf("failed to create exec-status pipe: %m");
    close(bootstrap_pipe[0]);
    close(bootstrap_pipe[1]);
    close(bridge_fd);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to prepare exec check", NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }
#else
  if (pipe(exec_status_pipe) != 0)
  {
    journal_errorf("failed to create exec-status pipe: %m");
    close(bootstrap_pipe[0]);
    close(bootstrap_pipe[1]);
    close(bridge_fd);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to prepare exec check", NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }
  // Set CLOEXEC on write end only (child writes on failure, exec closes it on success)
  {
    int fdflags = fcntl(exec_status_pipe[1], F_GETFD);
    if (fdflags >= 0)
      (void)fcntl(exec_status_pipe[1], F_SETFD, fdflags | FD_CLOEXEC);
  }
#endif

  pid_t child = spawn_bridge_process(
      pw,
      want_privileged,
      bridge_fd,
      bootstrap_pipe[0],    // Pass pipe read end to child (will be stdin)
      input_fd,             // Pass client connection FD (will be dup'd to FD 3 for Yamux)
      exec_status_pipe[1]); // Write end of exec-status pipe (CLOEXEC)

  // Parent: close pipe read end and exec-status write end (child has them)
  close(bootstrap_pipe[0]);
  close(exec_status_pipe[1]);

  if (child < 0)
  {
    close(bootstrap_pipe[1]);
    close(exec_status_pipe[0]);
    close(bridge_fd);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "failed to spawn bridge", NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Parent: write binary bootstrap to pipe, then close to signal EOF
  int rc_bootstrap = write_bootstrap_binary(
      bootstrap_pipe[1],
      session_id,
      user,
      appdata.motd_len > 0 ? appdata.motd : NULL,
      pw->pw_uid,
      pw->pw_gid,
      verbose_flag,
      want_privileged);
  close(bootstrap_pipe[1]);

  if (rc_bootstrap != 0)
  {
    journal_errorf("failed to write bootstrap to pipe");
    close(exec_status_pipe[0]);
    close(bridge_fd);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "bootstrap communication failed", NULL);
    kill(child, SIGTERM);
    (void)waitpid(child, NULL, 0);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Close bridge_fd - child has it via fork
  close(bridge_fd);

  // Wait for exec-status: EOF means exec succeeded, data means exec failed.
  // This ensures we don't send OK until the bridge binary has actually started.
  int exec_status_fd = exec_status_pipe[0];
  int exec_status_sel = -1;
  for (;;)
  {
    fd_set rfds;
    FD_ZERO(&rfds);
    FD_SET(exec_status_fd, &rfds);
    struct timeval tv;
    tv.tv_sec = BRIDGE_START_TIMEOUT_MS / 1000;
    tv.tv_usec = (BRIDGE_START_TIMEOUT_MS % 1000) * 1000;

    exec_status_sel = select(exec_status_fd + 1, &rfds, NULL, NULL, &tv);
    if (exec_status_sel < 0 && errno == EINTR)
      continue;
    break;
  }

  if (exec_status_sel == 0)
  {
    journal_errorf("bridge exec timed out after %d ms", BRIDGE_START_TIMEOUT_MS);
    close(exec_status_fd);
    kill(child, SIGKILL);
    while (waitpid(child, NULL, 0) < 0 && errno == EINTR)
    {
    }
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "bridge start timeout", NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  if (exec_status_sel < 0)
  {
    journal_errorf("exec-status wait failed: %m");
    close(exec_status_fd);
    kill(child, SIGKILL);
    while (waitpid(child, NULL, 0) < 0 && errno == EINTR)
    {
    }
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "bridge exec status failed", NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  uint8_t exec_status_byte = 0;
  ssize_t exec_status_n = -1;
  do
  {
    exec_status_n = read(exec_status_fd, &exec_status_byte, 1);
  } while (exec_status_n < 0 && errno == EINTR);
  close(exec_status_fd);

  if (exec_status_n > 0)
  {
    // Child wrote error byte - exec failed
    journal_errorf("bridge exec failed (status byte: %d)", exec_status_byte);
    send_response(output_fd, PROTO_STATUS_ERROR, 0, "bridge exec failed", NULL);
    // Child already exited, but wait to reap
    (void)waitpid(child, NULL, 0);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }
  // exec_status_n == 0 means EOF = exec succeeded (CLOEXEC closed the pipe)
  // exec_status_n < 0 is read error, but exec likely succeeded anyway

  // Trim trailing newline from MOTD if present
  if (appdata.motd_len > 0 && appdata.motd[appdata.motd_len - 1] == '\n')
  {
    appdata.motd[appdata.motd_len - 1] = '\0';
  }

  // Now we know bridge exec'd successfully - send OK response
  // Bridge inherits the connection via FD 3, server continues Yamux on same connection
  send_response(output_fd, PROTO_STATUS_OK, mode,
                NULL, appdata.motd_len > 0 ? appdata.motd : NULL);

  // Don't close input_fd/output_fd - the bridge (child) has the connection via FD 3
  // The parent's copy will be closed when we exit, which is fine

  journal_infof("auth: bridge spawned for user '%s' mode=%s", user,
                mode == PROTO_MODE_PRIVILEGED ? "privileged" : "unprivileged");

  int status = 0;
  while (waitpid(child, &status, 0) < 0 && errno == EINTR)
  {
  }

  int exitcode = 1;
  if (WIFEXITED(status))
    exitcode = WEXITSTATUS(status);
  else if (WIFSIGNALED(status))
    exitcode = 128 + WTERMSIG(status);

  if (exitcode != 0)
  {
    journal_errorf("bridge exited with status %d", exitcode);
  }

  pam_close_session(pamh, 0);
  pam_setcred(pamh, PAM_DELETE_CRED);
  pam_end(pamh, 0);

  return exitcode;
}

// -------- main ----------
int main(int argc, char *argv[])
{
  // Handle --version before any other checks
  if (argc == 2 && (strcmp(argv[0], "--version") == 0 || strcmp(argv[1], "--version") == 0 ||
                    strcmp(argv[1], "version") == 0))
  {
#ifdef LINUXIO_VERSION
    printf("LinuxIO Auth %s\n", LINUXIO_VERSION);
#else
    printf("LinuxIO Auth (version unknown)\n");
#endif
    return 0;
  }

  if (geteuid() != 0)
  {
    log_stderrf("must run as root (via systemd or sudo)");
    return 126;
  }
  (void)prctl(PR_SET_DUMPABLE, 0);

  if (isatty(STDIN_FILENO))
  {
    log_stderrf("this command is not meant to be run from the console");
    return 2;
  }

  // Best-effort socket timeouts (stdin/stdout are the accepted socket)
  struct timeval tv_read = {.tv_sec = SOCKET_READ_TIMEOUT, .tv_usec = 0};
  struct timeval tv_write = {.tv_sec = SOCKET_WRITE_TIMEOUT, .tv_usec = 0};
  (void)setsockopt(STDIN_FILENO, SOL_SOCKET, SO_RCVTIMEO, &tv_read, sizeof(tv_read));
  (void)setsockopt(STDOUT_FILENO, SOL_SOCKET, SO_SNDTIMEO, &tv_write, sizeof(tv_write));

  // Defense-in-depth: verify peer credentials before processing
  // This catches socket permission mistakes at the kernel level
  if (check_peer_creds(STDIN_FILENO) != 0)
  {
    return 1;
  }

  return handle_client(STDIN_FILENO, STDOUT_FILENO);
}

// write_all - needed by log_stderrf and send_response
static int write_all(int fd, const void *buf, size_t len)
{
  const unsigned char *p = (const unsigned char *)buf;
  while (len > 0)
  {
    ssize_t n = write(fd, p, len);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    p += (size_t)n;
    len -= (size_t)n;
  }
  return 0;
}
