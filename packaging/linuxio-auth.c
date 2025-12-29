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

// jsmn: minimal JSON parser
#define JSMN_STATIC
#define JSMN_PARENT_LINKS
#include "jsmn.h"

// Protocol constants (field names, limits, env vars)
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

// -------- JSON escaping --------
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

// Ensure /run/linuxio/<uid> exists and perms sane for bridge sockets
// Returns the validated user_fd for openat() operations, or -1 on error
// Caller must close the returned fd
static int ensure_runtime_dirs(const struct passwd *pw)
{
  if (!pw)
  {
    journal_errorf("runtime: no passwd");
    return -1;
  }

  struct group *gr = getgrnam("linuxio-bridge-socket");
  if (!gr)
  {
    journal_errorf("runtime: group linuxio-bridge-socket not found");
    return -1;
  }
  gid_t linuxio_gid = gr->gr_gid;

  mode_t old_umask = umask(0);
  int run_fd = -1, base_fd = -1, user_fd = -1;

  run_fd = open("/run", O_RDONLY | O_DIRECTORY | O_CLOEXEC);
  if (run_fd < 0)
  {
    journal_errorf("runtime: open /run failed: %m");
    goto cleanup;
  }

  if (mkdirat(run_fd, "linuxio", 0755) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir /run/linuxio failed: %m");
    goto cleanup;
  }

  base_fd = openat(run_fd, "linuxio", O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (base_fd < 0)
  {
    journal_errorf("runtime: open /run/linuxio failed: %m");
    goto cleanup;
  }

  struct stat st;
  if (fstat(base_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: stat /run/linuxio failed");
    goto cleanup;
  }
  // Base directory must not be group or world writable (only root writes here)
  if ((st.st_mode & S_IWGRP) || (st.st_mode & S_IWOTH))
  {
    journal_errorf("runtime: /run/linuxio is group/world-writable (unsafe)");
    goto cleanup;
  }

  if ((st.st_mode & 0777) != 0755)
  {
    if (fchmod(base_fd, 0755) != 0)
    {
      journal_errorf("runtime: fchmod(/run/linuxio, 0755) failed: %m");
      goto cleanup;
    }
  }
  if (fchown(base_fd, 0, linuxio_gid) != 0)
  {
    journal_errorf("runtime: fchown(/run/linuxio, 0, %u) failed: %m", (unsigned)linuxio_gid);
    goto cleanup;
  }

  char uid_str[32];
  safe_snprintf(uid_str, sizeof(uid_str), "%u", (unsigned)pw->pw_uid);
  if (mkdirat(base_fd, uid_str, 02710) != 0 && errno != EEXIST)
  {
    journal_errorf("runtime: mkdir /run/linuxio/%s failed: %m", uid_str);
    goto cleanup;
  }

  user_fd = openat(base_fd, uid_str, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (user_fd < 0)
  {
    journal_errorf("runtime: open /run/linuxio/%s failed: %m", uid_str);
    goto cleanup;
  }

  if (fstat(user_fd, &st) != 0 || !S_ISDIR(st.st_mode))
  {
    journal_errorf("runtime: stat /run/linuxio/%s failed", uid_str);
    goto cleanup;
  }

  if ((st.st_mode & 07777) != 02710)
  {
    if (fchmod(user_fd, 02710) != 0)
    {
      journal_errorf("runtime: fchmod(/run/linuxio/%s, 02710) failed: %m", uid_str);
      goto cleanup;
    }
  }
  if (fchown(user_fd, pw->pw_uid, linuxio_gid) != 0)
  {
    journal_errorf("runtime: fchown(/run/linuxio/%s, %u, %u) failed: %m",
                   uid_str, (unsigned)pw->pw_uid, (unsigned)linuxio_gid);
    goto cleanup;
  }

  // Success - close temporary fds but return user_fd to caller
  if (base_fd >= 0)
    close(base_fd);
  if (run_fd >= 0)
    close(run_fd);
  umask(old_umask);
  return user_fd;  // Caller must close this

cleanup:
  if (user_fd >= 0)
    close(user_fd);
  if (base_fd >= 0)
    close(base_fd);
  if (run_fd >= 0)
    close(run_fd);
  umask(old_umask);
  return -1;
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
  rl.rlim_cur = rl.rlim_max = nproc_limit;
  (void)setrlimit(RLIMIT_NPROC, &rl);

  rl.rlim_cur = rl.rlim_max = 16UL * 1024 * 1024 * 1024;
  (void)setrlimit(RLIMIT_AS, &rl);
}

static int wait_for_bridge_socket(const char *socket_path, int timeout_ms)
{
  if (!socket_path || !*socket_path)
    return -1;
  if (strlen(socket_path) >= sizeof(((struct sockaddr_un *)0)->sun_path))
    return -1;

  const int step_ms = 50;
  int elapsed_ms = 0;
  while (elapsed_ms < timeout_ms)
  {
    int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd >= 0)
    {
      struct sockaddr_un addr;
      memset(&addr, 0, sizeof(addr));
      addr.sun_family = AF_UNIX;
      memcpy(addr.sun_path, socket_path, strlen(socket_path) + 1);
      if (connect(fd, (struct sockaddr *)&addr, sizeof(addr)) == 0)
      {
        close(fd);
        return 0;
      }
      close(fd);
    }
    usleep(step_ms * 1000);
    elapsed_ms += step_ms;
  }
  return -1;
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

// Write bootstrap JSON to a file descriptor
// Returns number of bytes written, or -1 on error
static int write_bootstrap_json(
    int fd,
    const char *session_id,
    const char *username,
    uid_t uid,
    gid_t gid,
    const char *secret,
    const char *socket_path,
    const char *server_base_url,
    const char *server_cert,
    int verbose,
    int log_fd)
{
  char json[16384];
  char sess_id_esc[1024], username_esc[1024], secret_esc[16384];
  char server_base_esc[16384], socket_esc[8192];

  json_escape_string(sess_id_esc, sizeof(sess_id_esc), session_id ? session_id : "");
  json_escape_string(username_esc, sizeof(username_esc), username ? username : "");
  json_escape_string(secret_esc, sizeof(secret_esc), secret ? secret : "");
  json_escape_string(server_base_esc, sizeof(server_base_esc), server_base_url ? server_base_url : "");
  json_escape_string(socket_esc, sizeof(socket_esc), socket_path ? socket_path : "");

  if (server_cert && *server_cert)
  {
    char cert_esc[PROTO_MAX_SERVER_CERT];
    json_escape_string(cert_esc, sizeof(cert_esc), server_cert);

    safe_snprintf(json, sizeof(json),
                  "{"
                  "\"" FIELD_SESSION_ID "\":\"%s\","
                  "\"" FIELD_USERNAME "\":\"%s\","
                  "\"" FIELD_UID "\":%u,"
                  "\"" FIELD_GID "\":%u,"
                  "\"" FIELD_SECRET "\":\"%s\","
                  "\"" FIELD_SERVER_BASE_URL "\":\"%s\","
                  "\"" FIELD_SERVER_CERT "\":\"%s\","
                  "\"" FIELD_SOCKET_PATH "\":\"%s\","
                  "\"" FIELD_VERBOSE "\":%s,"
                  "\"" FIELD_LOG_FD "\":%d"
                  "}",
                  sess_id_esc, username_esc,
                  (unsigned)uid, (unsigned)gid,
                  secret_esc, server_base_esc, cert_esc,
                  socket_esc, verbose ? "true" : "false", log_fd);
  }
  else
  {
    safe_snprintf(json, sizeof(json),
                  "{"
                  "\"" FIELD_SESSION_ID "\":\"%s\","
                  "\"" FIELD_USERNAME "\":\"%s\","
                  "\"" FIELD_UID "\":%u,"
                  "\"" FIELD_GID "\":%u,"
                  "\"" FIELD_SECRET "\":\"%s\","
                  "\"" FIELD_SERVER_BASE_URL "\":\"%s\","
                  "\"" FIELD_SERVER_CERT "\":null,"
                  "\"" FIELD_SOCKET_PATH "\":\"%s\","
                  "\"" FIELD_VERBOSE "\":%s,"
                  "\"" FIELD_LOG_FD "\":%d"
                  "}",
                  sess_id_esc, username_esc,
                  (unsigned)uid, (unsigned)gid,
                  secret_esc, server_base_esc,
                  socket_esc, verbose ? "true" : "false", log_fd);
  }

  size_t len = strlen(json);
  if (write_all(fd, json, len) != 0)
    return -1;
  return (int)len;
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

  char buf[1024];
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

// Environment mode validation - whitelist valid modes
static int valid_env_mode(const char *s)
{
  if (!s || !*s)
    return 1;  // Empty is ok, defaults to "production"

  // Whitelist allowed environment modes
  if (strcmp(s, "production") == 0)
    return 1;
  if (strcmp(s, "development") == 0)
    return 1;
  return 0;  // Reject anything else
}

// Socket path validation
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

// ============================================================================
// Single-shot mode - socket-activated worker
// ============================================================================

// Simple JSON field extractor (finds "key":"value" pattern)
// Helper: compare JSON token string with key
static int jsoneq(const char *json, jsmntok_t *tok, const char *s)
{
  if (tok->type == JSMN_STRING && (int)strlen(s) == tok->end - tok->start &&
      strncmp(json + tok->start, s, tok->end - tok->start) == 0)
  {
    return 0;
  }
  return -1;
}

// Get string value for a key from parsed JSON
static char *json_get_string(const char *json, const char *key, char *buf, size_t bufsz)
{
  if (!json || !key || !buf || bufsz == 0)
    return NULL;
  buf[0] = '\0';

  jsmn_parser parser;
  jsmntok_t tokens[128];

  jsmn_init(&parser);
  int r = jsmn_parse(&parser, json, strlen(json), tokens, sizeof(tokens) / sizeof(tokens[0]));

  // Check for parse errors (JSMN_ERROR_NOMEM=-1, JSMN_ERROR_INVAL=-2, JSMN_ERROR_PART=-3)
  if (r < 0)
    return NULL;
  if (r < 1 || tokens[0].type != JSMN_OBJECT)
    return NULL;

  // Iterate over object keys
  for (int i = 1; i < r; i++)
  {
    if (jsoneq(json, &tokens[i], key) == 0)
    {
      // Found key, next token is the value
      i++;
      if (i >= r)
        return NULL;

      // Handle JSON null - treat as unset/missing
      if (tokens[i].type == JSMN_PRIMITIVE)
      {
        int token_len = tokens[i].end - tokens[i].start;
        if (token_len == 4 && strncmp(json + tokens[i].start, "null", 4) == 0)
        {
          buf[0] = '\0';
          return NULL;
        }
      }

      int len = tokens[i].end - tokens[i].start;
      if (len >= (int)bufsz)
        len = bufsz - 1;

      memcpy(buf, json + tokens[i].start, len);
      buf[len] = '\0';
      return buf;
    }
  }
  return NULL;
}

// Send JSON response to client
static void send_response(int fd, const char *status, const char *error, const char *mode, const char *socket_path, const char *motd)
{
  char buf[8192];
  char err_escaped[PROTO_MAX_ERROR] = "";
  char sock_escaped[PROTO_MAX_SOCKET_PATH] = "";
  char motd_escaped[PROTO_MAX_MOTD] = "";

  if (error && *error)
    json_escape_string(err_escaped, sizeof(err_escaped), error);
  if (socket_path && *socket_path)
    json_escape_string(sock_escaped, sizeof(sock_escaped), socket_path);
  if (motd && *motd)
    json_escape_string(motd_escaped, sizeof(motd_escaped), motd);

  int len;
  if (error && *error)
  {
    len = safe_snprintf(buf, sizeof(buf),
                        "{\"" FIELD_STATUS "\":\"%s\",\"" FIELD_ERROR "\":\"%s\"}\n",
                        status, err_escaped);
  }
  else if (mode && socket_path)
  {
    if (motd && *motd)
    {
      len = safe_snprintf(buf, sizeof(buf),
                          "{\"" FIELD_STATUS "\":\"%s\",\"" FIELD_MODE "\":\"%s\",\"" FIELD_SOCKET_PATH "\":\"%s\",\"" FIELD_MOTD "\":\"%s\"}\n",
                          status, mode, sock_escaped, motd_escaped);
    }
    else
    {
      len = safe_snprintf(buf, sizeof(buf),
                          "{\"" FIELD_STATUS "\":\"%s\",\"" FIELD_MODE "\":\"%s\",\"" FIELD_SOCKET_PATH "\":\"%s\"}\n",
                          status, mode, sock_escaped);
    }
  }
  else
  {
    len = safe_snprintf(buf, sizeof(buf), "{\"" FIELD_STATUS "\":\"%s\"}\n", status);
  }

  if (len > 0)
    (void)write_all(fd, buf, (size_t)len);
}

static pid_t spawn_bridge_process(
    const struct passwd *pw,
    int want_privileged,
    int bridge_fd,
    const char *env_mode,
    int verbose,
    int bootstrap_pipe_read,  // Pipe read end for bootstrap JSON (will be stdin)
    const char *session_id,
    const char *socket_path)
{
  pid_t pid = fork();
  if (pid < 0)
    return -1;
  if (pid > 0)
    return pid;

  // Child: set up stdin from bootstrap pipe
  if (bootstrap_pipe_read >= 0)
  {
    (void)dup2(bootstrap_pipe_read, STDIN_FILENO);
    close(bootstrap_pipe_read);
  }

  (void)dup2(STDERR_FILENO, STDOUT_FILENO);

  umask(077);
  set_resource_limits();

  // Preserve and validate environment variables before clearenv()
  const char *preserve_lang = getenv("LANG");
  const char *preserve_term = getenv("TERM");

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
    setenv(ENV_PRIVILEGED, "1", 1);
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

  if (env_mode && *env_mode)
    setenv(ENV_ENV, env_mode, 1);
  // Bootstrap is now passed via stdin pipe, no file path needed
  if (session_id && *session_id)
    setenv(ENV_SESSION_ID, session_id, 1);
  if (socket_path && *socket_path)
    setenv(ENV_SOCKET_PATH, socket_path, 1);
  setenv(ENV_BRIDGE, "1", 1);
  if (verbose)
    setenv(ENV_VERBOSE, "1", 1);

  // Only enable core dumps in development mode
  // In production, keep dumpable off to prevent leaking secrets
  if (env_mode && strcmp(env_mode, ENV_MODE_DEVELOPMENT) == 0)
    (void)prctl(PR_SET_DUMPABLE, 1);

  // Close all file descriptors except stdin(0), stdout(1), stderr(2), and bridge_fd
  // Uses close_range() syscall (Linux 5.9+) with fallback for older kernels
#ifndef __NR_close_range
  #define __NR_close_range 436
#endif
  // Try close_range first; fallback to manual loop on ENOSYS (kernel < 5.9)
  int close_range_failed = 0;
  if (bridge_fd > 2)
  {
    // Close FDs 3 to bridge_fd-1
    if (bridge_fd > 3)
    {
      if (syscall(__NR_close_range, 3, bridge_fd - 1, 0) == -1 && errno == ENOSYS)
        close_range_failed = 1;
    }
    // Close FDs bridge_fd+1 to max
    if (syscall(__NR_close_range, bridge_fd + 1, ~0U, 0) == -1 && errno == ENOSYS)
      close_range_failed = 1;
  }
  else
  {
    // bridge_fd is 0, 1, or 2 - just close everything after 2
    if (syscall(__NR_close_range, 3, ~0U, 0) == -1 && errno == ENOSYS)
      close_range_failed = 1;
  }

  // Fallback: manual loop for older kernels without close_range
  if (close_range_failed)
  {
    struct rlimit rl;
    if (getrlimit(RLIMIT_NOFILE, &rl) == 0)
    {
      int max_fd = (rl.rlim_cur < 4096) ? (int)rl.rlim_cur : 4096;
      for (int fd = 3; fd < max_fd; fd++)
      {
        if (fd != bridge_fd)
          (void)close(fd);
      }
    }
  }

  const char *argv_child[5];
  int ai = 0;
  argv_child[ai++] = "linuxio-bridge";  // argv[0] for process name
  argv_child[ai++] = "--env";
  argv_child[ai++] = env_mode ? env_mode : "production";
  if (verbose)
    argv_child[ai++] = "--verbose";
  argv_child[ai++] = NULL;

  // Execute the validated bridge binary via fd (prevents TOCTOU)
  // Try execveat first (Linux 3.19+); fallback to fexecve on ENOSYS
#ifndef __NR_execveat
  #define __NR_execveat 322
#endif
  long ret = syscall(__NR_execveat, bridge_fd, "", ARGV_UNCONST(argv_child), environ, AT_EMPTY_PATH);

  // Fallback for kernels without execveat (< 3.19)
  if (ret == -1 && errno == ENOSYS)
  {
    // Read the real path from /proc/self/fd/bridge_fd
    char fdpath[64], realpath_buf[PATH_MAX];
    safe_snprintf(fdpath, sizeof(fdpath), "/proc/self/fd/%d", bridge_fd);
    ssize_t len = readlink(fdpath, realpath_buf, sizeof(realpath_buf) - 1);
    if (len > 0)
    {
      realpath_buf[len] = '\0';
      // Close bridge_fd before exec (no longer needed)
      close(bridge_fd);
      // Use the real path we validated earlier
      execv(realpath_buf, ARGV_UNCONST(argv_child));
    }
  }

  _exit(127);
}

// Handle a single client request
static int handle_client(int input_fd, int output_fd)
{
  // Read request (newline-terminated JSON)
  char reqbuf[8192];
  ssize_t total = 0;
  while (total < (ssize_t)sizeof(reqbuf) - 1)
  {
    ssize_t n = read(input_fd, reqbuf + total, sizeof(reqbuf) - 1 - (size_t)total);
    if (n <= 0)
      break;
    total += n;
    // Check for newline
    char *nl = memchr(reqbuf, '\n', (size_t)total);
    if (nl)
    {
      // Truncate at newline to prevent request smuggling
      *nl = '\0';
      total = nl - reqbuf;
      break;
    }
  }
  reqbuf[total] = '\0';

  if (total == 0)
  {
    send_response(output_fd, STATUS_ERROR, "empty request", NULL, NULL, NULL);
    secure_bzero(reqbuf, sizeof(reqbuf));
    return 1;
  }

  // Parse JSON fields (sizes from linuxio_protocol.h)
  char user[PROTO_MAX_USERNAME] = "";
  char password[PROTO_MAX_PASSWORD] = "";
  char session_id[PROTO_MAX_SESSION_ID] = "";
  char socket_path[PROTO_MAX_SOCKET_PATH] = "";
  char bridge_path[PROTO_MAX_BRIDGE_PATH] = "";
  char env_mode[PROTO_MAX_ENV_MODE] = "";
  char verbose_str[16] = "";
  char secret[PROTO_MAX_SECRET] = "";
  char server_base_url[PROTO_MAX_SERVER_URL] = "";
  char server_cert[PROTO_MAX_SERVER_CERT] = "";

  json_get_string(reqbuf, FIELD_USER, user, sizeof(user));
  json_get_string(reqbuf, FIELD_PASSWORD, password, sizeof(password));
  json_get_string(reqbuf, FIELD_SESSION_ID, session_id, sizeof(session_id));
  json_get_string(reqbuf, FIELD_SOCKET_PATH, socket_path, sizeof(socket_path));
  json_get_string(reqbuf, FIELD_BRIDGE_PATH, bridge_path, sizeof(bridge_path));
  json_get_string(reqbuf, FIELD_ENV, env_mode, sizeof(env_mode));
  json_get_string(reqbuf, FIELD_VERBOSE, verbose_str, sizeof(verbose_str));
  json_get_string(reqbuf, FIELD_SECRET, secret, sizeof(secret));
  json_get_string(reqbuf, FIELD_SERVER_BASE_URL, server_base_url, sizeof(server_base_url));
  json_get_string(reqbuf, FIELD_SERVER_CERT, server_cert, sizeof(server_cert));
  secure_bzero(reqbuf, sizeof(reqbuf));

  // Validate required fields
  if (!user[0] || !session_id[0] || !socket_path[0])
  {
    send_response(output_fd, STATUS_ERROR, "missing required fields", NULL, NULL, NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Validate session_id (defense against path injection)
  if (!valid_session_id(session_id))
  {
    send_response(output_fd, STATUS_ERROR, "invalid session_id format", NULL, NULL, NULL);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Validate env_mode (whitelist valid environment modes)
  if (!valid_env_mode(env_mode))
  {
    send_response(output_fd, STATUS_ERROR, "invalid environment mode", NULL, NULL, NULL);
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
    send_response(output_fd, STATUS_ERROR, pam_strerror(NULL, rc), NULL, NULL, NULL);
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
    send_response(output_fd, STATUS_ERROR,
                  "Password has expired. Please change it via SSH or console.",
                  NULL, NULL, NULL);
    pam_end(pamh, rc);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  if (rc == PAM_SUCCESS)
    rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);

  if (rc != PAM_SUCCESS)
  {
    const char *err = pam_strerror(pamh, rc);
    send_response(output_fd, STATUS_ERROR, err, NULL, NULL, NULL);
    pam_end(pamh, rc);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Get user info
  struct passwd *pw = getpwnam(user);
  if (!pw)
  {
    send_response(output_fd, STATUS_ERROR, "user lookup failed", NULL, NULL, NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  journal_infof("auth: PAM auth success for user '%s' (uid=%u)", user, (unsigned)pw->pw_uid);

  // Validate socket path
  if (!valid_socket_path_for_uid(socket_path, pw->pw_uid))
  {
    send_response(output_fd, STATUS_ERROR, "invalid socket path for user", NULL, NULL, NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Prepare runtime directories and get validated user_fd for openat() operations
  int user_fd = ensure_runtime_dirs(pw);
  if (user_fd < 0)
  {
    send_response(output_fd, STATUS_ERROR, "failed to prepare runtime directory", NULL, NULL, NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    secure_bzero(password, sizeof(password));
    return 1;
  }

  // Check sudo capability
  int nopasswd = 0;
  int want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  // Clear password from memory
  secure_bzero(password, sizeof(password));

  const char *mode_str = want_privileged ? MODE_PRIVILEGED : MODE_UNPRIVILEGED;
  const char *envmode = (env_mode[0]) ? env_mode : "production";

  // Validate bridge binary and keep fd open (prevents TOCTOU)
  const char *bridge_bin = bridge_path[0] ? bridge_path : "/usr/local/bin/linuxio-bridge";
  int bridge_fd = -1;
  if (open_and_validate_bridge(bridge_bin, 0, &bridge_fd) != 0)
  {
    send_response(output_fd, STATUS_ERROR, "bridge validation failed", NULL, NULL, NULL);
    close(user_fd);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }
  // Keep bridge_fd open - we'll exec it directly to prevent TOCTOU

  int verbose_flag = (verbose_str[0] == '1' || verbose_str[0] == 't' || verbose_str[0] == 'T');

  // Create pipe for bootstrap data (secrets never touch filesystem)
  int bootstrap_pipe[2];
  if (pipe(bootstrap_pipe) != 0)
  {
    journal_errorf("failed to create bootstrap pipe: %m");
    send_response(output_fd, STATUS_ERROR, "failed to prepare bootstrap", NULL, NULL, NULL);
    close(bridge_fd);
    close(user_fd);
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
    close(user_fd);
    send_response(output_fd, STATUS_ERROR, err, NULL, NULL, NULL);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  pid_t child = spawn_bridge_process(
      pw,
      want_privileged,
      bridge_fd,
      envmode,
      verbose_flag,
      bootstrap_pipe[0],  // Pass pipe read end to child (will be stdin)
      session_id,
      socket_path);

  // Parent: close pipe read end (child has it)
  close(bootstrap_pipe[0]);

  if (child < 0)
  {
    close(bootstrap_pipe[1]);
    close(bridge_fd);
    close(user_fd);
    send_response(output_fd, STATUS_ERROR, "failed to spawn bridge", NULL, NULL, NULL);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Parent: write bootstrap JSON to pipe, then close to signal EOF
  int bytes_written = write_bootstrap_json(
      bootstrap_pipe[1],
      session_id,
      user,
      pw->pw_uid,
      pw->pw_gid,
      secret,
      socket_path,
      server_base_url,
      server_cert,
      verbose_flag,
      -1);
  close(bootstrap_pipe[1]);

  if (bytes_written < 0)
  {
    journal_errorf("failed to write bootstrap JSON to pipe");
    close(bridge_fd);
    close(user_fd);
    send_response(output_fd, STATUS_ERROR, "bootstrap communication failed", NULL, NULL, NULL);
    kill(child, SIGTERM);
    (void)waitpid(child, NULL, 0);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Close fds we don't need anymore (bridge_fd was dup'd by child, user_fd no longer needed)
  close(bridge_fd);
  close(user_fd);

  int wait_ms = env_get_int("LINUXIO_BRIDGE_START_TIMEOUT_MS", BRIDGE_START_TIMEOUT_MS, 1000, 30000);
  if (wait_for_bridge_socket(socket_path, wait_ms) != 0)
  {
    journal_errorf("bridge did not create socket in time");
    send_response(output_fd, STATUS_ERROR, "bridge startup failed", NULL, NULL, NULL);
    kill(child, SIGTERM);
    (void)waitpid(child, NULL, 0);
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 1;
  }

  // Trim trailing newline from MOTD if present
  if (appdata.motd_len > 0 && appdata.motd[appdata.motd_len - 1] == '\n')
  {
    appdata.motd[appdata.motd_len - 1] = '\0';
  }

  send_response(output_fd, STATUS_OK, NULL, mode_str, socket_path,
                appdata.motd_len > 0 ? appdata.motd : NULL);
  if (input_fd >= 0)
    close(input_fd);
  if (output_fd >= 0 && output_fd != input_fd)
    close(output_fd);

  journal_infof("auth: bridge spawned for user '%s' mode=%s socket=%s",
                user, mode_str, socket_path);

  // Wipe sensitive data
  secure_bzero(secret, sizeof(secret));
  secure_bzero(server_base_url, sizeof(server_base_url));
  secure_bzero(server_cert, sizeof(server_cert));

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
  (void)argc;
  (void)argv;

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
