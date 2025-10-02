// /usr/local/bin/linuxio-auth-helper  (install 4755 root:root)
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

#ifdef __has_include
#if __has_include(<systemd/sd-journal.h>)
#include <systemd/sd-journal.h>
#define HAVE_SD_JOURNAL 1
#endif
#endif

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
extern char **environ; // to pass environment to execveat/execv

// --- forward decls ---
static int write_all(int fd, const void *buf, size_t len);
static int env_get_int(const char *name, int defval, int minv, int maxv);

// -------- safe formatting helpers (C11 Annex K if available) --------
static int safe_vsnprintf(char *dst, size_t dstsz, const char *fmt, va_list ap)
{
  if (!dst || dstsz == 0)
    return -1;
#if defined(__STDC_LIB_EXT1__)
  // Use bounds-checking variant when available (Annex K)
  int n = vsnprintf_s(dst, dstsz, _TRUNCATE, fmt, ap); // NOLINT
  if (n < 0)
  {
    dst[0] = '\0';
    return -1;
  }
  return n;
#else
  // Fallback: standard vsnprintf with explicit NUL-termination
  int n = vsnprintf(dst, dstsz, fmt, ap); // NOLINT(clang-analyzer-security.insecureAPI.DeprecatedOrUnsafeBufferHandling)
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

// -------- safe bounded copy helpers (avoid memcpy/memmove flags) --------
static size_t min_size(size_t a, size_t b) { return a < b ? a : b; }
static void bounded_copy_bytes(void *vdst, size_t dstsz, const void *vsrc, size_t n)
{
  if (!vdst || !vsrc || dstsz == 0)
    return;
  size_t to_copy = min_size(n, dstsz);
  unsigned char *dst = (unsigned char *)vdst;
  const unsigned char *src = (const unsigned char *)vsrc;
  for (size_t i = 0; i < to_copy; ++i)
    dst[i] = src[i]; // byte-wise copy
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

// Add this after journal_errorf()
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

// -------- privilege drop -------
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
  if (envmode)
    setenv("LINUXIO_ENV", envmode, 1);
  if (bridge_bin)
    setenv("LINUXIO_BRIDGE_BIN", bridge_bin, 1);

  // Session / server env needed by linuxio-bridge
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

// Optional: readline with timeout (env tunable)
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
    return NULL; // timeout or error
  return readline_stdin(max);
}

// Env int helper
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
  {
    v = minv;
  }
  if (v > maxv)
  {
    v = maxv;
  }

  return (int)v;
}

static int ensure_runtime_dirs(const struct passwd *pw, gid_t *out_linuxio_gid)
{
  if (!pw)
  {
    journal_errorf("runtime: no passwd");
    return -1;
  }

  const char *base = "/run/linuxio";
  mode_t old_umask = umask(0);

  // Resolve linuxio group (fallback gid=0 if absent)
  gid_t linuxio_gid = 0;
  struct group *gr = getgrnam("linuxio");
  if (gr)
    linuxio_gid = gr->gr_gid;
  if (out_linuxio_gid)
    *out_linuxio_gid = linuxio_gid;

  // 1) Ensure /run/linuxio exists with sane ownership/perms
  if (mkdir(base, 02771) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir(%s) failed: %m", base);
    umask(old_umask);
    return -1;
  }

  struct stat st;
  if (stat(base, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: stat(%s) failed or not dir: %m", base);
    umask(old_umask);
    return -1;
  }

  // Only fail if base is dangerous (not root-owned OR world-writable)
  if (st.st_uid != 0 || (st.st_mode & S_IWOTH))
  {
    journal_errorf("runtime: base %s unsafe (uid=%u mode=%o)",
                   base, (unsigned)st.st_uid, st.st_mode & 07777);
    umask(old_umask);
    return -1;
  }

  // Try to fix group/mode best-effort (do not fail on error)
  (void)chown(base, 0, linuxio_gid);
  (void)chmod(base, 02771);

  // 2) Ensure /run/linuxio/<uid> exists with user:linuxio and 02770
  char userdir[64];
  safe_snprintf(userdir, sizeof(userdir), "%s/%u", base, (unsigned)pw->pw_uid);

  if (mkdir(userdir, 02770) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir(%s) failed: %m", userdir);
    umask(old_umask);
    return -1;
  }

  if (stat(userdir, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: stat(%s) failed or not dir: %m", userdir);
    umask(old_umask);
    return -1;
  }

  // If ownership/group not as desired, try to fix; only fail if ends up unsafe
  if (st.st_uid != pw->pw_uid || st.st_gid != linuxio_gid)
  {
    if (chown(userdir, pw->pw_uid, linuxio_gid) != 0)
    {
      journal_errorf("runtime: chown(%s) to %u:%u failed: %m",
                     userdir, (unsigned)pw->pw_uid, (unsigned)linuxio_gid);
      // Acceptable if already user-owned and not world-writable; otherwise fail.
      if (st.st_uid != pw->pw_uid)
      {
        umask(old_umask);
        return -1;
      }
    }
    // Refresh stat after potential chown
    if (stat(userdir, &st) != 0)
    {
      journal_errorf("runtime: stat(%s) after chown failed: %m", userdir);
      umask(old_umask);
      return -1;
    }
  }

  // Ensure permissions; only fail if world-writable remains
  if (chmod(userdir, 02770) != 0)
  {
    journal_errorf("runtime: chmod(%s,02770) failed: %m", userdir);
    if (st.st_mode & S_IWOTH)
    {
      umask(old_umask);
      return -1;
    }
  }

  umask(old_umask);
  return 0;
}

// -------- validate bridge owner
static int validate_bridge_path_owned(const char *path, uid_t required_owner)
{
  struct stat st;
  if (!path)
    return -1;
  if (stat(path, &st) != 0)
  {
    perror("stat bridge");
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
    log_stderrf("bridge owner mismatch: expect uid %u", (unsigned)required_owner);
    return -1;
  }
  if ((st.st_mode & 0111) == 0)
  {
    log_stderrf("bridge is not executable");
    return -1;
  }
  return 0;
}

// Optional: ensure bridge dir is root-owned and not group/world writable
static int dir_is_safe(const char *file_path)
{
  if (!file_path || !*file_path)
    return -1;

  // Open the target path O_PATH|O_NOFOLLOW (fails if it's a symlink)
  int file_fd = open(file_path, O_PATH | O_NOFOLLOW | O_CLOEXEC);
  if (file_fd < 0)
    return -1;

  // Resolve the actual kernel path of the opened fd
  char linkbuf[PATH_MAX];
  char fdlink[64];
  safe_snprintf(fdlink, sizeof(fdlink), "/proc/self/fd/%d", file_fd);
  ssize_t n = readlink(fdlink, linkbuf, sizeof(linkbuf) - 1);
  close(file_fd);
  if (n < 0)
    return -1;
  linkbuf[n] = '\0';

  // Strip last component to get the parent directory
  char *slash = strrchr(linkbuf, '/');
  if (!slash || slash == linkbuf)
    return -1;
  *slash = '\0';

  // Open parent O_NOFOLLOW and validate ownership/permissions
  int dfd = open(linkbuf, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (dfd < 0)
    return -1;

  struct stat ds;
  int ok = 0;
  if (fstat(dfd, &ds) == 0 &&
      S_ISDIR(ds.st_mode) &&
      ds.st_uid == 0 &&
      !(ds.st_mode & (S_IWGRP | S_IWOTH)))
  {
    ok = 1;
  }
  close(dfd);
  return ok ? 0 : -1;
}

// --- replace the whole function with this version ---
static int exec_bridge_fd_checked(const char *bridge_path, char *const argv[])
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
  if (!S_ISREG(st.st_mode) || (st.st_mode & (S_IWGRP | S_IWOTH)))
  {
    close(fd);
    errno = EACCES;
    return -1;
  }

  // Try execveat by FD to avoid TOCTOU between check and exec.
#if defined(SYS_execveat)
  if (syscall(SYS_execveat, fd, "", argv, environ, AT_EMPTY_PATH) == 0)
  {
    // not reached
  }
  // If execveat is unavailable or fails with ENOSYS/EXDEV/etc, fall back:
#endif

  close(fd);
  return execv(bridge_path, argv);
}

// Redirect the future bridge child's stdout/stderr to journald; fallback to file in /run/linuxio/<uid>/bridge-<sid8>.log
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
  int jfd = sd_journal_stream_fd("linuxio-bridge", LOG_INFO, 1 /*level_prefix*/);
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
      dup2(devnull, STDOUT_FILENO);
      dup2(devnull, STDERR_FILENO);
      close(devnull);
      return 0;
    }
    return -1;
  }
  if (fchown(fd, owner_uid, linuxio_gid) != 0)
  {
    journal_errorf("fchown(%s) failed: %m", path);
  }

  if (dup2(fd, STDOUT_FILENO) < 0)
  {
    journal_errorf("dup2 stdout->file failed: %m");
    close(fd);
    return -1;
  }
  if (dup2(fd, STDERR_FILENO) < 0)
  {
    journal_errorf("dup2 stderr->file failed: %m");
    close(fd);
    return -1;
  }
  close(fd);
  return 0;
}

// ---- utilities to probe sudo capability (auto privilege decision) ----
static int run_cmd_as_user_with_input(const struct passwd *pw,
                                      char *const argv[],
                                      const char *stdin_data,
                                      int timeout_sec)
{
  int inpipe[2] = {-1, -1};
  if (pipe(inpipe) != 0)
    return -1;

  pid_t pid = fork();
  if (pid < 0)
  {
    close(inpipe[0]);
    close(inpipe[1]);
    return -1;
  }

  if (pid == 0)
  {
    // Child: drop to user, hook stdin from pipe
    if (setgroups(0, NULL) != 0)
      _exit(127);
    if (initgroups(pw->pw_name, pw->pw_gid) != 0)
      _exit(127);
    if (setgid(pw->pw_gid) != 0)
      _exit(127);
    if (setuid(pw->pw_uid) != 0)
      _exit(127);

    // stdin from read-end
    if (dup2(inpipe[0], STDIN_FILENO) < 0)
      _exit(127);
    close(inpipe[0]);
    close(inpipe[1]);

    // minimal env for sudo -v
    clearenv();
    setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
    setenv("LANG", "C", 1);

    execv("/usr/bin/sudo", argv);
    _exit(127);
  }

  // Parent
  close(inpipe[0]);

  // Feed stdin if provided
  if (stdin_data && *stdin_data)
  {
    (void)write_all(inpipe[1], stdin_data, strlen(stdin_data));
  }
  close(inpipe[1]);

  // Wait with timeout
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

  // Timeouts configurable via env; defaults preserve previous behavior
  int to_nopw = env_get_int("LINUXIO_SUDO_TIMEOUT_NOPASSWD", 3, 1, 30);
  int to_pw = env_get_int("LINUXIO_SUDO_TIMEOUT_PASSWORD", 4, 1, 30);

  // 1) NOPASSWD probe (no password)
  char *argv_nopw[] = {"/usr/bin/sudo", "-n", "-v", NULL};
  int rc = run_cmd_as_user_with_input(pw, argv_nopw, NULL, to_nopw);
  if (rc == 0)
  {
    if (out_nopasswd)
      *out_nopasswd = 1;
    return 1;
  }

  // 2) Password-based probe: sudo -S -p '' -v
  if (password && *password)
  {
    char *argv_pw[] = {"/usr/bin/sudo", "-S", "-p", "", "-v", NULL};
    char buf[1024];
    (void)safe_snprintf(buf, sizeof(buf), "%s\n", password);
    rc = run_cmd_as_user_with_input(pw, argv_pw, buf, to_pw);
    secure_bzero(buf, sizeof(buf));
    if (rc == 0)
      return 1;
  }

  return 0;
}

// -------- main ----------
int main(void)
{
  if (geteuid() != 0)
  {
    log_stderrf("must be setuid root");
    return 126;
  }

  // Copy env strings we will use later so they won't be invalidated by clearenv()
  const char *user_env = getenv("LINUXIO_TARGET_USER");
  char *user = (user_env && *user_env) ? strdup(user_env) : NULL;
  if (!user)
  {
    log_stderrf("missing LINUXIO_TARGET_USER");
    return 2;
  }

  // password from stdin (or env for testing)
  char *password = NULL;
  const char *env_pw = getenv("LINUXIO_PASSWORD");
  if (env_pw && *env_pw)
  {
    password = strdup(env_pw);
    unsetenv("LINUXIO_PASSWORD"); // reduce exposure
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

  // PAM auth + account + creds
  struct pam_conv conv = {pam_conv_func, (void *)password};
  pam_handle_t *pamh = NULL;
  int rc = pam_start("linuxio", user, &conv, &pamh);
  if (rc != PAM_SUCCESS)
  {
    log_stderrf("pam_start: %s", pam_strerror(pamh, rc));
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

  // target user info
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

  // Decide privileged mode
  int want_privileged = 0;
  int nopasswd = 0;
  want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  // ===== DEBUG: Compare ENV vs DEDUCED UID/GID =====
  char deduced_uid_str[32];
  char deduced_gid_str[32];
  safe_snprintf(deduced_uid_str, sizeof(deduced_uid_str), "%u", (unsigned)pw->pw_uid);
  safe_snprintf(deduced_gid_str, sizeof(deduced_gid_str), "%u", (unsigned)pw->pw_gid);

  // Read remaining inputs/env before exec (copy what we might need)
  const char *envmode_in = getenv("LINUXIO_ENV");
  const char *bridge_in = getenv("LINUXIO_BRIDGE_BIN");
  const char *sess_id = getenv("LINUXIO_SESSION_ID");
  const char *sess_user = getenv("LINUXIO_SESSION_USER");
  const char *sess_uid = deduced_uid_str;
  const char *sess_gid = deduced_gid_str;
  const char *sess_secret = getenv("LINUXIO_BRIDGE_SECRET");
  const char *server_base = getenv("LINUXIO_SERVER_BASE_URL");
  const char *server_cert = getenv("LINUXIO_SERVER_CERT");
  const char *verbose_in = getenv("LINUXIO_VERBOSE");

  const char *envmode = (envmode_in && *envmode_in) ? envmode_in : "production";
  const char *bridge_path = (bridge_in && *bridge_in) ? bridge_in : "/usr/local/bin/linuxio-bridge";

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

  // Now wipe the password; we no longer need it
  if (password)
  {
    secure_bzero(password, strlen(password));
    free(password);
    password = NULL;
  }

  // bridge binary validation according to decision
  if (dir_is_safe(bridge_path) != 0)
  {
    log_stderrf("unsafe bridge dir");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    free(user);
    return 5;
  }
  if (want_privileged)
  {
    if (validate_bridge_path_owned(bridge_path, 0 /*root*/) != 0)
    {
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      free(user);
      return 5;
    }
  }
  else
  {
    struct stat st;
    if (stat(bridge_path, &st) != 0 || !S_ISREG(st.st_mode) ||
        (st.st_mode & (S_IWGRP | S_IWOTH)) ||
        !(st.st_uid == 0 || st.st_uid == pw->pw_uid))
    {
      log_stderrf("bridge must be owned by root or %s and not group/world-writable", pw->pw_name);
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      free(user);
      return 5;
    }
  }

  const char *mode = want_privileged ? "MODE=privileged\n" : "MODE=unprivileged\n";
  (void)write_all(STDOUT_FILENO, mode, strlen(mode));

  // ---------- Correct double-fork: NANNY first, then BRIDGE ----------
  pid_t nanny = fork();
  if (nanny < 0)
  {
    perror("fork nanny");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    free(user);
    return 5;
  }

  if (nanny == 0)
  {
    // Nanny process: create the bridge as *its* child, then wait it and close PAM
    pid_t child = fork();
    if (child < 0)
    {
      perror("fork bridge");
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      _exit(5);
    }

    if (child == 0)
    {
      // Bridge child: set identity/env and exec
      umask(077);
      if (want_privileged)
      {
        minimal_env(NULL, envmode, bridge_path, /*set_xdg=*/0,
                    sess_id, sess_user, sess_uid, sess_gid, sess_secret,
                    server_base, server_cert);
        setenv("HOME", "/root", 1);
        setenv("USER", "root", 1);
        setenv("LOGNAME", "root", 1);

        // Ensure full root creds survive to exec:
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
        drop_to_user(pw);
        minimal_env(pw, envmode, bridge_path, /*set_xdg=*/1,
                    sess_id, sess_user, sess_uid, sess_gid, sess_secret,
                    server_base, server_cert);
        if (chdir(pw->pw_dir) != 0)
        {
          perror("chdir");
          _exit(127);
        }
      }

      // Also forward verbosity to the bridge via env
      if (verbose)
        setenv("LINUXIO_VERBOSE", "1", 1);

      // Redirect bridge stdout/stderr to journald (or file fallback)
      uid_t owner_uid = pw->pw_uid; // keep logs under the user's /run/linuxio/<uid> even in privileged mode
      if (redirect_bridge_output(owner_uid, linuxio_gid, sess_id) != 0)
      {
        int devnull = open("/dev/null", O_WRONLY | O_CLOEXEC);
        if (devnull >= 0)
        {
          dup2(devnull, STDOUT_FILENO);
          dup2(devnull, STDERR_FILENO);
          close(devnull);
        }
      }

      // Debug: pre-exec credential trace (you can remove later)
      {
        uid_t r, e, s;
        gid_t gr, ge, gs;
        getresuid(&r, &e, &s);
        getresgid(&gr, &ge, &gs);
        journal_infof("pre-exec bridge: want_priv=%d ruid=%d euid=%d suid=%d rgid=%d egid=%d sgid=%d path=%s",
                      want_privileged, (int)r, (int)e, (int)s, (int)gr, (int)ge, (int)gs, bridge_path);
      }

      // Build argv: linuxio-bridge --env <env> [--verbose]
      char *argv_child[6];
      int ai = 0;
      argv_child[ai++] = (char *)bridge_path;
      argv_child[ai++] = "--env";
      argv_child[ai++] = (char *)envmode;
      if (verbose)
        argv_child[ai++] = "--verbose";
      argv_child[ai++] = NULL;

      // In bridge child, after dropping privileges but before exec:
      struct rlimit rl;

      // CPU time limit (configurable via env)
      int cpu_limit = env_get_int("LINUXIO_RLIMIT_CPU", 7200, 60, 86400); // 2hr default
      rl.rlim_cur = rl.rlim_max = cpu_limit;
      setrlimit(RLIMIT_CPU, &rl);

      // Process count limit
      int nproc_limit = env_get_int("LINUXIO_RLIMIT_NPROC", 512, 10, 2048);
      rl.rlim_cur = rl.rlim_max = nproc_limit;
      setrlimit(RLIMIT_NPROC, &rl);

      // File size limit
      rl.rlim_cur = rl.rlim_max = 1UL * 1024 * 1024 * 1024; // 1GB
      setrlimit(RLIMIT_FSIZE, &rl);

      // Address space (virtual memory)
      rl.rlim_cur = rl.rlim_max = 16UL * 1024 * 1024 * 1024; // 16GB
      setrlimit(RLIMIT_AS, &rl);

      // Disable core dumps
      rl.rlim_cur = rl.rlim_max = 0;
      setrlimit(RLIMIT_CORE, &rl);

      exec_bridge_fd_checked(bridge_path, argv_child);
      perror("exec linuxio-bridge");
      _exit(127);
    }

    // Nanny waits bridge, then closes PAM and exits with same status
    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR)
    {
    }
    (void)pam_close_session(pamh, 0);
    (void)pam_setcred(pamh, PAM_DELETE_CRED);
    (void)pam_end(pamh, 0);
    _exit(WIFEXITED(status)     ? WEXITSTATUS(status)
          : WIFSIGNALED(status) ? 128 + WTERMSIG(status)
                                : 1);
  }

  // Original parent: report success immediately & exit
  (void)write_all(STDOUT_FILENO, "OK\n", 3);
  (void)close(STDIN_FILENO);
  (void)close(STDOUT_FILENO);
  (void)close(STDERR_FILENO);
  free(user);
  _exit(0);
}

// -------- write_all ----------
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
      return -1; // real error
    }
    p += (size_t)n;
    len -= (size_t)n;
  }
  return 0;
}