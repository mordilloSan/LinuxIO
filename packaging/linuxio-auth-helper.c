// /usr/local/bin/linuxio-auth-helper  (install 4755 root:root)
#define __STDC_WANT_LIB_EXT1__ 1
#define _GNU_SOURCE
#include <security/pam_appl.h>
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
#ifndef PR_SET_NO_NEW_PRIVS
#define PR_SET_NO_NEW_PRIVS 38
#endif

#ifdef __has_include
#if __has_include(<systemd/sd-journal.h>)
#include <systemd/sd-journal.h>
#define HAVE_SD_JOURNAL 1
#endif
#endif

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
extern char **environ;

// --- forward decls ---
static int write_all(int fd, const void *buf, size_t len);
static int env_get_int(const char *name, int defval, int minv, int maxv);

// Max lengths for environment variables
#define MAX_USERNAME_LEN 256
#define MAX_PATH_LEN 4096
#define MAX_ENV_VALUE_LEN 8192

// -------- safe formatting helpers (C11 Annex K if available) --------
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
// Prefer the fortified builtin when available; falls back to vsnprintf otherwise.
#if defined(__GNUC__) && !defined(__clang_analyzer__)
  int n = __builtin___vsnprintf_chk(dst, dstsz, 0 /* _FORTIFY */, dstsz, fmt, ap);
#else
  int n = vsnprintf(dst, dstsz, fmt, ap); // NOLINT(clang-analyzer-security.insecureAPI.DeprecatedOrUnsafeBufferHandling)
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

// -------- safe bounded copy helpers --------
static size_t min_size(size_t a, size_t b) { return a < b ? a : b; }

static void bounded_copy_bytes(void *vdst, size_t dstsz, const void *vsrc, size_t n)
{
  if (!vdst || !vsrc || dstsz == 0)
    return;
  size_t to_copy = min_size(n, dstsz);
  unsigned char *dst = (unsigned char *)vdst;
  const unsigned char *src = (const unsigned char *)vsrc;
  for (size_t i = 0; i < to_copy; ++i)
    dst[i] = src[i];
}

static void bounded_copy_cstr(char *dst, size_t dstsz, const char *src, size_t n_max)
{
  if (!dst || dstsz == 0)
    return;
  size_t srclen = src ? strnlen(src, n_max) : 0;
  size_t to_copy = (srclen < dstsz - 1) ? srclen : (dstsz - 1);
  if (src && to_copy > 0)
    bounded_copy_bytes(dst, to_copy, src, to_copy);
  dst[to_copy] = '\0';
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

// -------- stderr logger (bounded) --------
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

// -------- journald helpers --------
static void journal_errorf(const char *fmt, ...)
{
  char buf[512];
  va_list ap;
  va_start(ap, fmt);
  (void)safe_vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
#ifdef HAVE_SD_JOURNAL
  (void)sd_journal_send("MESSAGE=%s", buf, "PRIORITY=%i", LOG_ERR,
                        "SYSLOG_IDENTIFIER=linuxio-auth-helper", NULL);
#else
  openlog("linuxio-auth-helper", LOG_PID, LOG_AUTHPRIV);
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
                        "SYSLOG_IDENTIFIER=linuxio-auth-helper", NULL);
#else
  openlog("linuxio-auth-helper", LOG_PID, LOG_AUTHPRIV);
  syslog(LOG_INFO, "%s", buf);
  closelog();
#endif
}

// -------- PAM conversation ----
static int pam_conv_func(int n, const struct pam_message **msg, struct pam_response **resp, void *appdata_ptr)
{
  const char *password = (const char *)appdata_ptr;
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
      if (password)
      {
        r[i].resp = strdup(password);
        if (!r[i].resp)
        {
          for (int j = 0; j < i; j++)
            free(r[j].resp);
          free(r);
          return PAM_CONV_ERR;
        }
      }
      break;
    case PAM_PROMPT_ECHO_ON:
    case PAM_ERROR_MSG:
    case PAM_TEXT_INFO:
    default:
      break;
    }
  }
  *resp = r;
  return PAM_SUCCESS;
}

// -------- privilege drop with verification -------
static void drop_to_user(const struct passwd *pw)
{
  if (setgroups(0, NULL) != 0)
  {
    perror("setgroups");
    _exit(127);
  }
  if (initgroups(pw->pw_name, pw->pw_gid) != 0)
  {
    perror("initgroups");
    _exit(127);
  }
  if (setgid(pw->pw_gid) != 0)
  {
    perror("setgid");
    _exit(127);
  }
  if (setuid(pw->pw_uid) != 0)
  {
    perror("setuid");
    _exit(127);
  }

  // FIX #4: Verify privileges cannot be regained
  if (setuid(0) == 0)
  {
    log_stderrf("SECURITY: privilege drop failed - can regain root!");
    _exit(127);
  }
  if (getuid() != pw->pw_uid || geteuid() != pw->pw_uid)
  {
    log_stderrf("SECURITY: uid mismatch after drop");
    _exit(127);
  }
  if (getgid() != pw->pw_gid || getegid() != pw->pw_gid)
  {
    log_stderrf("SECURITY: gid mismatch after drop");
    _exit(127);
  }
}

// -------- env builder ----------
static void minimal_env(const struct passwd *pw, const char *envmode, const char *bridge_bin,
                        int set_xdg,
                        const char *sess_id, const char *sess_user, const char *sess_uid,
                        const char *sess_gid, const char *sess_secret,
                        const char *server_base, const char *server_cert)
{
  clearenv();
  if (pw)
  {
    setenv("HOME", pw->pw_dir, 1);
    setenv("USER", pw->pw_name, 1);
    setenv("LOGNAME", pw->pw_name, 1);
  }
  setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
  setenv("LANG", "C", 1);
  setenv("LC_ALL", "C", 1);
  if (envmode)
    setenv("LINUXIO_ENV", envmode, 1);
  if (bridge_bin)
    setenv("LINUXIO_BRIDGE_BIN", bridge_bin, 1);

  if (sess_id)
    setenv("LINUXIO_SESSION_ID", sess_id, 1);
  if (sess_user)
    setenv("LINUXIO_SESSION_USER", sess_user, 1);
  if (sess_uid)
    setenv("LINUXIO_SESSION_UID", sess_uid, 1);
  if (sess_gid)
    setenv("LINUXIO_SESSION_GID", sess_gid, 1);
  if (sess_secret)
    setenv("LINUXIO_BRIDGE_SECRET", sess_secret, 1);
  if (server_base)
    setenv("LINUXIO_SERVER_BASE_URL", server_base, 1);
  if (server_cert)
    setenv("LINUXIO_SERVER_CERT", server_cert, 1);

  if (set_xdg && pw)
  {
    char xdg[64];
    safe_snprintf(xdg, sizeof(xdg), "/run/user/%u", (unsigned)pw->pw_uid);
    setenv("XDG_RUNTIME_DIR", xdg, 1);
  }
}

// -------- read line from stdin -
static char *readline_stdin(size_t max)
{
  char *buf = malloc(max);
  if (!buf)
    return NULL;
  size_t i = 0;
  int c;
  while (i + 1 < max && (c = fgetc(stdin)) != EOF && c != '\n')
    buf[i++] = (char)c;
  buf[i] = '\0';
  return buf;
}

static char *readline_stdin_timeout(size_t max, int timeout_sec)
{
  if (timeout_sec <= 0)
    return readline_stdin(max);
  fd_set fds;
  FD_ZERO(&fds);
  FD_SET(STDIN_FILENO, &fds);
  struct timeval tv = {.tv_sec = timeout_sec, .tv_usec = 0};
  int r = select(STDIN_FILENO + 1, &fds, NULL, NULL, &tv);
  if (r <= 0)
    return NULL;
  return readline_stdin(max);
}

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

// FIX #7: Use FD-based operations for directory creation
static int ensure_runtime_dirs(const struct passwd *pw, gid_t *out_linuxio_gid)
{
  if (!pw)
  {
    journal_errorf("runtime: no passwd");
    return -1;
  }

  const char *base = "/run/linuxio";
  mode_t old_umask = umask(0);

  gid_t linuxio_gid = 0;
  struct group *gr = getgrnam("linuxio");
  if (gr)
    linuxio_gid = gr->gr_gid;
  if (out_linuxio_gid)
    *out_linuxio_gid = linuxio_gid;

  // Open /run for use with *at() functions
  int run_fd = open("/run", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
  if (run_fd < 0)
  {
    journal_errorf("runtime: open /run failed: %m");
    umask(old_umask);
    return -1;
  }

  // Create /run/linuxio using mkdirat
  if (mkdirat(run_fd, "linuxio", 02771) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdirat(%s) failed: %m", base);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  // Open base directory with O_NOFOLLOW
  int base_fd = openat(run_fd, "linuxio", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (base_fd < 0)
  {
    journal_errorf("runtime: openat(%s) failed: %m", base);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  struct stat st;
  if (fstat(base_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: fstat(%s) failed or not dir: %m", base);
    close(base_fd);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  if (st.st_uid != 0 || (st.st_mode & S_IWOTH))
  {
    journal_errorf("runtime: base %s unsafe (uid=%u mode=%o)",
                   base, (unsigned)st.st_uid, st.st_mode & 07777);
    close(base_fd);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  if (fchown(base_fd, 0, linuxio_gid) != 0)
  {
    // If we couldn't change ownership, verify it's already correct; otherwise fail.
    struct stat st2;
    if (fstat(base_fd, &st2) != 0 || st2.st_uid != 0 || st2.st_gid != linuxio_gid)
    {
      journal_errorf("runtime: fchown(%s) to 0:%u failed: %m", base, (unsigned)linuxio_gid);
      close(base_fd);
      close(run_fd);
      umask(old_umask);
      return -1;
    }
  }

  (void)fchmod(base_fd, 02771);

  // Create user directory using FD operations
  char uid_str[32];
  safe_snprintf(uid_str, sizeof(uid_str), "%u", (unsigned)pw->pw_uid);

  if (mkdirat(base_fd, uid_str, 02770) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdirat(%s/%s) failed: %m", base, uid_str);
    close(base_fd);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  int user_fd = openat(base_fd, uid_str, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (user_fd < 0)
  {
    journal_errorf("runtime: openat(%s/%s) failed: %m", base, uid_str);
    close(base_fd);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  if (fstat(user_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: fstat userdir failed or not dir: %m");
    close(user_fd);
    close(base_fd);
    close(run_fd);
    umask(old_umask);
    return -1;
  }

  if (st.st_uid != pw->pw_uid || st.st_gid != linuxio_gid)
  {
    if (fchown(user_fd, pw->pw_uid, linuxio_gid) != 0)
    {
      journal_errorf("runtime: fchown userdir to %u:%u failed: %m",
                     (unsigned)pw->pw_uid, (unsigned)linuxio_gid);
      if (st.st_uid != pw->pw_uid)
      {
        close(user_fd);
        close(base_fd);
        close(run_fd);
        umask(old_umask);
        return -1;
      }
    }
    if (fstat(user_fd, &st) != 0)
    {
      journal_errorf("runtime: fstat userdir after chown failed: %m");
      close(user_fd);
      close(base_fd);
      close(run_fd);
      umask(old_umask);
      return -1;
    }
  }

  if (fchmod(user_fd, 02770) != 0)
  {
    journal_errorf("runtime: fchmod userdir failed: %m");
    if (st.st_mode & S_IWOTH)
    {
      close(user_fd);
      close(base_fd);
      close(run_fd);
      umask(old_umask);
      return -1;
    }
  }

  close(user_fd);
  close(base_fd);
  close(run_fd);
  umask(old_umask);
  return 0;
}

// FIX #2: Improved TOCTOU protection - validate via single FD
static int validate_bridge_via_fd(int fd, uid_t required_owner)
{
  struct stat st;
  if (fstat(fd, &st) != 0)
  {
    perror("fstat bridge");
    return -1;
  }
  if (!S_ISREG(st.st_mode))
  {
    log_stderrf("bridge not regular file");
    return -1;
  }
  if ((st.st_mode & (S_IWGRP | S_IWOTH)) != 0)
  {
    log_stderrf("bridge is group/world-writable");
    return -1;
  }
  if (st.st_uid != required_owner)
  {
    log_stderrf("bridge owner mismatch: expect uid %u, got %u",
                (unsigned)required_owner, (unsigned)st.st_uid);
    return -1;
  }
  if ((st.st_mode & 0111) == 0)
  {
    log_stderrf("bridge is not executable");
    return -1;
  }
  if (st.st_mode & (S_ISUID | S_ISGID))
  {
    log_stderrf("bridge must not be setuid/setgid");
    return -1;
  }
  return 0;
}

// Replace your validate_parent_dir_via_fd() with:

static int validate_parent_dir_policy(const struct stat *ds, uid_t file_owner, uid_t user_uid)
{
  if (!S_ISDIR(ds->st_mode))
    return -1;

  if (file_owner == 0)
  { // root-owned bridge file
    if (ds->st_uid != 0)
      return -1;
    if (ds->st_mode & (S_IWGRP | S_IWOTH))
      return -1;
    return 0;
  }

  if (file_owner == user_uid)
  { // user-owned bridge file
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

// FIX #2: Open and validate bridge using FD operations only
static int open_and_validate_bridge(const char *bridge_path, uid_t required_owner, int *out_fd)
{
  // Open the file with O_PATH and O_NOFOLLOW
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
  // Validate the file via FD
  if (validate_bridge_via_fd(fd, required_owner) != 0)
  {
    close(fd);
    return -1;
  }

  // Resolve and validate parent directory
  char linkbuf[PATH_MAX];
  char fdlink[64];
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

  int dir_ok = validate_parent_dir_via_fd(dfd, st.st_uid, required_owner /* user uid or 0 */);
  close(dfd);

  if (dir_ok != 0)
  {
    log_stderrf("bridge parent directory unsafe");
    close(fd);
    return -1;
  }

  *out_fd = fd;
  return 0;
}

static int exec_bridge_via_fd(int fd, const char *bridge_path, char *const argv[])
{
#if defined(SYS_execveat)
  if (syscall(SYS_execveat, fd, "", argv, environ, AT_EMPTY_PATH) == -1)
  {
    if (errno == ENOSYS)
    {
      log_stderrf("execveat(%s) not supported on this system", bridge_path);
      journal_errorf("SECURITY: execveat unavailable for %s, cannot safely exec bridge", bridge_path);
      close(fd);
      return -1;
    }
    int saved = errno;
    log_stderrf("execveat(%s) failed: %s", bridge_path, strerror(saved));
    journal_errorf("execveat(%s) failed: %s", bridge_path, strerror(saved));
    close(fd);
    errno = saved;
    return -1;
  }
  /* not reached */
#else
  log_stderrf("execveat(%s) not compiled in", bridge_path);
  journal_errorf("SECURITY: execveat support not compiled, cannot safely exec bridge %s", bridge_path);
  close(fd);
  errno = ENOSYS;
  return -1;
#endif
  return -1; // placate “all control paths” analyzers
}

// Ensure FDs are closed on all paths
static int redirect_bridge_output(uid_t owner_uid, gid_t linuxio_gid, const char *sess_id)
{
  char sid8[9] = {0};
  if (sess_id && *sess_id)
  {
    bounded_copy_cstr(sid8, sizeof(sid8), sess_id, 8);
  }
  else
  {
    bounded_copy_cstr(sid8, sizeof(sid8), "nosessid", 8);
  }

#ifdef HAVE_SD_JOURNAL
  int jfd = sd_journal_stream_fd("linuxio-bridge", LOG_INFO, 1);
  if (jfd >= 0)
  {
    if (dup2(jfd, STDOUT_FILENO) < 0)
    {
      journal_errorf("dup2 stdout->journald failed: %m");
      close(jfd);
      return -1;
    }
    if (dup2(jfd, STDERR_FILENO) < 0)
    {
      journal_errorf("dup2 stderr->journald failed: %m");
      close(jfd);
      return -1;
    }
    close(jfd);
    return 0;
  }
  journal_errorf("sd_journal_stream_fd failed; falling back to file log");
#endif

  char dir[PATH_MAX], path[PATH_MAX];
  safe_snprintf(dir, sizeof(dir), "/run/linuxio/%u", (unsigned)owner_uid);
  safe_snprintf(path, sizeof(path), "%s/bridge-%s.log", dir, sid8);

  int fd = open(path, O_CREAT | O_WRONLY | O_APPEND | O_CLOEXEC, 0640);
  if (fd < 0)
  {
    journal_errorf("open %s failed: %m; falling back to /dev/null", path);
    int devnull = open("/dev/null", O_WRONLY | O_CLOEXEC);
    if (devnull >= 0)
    {
      (void)dup2(devnull, STDOUT_FILENO);
      (void)dup2(devnull, STDERR_FILENO);
      close(devnull);
      return 0;
    }
    return -1;
  }

  if (fchown(fd, owner_uid, linuxio_gid) != 0)
  {
    journal_errorf("redirect: fchown(log) to %u:%u failed: %m",
                   (unsigned)owner_uid, (unsigned)linuxio_gid);
    // Non-fatal: we still try to dup the fd; perms may already be acceptable.
  }

  int dup_ok = 1;
  if (dup2(fd, STDOUT_FILENO) < 0)
  {
    journal_errorf("dup2 stdout->file failed: %m");
    dup_ok = 0;
  }
  if (dup2(fd, STDERR_FILENO) < 0)
  {
    journal_errorf("dup2 stderr->file failed: %m");
    dup_ok = 0;
  }
  close(fd); // FIX #9: Always close

  return dup_ok ? 0 : -1;
}

static int run_cmd_as_user_with_input(const struct passwd *pw,
                                      char *const argv[],
                                      const char *stdin_data,
                                      int timeout_sec)
{
  int inpipe[2] = {-1, -1};
#if defined(HAVE_PIPE2) || (defined(__linux__) && defined(O_CLOEXEC))
  if (pipe2(inpipe, O_CLOEXEC) != 0)
    return -1;
#else
  if (pipe(inpipe) != 0)
    return -1;
  (void)fcntl(inpipe[0], F_SETFD, fcntl(inpipe[0], F_GETFD) | FD_CLOEXEC);
  (void)fcntl(inpipe[1], F_SETFD, fcntl(inpipe[1], F_GETFD) | FD_CLOEXEC);
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
    // Child: drop to user
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
    setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
    setenv("LANG", "C", 1);

    execv("/usr/bin/sudo", argv);
    _exit(127);
  }

  // Parent
  close(inpipe[0]);

  if (stdin_data && *stdin_data)
  {
    (void)write_all(inpipe[1], stdin_data, strlen(stdin_data));
  }
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
  if (out_nopasswd)
    *out_nopasswd = 0;

  int to_nopw = env_get_int("LINUXIO_SUDO_TIMEOUT_NOPASSWD", 3, 1, 30);
  int to_pw = env_get_int("LINUXIO_SUDO_TIMEOUT_PASSWORD", 4, 1, 30);

  char *argv_nopw[] = {"/usr/bin/sudo", "-n", "-v", NULL};
  int rc = run_cmd_as_user_with_input(pw, argv_nopw, NULL, to_nopw);
  if (rc == 0)
  {
    if (out_nopasswd)
      *out_nopasswd = 1;
    char *argv_sudo_k[] = {"/usr/bin/sudo", "-k", NULL};
    (void)run_cmd_as_user_with_input(pw, argv_sudo_k, NULL, 2);
    return 1;
  }

  if (password && *password)
  {
    char *argv_pw[] = {"/usr/bin/sudo", "-S", "-p", "", "-v", NULL};
    char buf[1024];
    (void)safe_snprintf(buf, sizeof(buf), "%s\n", password);
    rc = run_cmd_as_user_with_input(pw, argv_pw, buf, to_pw);
    secure_bzero(buf, sizeof(buf));
    if (rc == 0)
    {
      char *argv_sudo_k[] = {"/usr/bin/sudo", "-k", NULL};
      (void)run_cmd_as_user_with_input(pw, argv_sudo_k, NULL, 2);
      return 1;
    }
  }
  return 0; // not privileged
}

// FIX #8: Set resource limits before forking
static void set_resource_limits(void)
{
  struct rlimit rl;

  rl.rlim_cur = rl.rlim_max = 64;
  (void)setrlimit(RLIMIT_CPU, &rl);
  (void)setrlimit(RLIMIT_NOFILE, &rl);

  int nproc_limit = env_get_int("LINUXIO_RLIMIT_NPROC", 512, 10, 2048);
  rl.rlim_cur = rl.rlim_max = nproc_limit;
  (void)setrlimit(RLIMIT_NPROC, &rl);

  rl.rlim_cur = rl.rlim_max = 1UL * 1024 * 1024 * 1024;
  (void)setrlimit(RLIMIT_FSIZE, &rl);

  rl.rlim_cur = rl.rlim_max = 16UL * 1024 * 1024 * 1024;
  (void)setrlimit(RLIMIT_AS, &rl);

  rl.rlim_cur = rl.rlim_max = 0;
  (void)setrlimit(RLIMIT_CORE, &rl);
}

// FIX #6: Validate environment variable length
static char *safe_getenv_strdup(const char *name, size_t max_len)
{
  const char *val = getenv(name);
  if (!val || !*val)
    return NULL;

  size_t len = strnlen(val, max_len + 1);
  if (len > max_len)
  {
    journal_errorf("env var %s too long (%zu > %zu)", name, len, max_len);
    return NULL;
  }

  return strdup(val);
}

// -------- main ----------
int main(void)
{
  if (geteuid() != 0)
  {
    log_stderrf("must be setuid root");
    return 126;
  }

  // --- PATCH: prevent core dumps/ptrace on the helper itself ---
  (void)prctl(PR_SET_DUMPABLE, 0);

  // Validate environment variable lengths
  char *user = safe_getenv_strdup("LINUXIO_SESSION_USER", MAX_USERNAME_LEN);
  if (!user)
  {
    log_stderrf("missing or invalid LINUXIO_SESSION_USER");
    return 2;
  }

  // password from stdin (or env for testing)
  char *password = NULL;
  const char *env_pw = getenv("LINUXIO_PASSWORD");
  if (env_pw && *env_pw)
  {
    password = safe_getenv_strdup("LINUXIO_PASSWORD", 1024);
    unsetenv("LINUXIO_PASSWORD");
  }
  else
  {
    int pw_to = env_get_int("LINUXIO_PASSWORD_TIMEOUT", 10, 1, 60);
    password = readline_stdin_timeout(1024, pw_to);
  }
  if (!password || !*password)
  {
    log_stderrf("missing password on stdin/env");
    free(password);
    free(user);
    return 2;
  }

  // PAM auth
  struct pam_conv conv = {pam_conv_func, (void *)password};
  pam_handle_t *pamh = NULL;
  int rc = pam_start("linuxio", user, &conv, &pamh);
  if (rc != PAM_SUCCESS)
  {
    log_stderrf("pam_start: %s", pam_strerror(NULL, rc));
    secure_bzero(password, strlen(password));
    free(password);
    free(user);
    return 5;
  }

  (void)pam_set_item(pamh, PAM_RHOST, "web");
  rc = pam_authenticate(pamh, 0);
  if (rc == PAM_SUCCESS)
    rc = pam_acct_mgmt(pamh, 0);
  if (rc == PAM_SUCCESS)
    rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);
  if (rc != PAM_SUCCESS)
  {
    log_stderrf("%s", pam_strerror(pamh, rc));
    pam_end(pamh, rc);
    if (password)
    {
      secure_bzero(password, strlen(password));
      free(password);
    }
    free(user);
    return 1;
  }

  rc = pam_open_session(pamh, 0);
  if (rc != PAM_SUCCESS)
  {
    log_stderrf("open_session: %s", pam_strerror(pamh, rc));
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password)
    {
      secure_bzero(password, strlen(password));
      free(password);
    }
    free(user);
    return 5;
  }

  struct passwd *pw = getpwnam(user);
  if (!pw)
  {
    perror("getpwnam");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password)
    {
      secure_bzero(password, strlen(password));
      free(password);
    }
    free(user);
    return 5;
  }

  gid_t linuxio_gid = 0;
  if (ensure_runtime_dirs(pw, &linuxio_gid) != 0)
  {
    log_stderrf("prepare runtime dir failed");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password)
    {
      secure_bzero(password, strlen(password));
      free(password);
    }
    free(user);
    return 5;
  }

  int want_privileged = 0;
  int nopasswd = 0;
  want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  char deduced_uid_str[32];
  char deduced_gid_str[32];
  safe_snprintf(deduced_uid_str, sizeof(deduced_uid_str), "%u", (unsigned)pw->pw_uid);
  safe_snprintf(deduced_gid_str, sizeof(deduced_gid_str), "%u", (unsigned)pw->pw_gid);

  // FIX #6: Validate all environment variables with length limits
  char *envmode_in = safe_getenv_strdup("LINUXIO_ENV", 128);
  char *bridge_in = safe_getenv_strdup("LINUXIO_BRIDGE_BIN", MAX_PATH_LEN);
  char *sess_id = safe_getenv_strdup("LINUXIO_SESSION_ID", 256);
  char *sess_user = safe_getenv_strdup("LINUXIO_SESSION_USER", MAX_USERNAME_LEN);
  char *sess_secret = safe_getenv_strdup("LINUXIO_BRIDGE_SECRET", MAX_ENV_VALUE_LEN);
  char *server_base = safe_getenv_strdup("LINUXIO_SERVER_BASE_URL", MAX_ENV_VALUE_LEN);
  char *server_cert = safe_getenv_strdup("LINUXIO_SERVER_CERT", MAX_ENV_VALUE_LEN);
  char *verbose_in = safe_getenv_strdup("LINUXIO_VERBOSE", 16);

  const char *envmode = (envmode_in && *envmode_in) ? envmode_in : "production";
  const char *bridge_path = (bridge_in && *bridge_in) ? bridge_in : "/usr/local/bin/linuxio-bridge";

  // --- PATCH: require absolute path for bridge ---
  if (bridge_path[0] != '/')
  {
    log_stderrf("bridge path must be absolute");
    // mirror the same cleanup path as other early failures
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    free(user);
    free(envmode_in);
    free(bridge_in);
    free(sess_id);
    free(sess_user);
    free(sess_secret);
    free(server_base);
    free(server_cert);
    free(verbose_in);
    return 5;
  }

  int verbose = 0;
  if (verbose_in && *verbose_in)
  {
    if (!strcasecmp(verbose_in, "1") ||
        !strcasecmp(verbose_in, "true") ||
        !strcasecmp(verbose_in, "yes") ||
        !strcasecmp(verbose_in, "on"))
    {
      verbose = 1;
    }
  }

  // Wipe password now
  if (password)
  {
    secure_bzero(password, strlen(password));
    free(password);
    password = NULL;
  }

  // FIX #2: Open and validate bridge using FD operations
  int bridge_fd = -1;

  // Allow bridge to be owned by either root or user in unprivileged mode
  if (!want_privileged)
  {
    // Try user ownership first
    if (open_and_validate_bridge(bridge_path, pw->pw_uid, &bridge_fd) != 0)
    {
      // Try root ownership as fallback
      if (open_and_validate_bridge(bridge_path, 0, &bridge_fd) != 0)
      {
        log_stderrf("bridge validation failed");
        pam_close_session(pamh, 0);
        pam_setcred(pamh, PAM_DELETE_CRED);
        pam_end(pamh, 0);
        free(user);
        free(envmode_in);
        free(bridge_in);
        free(sess_id);
        free(sess_user);
        free(sess_secret);
        free(server_base);
        free(server_cert);
        free(verbose_in);
        return 5;
      }
    }
  }
  else
  {
    if (open_and_validate_bridge(bridge_path, 0, &bridge_fd) != 0)
    {
      log_stderrf("bridge validation failed");
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      free(user);
      free(envmode_in);
      free(bridge_in);
      free(sess_id);
      free(sess_user);
      free(sess_secret);
      free(server_base);
      free(server_cert);
      free(verbose_in);
      return 5;
    }
  }

  const char *mode = want_privileged ? "MODE=privileged\n" : "MODE=unprivileged\n";
  (void)write_all(STDOUT_FILENO, mode, strlen(mode));

  // FIX #8: Set resource limits before forking
  set_resource_limits();

  pid_t nanny = fork();
  if (nanny < 0)
  {
    perror("fork nanny");
    close(bridge_fd); // FIX #9
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    free(user);
    free(envmode_in);
    free(bridge_in);
    free(sess_id);
    free(sess_user);
    free(sess_secret);
    free(server_base);
    free(server_cert);
    free(verbose_in);
    return 5;
  }

  if (nanny == 0)
  {
    // Nanny process
    (void)prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0);
    pid_t child = fork();
    if (child < 0)
    {
      perror("fork bridge");
      close(bridge_fd); // FIX #9
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      _exit(5);
    }

    if (child == 0)
    {
      // Bridge child
      umask(077);

      if (want_privileged)
      {
        minimal_env(NULL, envmode, bridge_path, 0,
                    sess_id, sess_user, deduced_uid_str, deduced_gid_str, sess_secret,
                    server_base, server_cert);
        setenv("HOME", "/root", 1);
        setenv("USER", "root", 1);
        setenv("LOGNAME", "root", 1);

        if (setgroups(0, NULL) != 0)
        {
          perror("setgroups");
          _exit(127);
        }
        if (setresgid(0, 0, 0) != 0)
        {
          perror("setresgid");
          _exit(127);
        }
        if (setresuid(0, 0, 0) != 0)
        {
          perror("setresuid");
          _exit(127);
        }
      }
      else
      {
        drop_to_user(pw); // Now includes verification
        minimal_env(pw, envmode, bridge_path, 1,
                    sess_id, sess_user, deduced_uid_str, deduced_gid_str, sess_secret,
                    server_base, server_cert);
        if (chdir(pw->pw_dir) != 0)
        {
          perror("chdir");
          _exit(127);
        }
      }

      if (verbose)
        setenv("LINUXIO_VERBOSE", "1", 1);

      uid_t owner_uid = pw->pw_uid;
      if (redirect_bridge_output(owner_uid, linuxio_gid, sess_id) != 0)
      {
        int devnull = open("/dev/null", O_WRONLY | O_CLOEXEC);
        if (devnull >= 0)
        {
          (void)dup2(devnull, STDOUT_FILENO);
          (void)dup2(devnull, STDERR_FILENO);
          close(devnull);
        }
      }

      {
        uid_t r, e, s;
        gid_t gr, ge, gs;
        getresuid(&r, &e, &s);
        getresgid(&gr, &ge, &gs);
        journal_infof("pre-exec bridge: want_priv=%d ruid=%d euid=%d suid=%d rgid=%d egid=%d sgid=%d path=%s",
                      want_privileged, (int)r, (int)e, (int)s, (int)gr, (int)ge, (int)gs, bridge_path);
      }

      char *argv_child[6];
      int ai = 0;
      argv_child[ai++] = (char *)bridge_path;
      argv_child[ai++] = "--env";
      argv_child[ai++] = (char *)envmode;
      if (verbose)
        argv_child[ai++] = "--verbose";
      argv_child[ai++] = NULL;

      exec_bridge_via_fd(bridge_fd, bridge_path, argv_child);
      perror("exec linuxio-bridge");
      _exit(127);
    }

    // Nanny waits for bridge
    close(bridge_fd); // FIX #9
    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR)
    {
    }
    // FIX #5: Ensure PAM cleanup
    (void)pam_close_session(pamh, 0);
    (void)pam_setcred(pamh, PAM_DELETE_CRED);
    (void)pam_end(pamh, 0);
    _exit(WIFEXITED(status)     ? WEXITSTATUS(status)
          : WIFSIGNALED(status) ? 128 + WTERMSIG(status)
                                : 1);
  }

  // Original parent
  close(bridge_fd); // FIX #9
  (void)write_all(STDOUT_FILENO, "OK\n", 3);
  (void)close(STDIN_FILENO);
  (void)close(STDOUT_FILENO);
  (void)close(STDERR_FILENO);

  // Clean up allocated strings
  free(user);
  free(envmode_in);
  free(bridge_in);
  free(sess_id);
  free(sess_user);
  free(sess_secret);
  free(server_base);
  free(server_cert);
  free(verbose_in);

  _exit(0);
}

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