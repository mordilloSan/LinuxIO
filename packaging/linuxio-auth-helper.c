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

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
extern char **environ;

// ---- forward decls ----
static int write_all(int fd, const void *buf, size_t len);
static int env_get_int(const char *name, int defval, int minv, int maxv);

// Max lengths
#define MAX_USERNAME_LEN 256
#define MAX_PATH_LEN 4096
#define MAX_ENV_VALUE_LEN 8192

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

// -------- minimal logging (errors only to journal/syslog) --------
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

// -------- JSON escaping (FIX #1) --------
static int json_escape_string(char *dst, size_t dstsz, const char *src)
{
  if (!dst || dstsz == 0)
    return -1;
  if (!src)
  {
    dst[0] = '\0';
    return 0;
  }

  size_t j = 0;
  for (size_t i = 0; src[i] && j + 6 < dstsz; i++)
  {
    unsigned char c = (unsigned char)src[i];
    switch (c)
    {
    case '"':
      dst[j++] = '\\';
      dst[j++] = '"';
      break;
    case '\\':
      dst[j++] = '\\';
      dst[j++] = '\\';
      break;
    case '\b':
      dst[j++] = '\\';
      dst[j++] = 'b';
      break;
    case '\f':
      dst[j++] = '\\';
      dst[j++] = 'f';
      break;
    case '\n':
      dst[j++] = '\\';
      dst[j++] = 'n';
      break;
    case '\r':
      dst[j++] = '\\';
      dst[j++] = 'r';
      break;
    case '\t':
      dst[j++] = '\\';
      dst[j++] = 't';
      break;
    default:
      if (c < 0x20)
      {
        int n = safe_snprintf(dst + j, dstsz - j, "\\u%04x", c);
        if (n > 0)
          j += n;
      }
      else
      {
        dst[j++] = c;
      }
    }
  }
  dst[j] = '\0';
  return (int)j;
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
    if (msg[i]->msg_style == PAM_PROMPT_ECHO_OFF && password)
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
  if (setuid(0) == 0)
  {
    log_stderrf("SECURITY: privilege drop failed");
    _exit(127);
  }
}

// -------- read line from stdin - (with optional timeout)
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

// Validate env var length and dup it
static char *safe_getenv_strdup(const char *name, size_t max_len)
{
  const char *val = getenv(name);
  if (!val || !*val)
    return NULL;

  size_t len = strnlen(val, max_len + 1);
  if (len > max_len)
  {
    journal_errorf("env var %s too long", name);
    return NULL;
  }

  return strdup(val);
}

// Ensure /run/linuxio/<uid> exists and perms sane (FIX #4 - proper cleanup)
static int ensure_runtime_dirs(const struct passwd *pw, gid_t *out_linuxio_gid)
{
  if (!pw)
  {
    journal_errorf("runtime: no passwd");
    return -1;
  }
  const char *base = "/run/linuxio";
  mode_t old_umask = umask(0);
  int run_fd = -1, base_fd = -1, user_fd = -1;
  int ret = -1;

  gid_t linuxio_gid = 0;
  struct group *gr = getgrnam("linuxio");
  if (gr)
    linuxio_gid = gr->gr_gid;
  if (out_linuxio_gid)
    *out_linuxio_gid = linuxio_gid;

  run_fd = open("/run", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
  if (run_fd < 0)
  {
    journal_errorf("runtime: open /run failed: %m");
    goto cleanup;
  }

  if (mkdirat(run_fd, "linuxio", 02771) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir %s failed: %m", base);
    goto cleanup;
  }

  base_fd = openat(run_fd, "linuxio", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (base_fd < 0)
  {
    journal_errorf("runtime: open %s failed: %m", base);
    goto cleanup;
  }

  struct stat st;
  if (fstat(base_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: stat %s failed", base);
    goto cleanup;
  }
  if (st.st_uid != 0 || (st.st_mode & S_IWOTH))
  {
    journal_errorf("runtime: %s unsafe perms", base);
    goto cleanup;
  }
  /* base directory ownership */
  if (fchown(base_fd, 0, linuxio_gid) != 0)
  {
    journal_errorf("runtime: fchown(base_fd, 0, %u) failed: %m", (unsigned)linuxio_gid);
    goto cleanup;
  }
  if (fchmod(base_fd, 02771) != 0)
  {
    journal_errorf("runtime: fchmod(base_fd, 02771) failed: %m");
    goto cleanup;
  }

  char uid_str[32];
  safe_snprintf(uid_str, sizeof(uid_str), "%u", (unsigned)pw->pw_uid);
  if (mkdirat(base_fd, uid_str, 02770) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir %s/%s failed: %m", base, uid_str);
    goto cleanup;
  }

  user_fd = openat(base_fd, uid_str, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (user_fd < 0)
  {
    journal_errorf("runtime: open %s/%s failed: %m", base, uid_str);
    goto cleanup;
  }

  if (fstat(user_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: userdir stat failed");
    goto cleanup;
  }
  /* user directory ownership */
  if (fchown(user_fd, pw->pw_uid, linuxio_gid) != 0)
  {
    journal_errorf("runtime: fchown(user_fd, %u, %u) failed: %m",
                   (unsigned)pw->pw_uid, (unsigned)linuxio_gid);
    goto cleanup;
  }
  if (fchmod(user_fd, 02770) != 0)
  {
    journal_errorf("runtime: fchmod(user_fd, 02770) failed: %m");
    goto cleanup;
  }

  ret = 0;

cleanup:
  if (user_fd >= 0)
    close(user_fd);
  if (base_fd >= 0)
    close(base_fd);
  if (run_fd >= 0)
    close(run_fd);
  umask(old_umask);
  return ret;
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

// Prefer execveat(); fall back to execv() on EACCES/EPERM/ENOSYS for compatibility.
static int exec_bridge_via_fd(int fd, const char *bridge_path, const char *const argv[])
{
#if defined(SYS_execveat)
  if (syscall(SYS_execveat, fd, "", ARGV_UNCONST(argv), environ, AT_EMPTY_PATH) == -1)
  {
    int saved = errno;
    if (saved == EACCES || saved == EPERM || saved == ENOSYS)
    {
      execv(bridge_path, ARGV_UNCONST(argv));
      return -1; // if execv also fails
    }
    errno = saved;
    return -1;
  }
#else
  execv(bridge_path, ARGV_UNCONST(argv));
  return -1;
#endif
  return -1;
}

// Redirect bridge stdout/stderr (best effort)
static int redirect_bridge_output(uid_t owner_uid, gid_t linuxio_gid, const char *sess_id)
{
#ifdef HAVE_SD_JOURNAL
  int jfd = sd_journal_stream_fd("linuxio-bridge", LOG_INFO, 1);
  if (jfd >= 0)
  {
    if (dup2(jfd, STDOUT_FILENO) < 0)
    {
      close(jfd);
      return -1;
    }
    if (dup2(jfd, STDERR_FILENO) < 0)
    {
      close(jfd);
      return -1;
    }
    close(jfd);
    return 0;
  }
#endif
  (void)sess_id;
  (void)owner_uid;
  (void)linuxio_gid;
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

// Lock password memory
static char *get_password_locked(int *locked_out)
{
  char *password = NULL;
  *locked_out = 0;

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

  if (password)
  {
    // Lock memory to prevent swapping
    if (mlock(password, strlen(password)) != 0)
    {
      // Not fatal, but log it
      journal_errorf("mlock password failed: %m");
    }
    else
    {
      *locked_out = 1;
    }
  }

  return password;
}

static int user_has_sudo(const struct passwd *pw, const char *password, int *out_nopasswd)
{
  if (out_nopasswd)
    *out_nopasswd = 0;
  int to_nopw = env_get_int("LINUXIO_SUDO_TIMEOUT_NOPASSWD", 3, 1, 30);
  int to_pw = env_get_int("LINUXIO_SUDO_TIMEOUT_PASSWORD", 4, 1, 30);

  const char *argv_nopw[] = {"/usr/bin/sudo", "-n", "-v", NULL};
  int rc = run_cmd_as_user_with_input(pw, argv_nopw, NULL, to_nopw);
  if (rc == 0)
  {
    if (out_nopasswd)
      *out_nopasswd = 1;
    const char *argv_k[] = {"/usr/bin/sudo", "-k", NULL};
    (void)run_cmd_as_user_with_input(pw, argv_k, NULL, 2);
    return 1;
  }

  if (password && *password)
  {
    const char *argv_pw[] = {"/usr/bin/sudo", "-S", "-p", "", "-v", NULL};
    char buf[1024];
    (void)safe_snprintf(buf, sizeof(buf), "%s\n", password);
    rc = run_cmd_as_user_with_input(pw, argv_pw, buf, to_pw);
    secure_bzero(buf, sizeof(buf));
    if (rc == 0)
    {
      const char *argv_k[] = {"/usr/bin/sudo", "-k", NULL};
      (void)run_cmd_as_user_with_input(pw, argv_k, NULL, 2);
      return 1;
    }
  }

  return 0;
}

// FIX #7: Resource limits moved to function (called in bridge child)
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

// FIX #3: Improved socket path validation
static int valid_socket_path_for_uid(const char *p, uid_t uid)
{
  if (!p || !*p)
    return 0;
  if (p[0] != '/')
    return 0;

  size_t L = strlen(p);
  if (L < 6 || L >= PATH_MAX)
    return 0;

  // Check for dangerous patterns
  if (strstr(p, "/../"))
    return 0;
  if (strstr(p, "/./"))
    return 0;
  if (strstr(p, "//"))
    return 0;
  if (strstr(p, "/.."))
    return 0; // catches trailing ..
  if (p[L - 1] == '/')
    return 0;

  // Must end in .sock
  if (strcmp(p + L - 5, ".sock") != 0)
    return 0;

  // Must start with correct prefix
  char prefix[64];
  safe_snprintf(prefix, sizeof(prefix), "/run/linuxio/%u/", (unsigned)uid);
  size_t prelen = strlen(prefix);
  if (strncmp(p, prefix, prelen) != 0)
    return 0;

  // Validate all components after prefix don't contain dots
  const char *rest = p + prelen;
  for (const char *c = rest; *c; c++)
  {
    if (*c == '.' && (c == rest || *(c - 1) == '/'))
      return 0; // . at start or after /
  }

  return 1;
}

// -------- main ----------
int main(void)
{
  if (geteuid() != 0)
  {
    log_stderrf("must be setuid root");
    return 126;
  }
  (void)prctl(PR_SET_DUMPABLE, 0);

  char *user = safe_getenv_strdup("LINUXIO_SESSION_USER", MAX_USERNAME_LEN);
  if (!user)
  {
    log_stderrf("missing or invalid LINUXIO_SESSION_USER");
    return 2;
  }

  // FIX #6: Get password with memory locking
  int pw_locked = 0;
  char *password = get_password_locked(&pw_locked);
  if (!password || !*password)
  {
    log_stderrf("missing password");
    if (password)
    {
      if (pw_locked)
        munlock(password, strlen(password));
      secure_bzero(password, strlen(password));
      free(password);
    }
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
    if (pw_locked)
      munlock(password, strlen(password));
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
    if (pw_locked)
      munlock(password, strlen(password));
    secure_bzero(password, strlen(password));
    free(password);
    free(user);
    return 1;
  }

  // NOTE: Don't open session yet - will do in nanny child (FIX #2)

  struct passwd *pw = getpwnam(user);
  if (!pw)
  {
    perror("getpwnam");
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (pw_locked)
      munlock(password, strlen(password));
    secure_bzero(password, strlen(password));
    free(password);
    free(user);
    return 5;
  }

  gid_t linuxio_gid = 0;
  if (ensure_runtime_dirs(pw, &linuxio_gid) != 0)
  {
    log_stderrf("prepare runtime dir failed");
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (pw_locked)
      munlock(password, strlen(password));
    secure_bzero(password, strlen(password));
    free(password);
    free(user);
    return 5;
  }

  int want_privileged = 0, nopasswd = 0;
  want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  // Read env inputs (bounded)
  char *envmode_in = safe_getenv_strdup("LINUXIO_ENV", 128);
  char *bridge_in = safe_getenv_strdup("LINUXIO_BRIDGE_BIN", MAX_PATH_LEN);
  char *sess_id = safe_getenv_strdup("LINUXIO_SESSION_ID", 256);
  char *sess_user = safe_getenv_strdup("LINUXIO_SESSION_USER", MAX_USERNAME_LEN);
  char *sess_secret = safe_getenv_strdup("LINUXIO_BRIDGE_SECRET", MAX_ENV_VALUE_LEN);
  char *server_base = safe_getenv_strdup("LINUXIO_SERVER_BASE_URL", MAX_ENV_VALUE_LEN);
  char *server_cert = safe_getenv_strdup("LINUXIO_SERVER_CERT", MAX_ENV_VALUE_LEN);
  char *verbose_in = safe_getenv_strdup("LINUXIO_VERBOSE", 16);
  char *socket_path_env = safe_getenv_strdup("LINUXIO_SOCKET_PATH", MAX_PATH_LEN);

  const char *envmode = (envmode_in && *envmode_in) ? envmode_in : "production";
  const char *bridge_path = (bridge_in && *bridge_in) ? bridge_in : "/usr/local/bin/linuxio-bridge";
  if (bridge_path[0] != '/')
  {
    log_stderrf("bridge path must be absolute");
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (pw_locked)
      munlock(password, strlen(password));
    secure_bzero(password, strlen(password));
    free(password);
    free(user);
    free(envmode_in);
    free(bridge_in);
    free(sess_id);
    free(sess_user);
    free(sess_secret);
    free(server_base);
    free(server_cert);
    free(verbose_in);
    free(socket_path_env);
    return 5;
  }

  // Wipe password after sudo probe
  if (pw_locked)
    munlock(password, strlen(password));
  secure_bzero(password, strlen(password));
  free(password);
  password = NULL;

  int verbose = 0;
  if (verbose_in && *verbose_in)
  {
    if (!strcasecmp(verbose_in, "1") || !strcasecmp(verbose_in, "true") ||
        !strcasecmp(verbose_in, "yes") || !strcasecmp(verbose_in, "on"))
      verbose = 1;
  }

  // Bridge validation
  int bridge_fd = -1;
  if (!want_privileged)
  {
    if (open_and_validate_bridge(bridge_path, pw->pw_uid, &bridge_fd) != 0)
    {
      if (open_and_validate_bridge(bridge_path, 0, &bridge_fd) != 0)
      {
        log_stderrf("bridge validation failed");
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
        free(socket_path_env);
        return 5;
      }
    }
  }
  else
  {
    if (open_and_validate_bridge(bridge_path, 0, &bridge_fd) != 0)
    {
      log_stderrf("bridge validation failed");
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
      free(socket_path_env);
      return 5;
    }
  }

  // Tell parent our mode
  const char *mode = want_privileged ? "MODE=privileged\n" : "MODE=unprivileged\n";
  (void)write_all(STDOUT_FILENO, mode, strlen(mode));

  int boot_pipe[2];
  if (pipe(boot_pipe) != 0)
  {
    perror("pipe");
    close(bridge_fd);
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
    free(socket_path_env);
    return 5;
  }

  pid_t nanny = fork();
  if (nanny < 0)
  {
    perror("fork nanny");
    close(bridge_fd);
    close(boot_pipe[0]);
    close(boot_pipe[1]);
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
    free(socket_path_env);
    return 5;
  }

  if (nanny == 0)
  {
    // FIX #2: Open PAM session in nanny child
    rc = pam_open_session(pamh, 0);
    if (rc != PAM_SUCCESS)
    {
      log_stderrf("open_session: %s", pam_strerror(pamh, rc));
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      _exit(5);
    }

    (void)prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0);

    pid_t child = fork();
    if (child < 0)
    {
      perror("fork bridge");
      close(bridge_fd);
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      _exit(5);
    }

    if (child == 0)
    {
      // Bridge child
      close(boot_pipe[1]);
      if (dup2(boot_pipe[0], 3) < 0)
        _exit(127);
      if (fcntl(3, F_SETFD, 0) < 0)
        _exit(127);
      close(boot_pipe[0]);

      umask(077);

      // FIX #7: Apply resource limits here in bridge child
      set_resource_limits();

      if (want_privileged)
      {
        clearenv();
        setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
        setenv("LANG", "C", 1);
        setenv("LC_ALL", "C", 1);
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
        clearenv();
        setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
        setenv("LANG", "C", 1);
        setenv("LC_ALL", "C", 1);
        if (pw)
        {
          setenv("HOME", pw->pw_dir, 1);
          setenv("USER", pw->pw_name, 1);
          setenv("LOGNAME", pw->pw_name, 1);
          char xdg[64];
          safe_snprintf(xdg, sizeof(xdg), "/run/user/%u", (unsigned)pw->pw_uid);
          setenv("XDG_RUNTIME_DIR", xdg, 1);
        }
        if (chdir(pw->pw_dir) != 0)
        {
          log_stderrf("chdir(%s) failed: %m", pw->pw_dir);
          _exit(127);
        }
      }

      // Re-enable core dumps for the bridge process now that we're running
      // under the target user. The helper disables dumpability early on to
      // avoid leaking credentials, so we have to opt-in again here.
      (void)prctl(PR_SET_DUMPABLE, 1);

      if (verbose)
        setenv("LINUXIO_VERBOSE", "1", 1);

      (void)redirect_bridge_output(pw->pw_uid, linuxio_gid, sess_id);

      const char *argv_child[5];
      int ai = 0;
      argv_child[ai++] = bridge_path;
      argv_child[ai++] = "--env";
      argv_child[ai++] = envmode;
      if (verbose)
        argv_child[ai++] = "--verbose";
      argv_child[ai++] = NULL;

      if (exec_bridge_via_fd(bridge_fd, bridge_path, argv_child) != 0)
      {
        perror("exec linuxio-bridge");
      }
      _exit(127);
    }

    // Nanny: build socket path
    const char *sock = NULL;
    char sockbuf[PATH_MAX];
    if (valid_socket_path_for_uid(socket_path_env, pw->pw_uid))
    {
      sock = socket_path_env;
    }
    else
    {
      safe_snprintf(sockbuf, sizeof(sockbuf), "/run/linuxio/%u/linuxio-bridge-%s.sock",
                    (unsigned)pw->pw_uid, (sess_id && *sess_id) ? sess_id : "nosessid");
      sock = sockbuf;
    }

    // FIX #1: JSON with proper escaping for all fields
    char json[16384]; // Increased size for escaped content
    char sess_id_esc[1024], sess_user_esc[1024], secret_esc[16384];
    char server_base_esc[16384], sock_esc[8192];

    json_escape_string(sess_id_esc, sizeof(sess_id_esc), sess_id ? sess_id : "");
    json_escape_string(sess_user_esc, sizeof(sess_user_esc), sess_user ? sess_user : "");
    json_escape_string(secret_esc, sizeof(secret_esc), sess_secret ? sess_secret : "");
    json_escape_string(server_base_esc, sizeof(server_base_esc), server_base ? server_base : "");
    json_escape_string(sock_esc, sizeof(sock_esc), sock);

    if (server_cert && *server_cert)
    {
      char cert_esc[16384];
      json_escape_string(cert_esc, sizeof(cert_esc), server_cert);

      safe_snprintf(json, sizeof(json),
                    "{"
                    "\"session_id\":\"%s\","
                    "\"username\":\"%s\","
                    "\"uid\":\"%u\","
                    "\"gid\":\"%u\","
                    "\"secret\":\"%s\","
                    "\"server_base_url\":\"%s\","
                    "\"server_cert\":\"%s\","
                    "\"socket_path\":\"%s\","
                    "\"verbose\":\"%s\""
                    "}",
                    sess_id_esc, sess_user_esc,
                    (unsigned)pw->pw_uid, (unsigned)pw->pw_gid,
                    secret_esc, server_base_esc, cert_esc,
                    sock_esc, verbose ? "1" : "0");
    }
    else
    {
      safe_snprintf(json, sizeof(json),
                    "{"
                    "\"session_id\":\"%s\","
                    "\"username\":\"%s\","
                    "\"uid\":\"%u\","
                    "\"gid\":\"%u\","
                    "\"secret\":\"%s\","
                    "\"server_base_url\":\"%s\","
                    "\"server_cert\":null,"
                    "\"socket_path\":\"%s\","
                    "\"verbose\":\"%s\""
                    "}",
                    sess_id_esc, sess_user_esc,
                    (unsigned)pw->pw_uid, (unsigned)pw->pw_gid,
                    secret_esc, server_base_esc,
                    sock_esc, verbose ? "1" : "0");
    }

    (void)write_all(boot_pipe[1], json, strlen(json));
    close(boot_pipe[1]);
    close(bridge_fd);

    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR)
    {
    }
    int exitcode = WIFEXITED(status) ? WEXITSTATUS(status) : (WIFSIGNALED(status) ? 128 + WTERMSIG(status) : 1);

    // FIX #2: Clean PAM session properly in nanny
    (void)pam_close_session(pamh, 0);
    (void)pam_setcred(pamh, PAM_DELETE_CRED);
    (void)pam_end(pamh, 0);
    _exit(exitcode);
  }

  // Original parent - close everything and exit
  close(boot_pipe[0]);
  close(boot_pipe[1]);
  close(bridge_fd);
  (void)write_all(STDOUT_FILENO, "OK\n", 3);
  (void)close(STDIN_FILENO);
  (void)close(STDOUT_FILENO);
  (void)close(STDERR_FILENO);

  // Cleanup
  free(user);
  free(envmode_in);
  free(bridge_in);
  free(sess_id);
  free(sess_user);
  free(sess_secret);
  free(server_base);
  free(server_cert);
  free(verbose_in);
  free(socket_path_env);

  // Parent doesn't manage PAM anymore
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
