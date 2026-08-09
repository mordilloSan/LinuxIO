// /usr/local/bin/linuxio-auth  (install 0755 root:root, runs via systemd)
// Single-shot mode: read one binary auth request from stdin (socket-activated)
#define _GNU_SOURCE
#include <security/pam_appl.h>
#include <paths.h>
#include <stdint.h>
#include <time.h>
#include <utmp.h>
#include <sys/file.h>
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
#include <sys/syscall.h>
#include <poll.h>
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <syslog.h>
#include <stdarg.h>
#include <limits.h>
#include <sys/prctl.h>
// Safe argv shim for exec* (drops const only at the API boundary)
#define ARGV_UNCONST(a) \
  ((union { const char *const *in; char *const *out; }){.in = (a)}.out)
#include <systemd/sd-journal.h>

// Protocol constants
#include "linuxio_protocol.h"

// Socket timeouts (seconds)
#define SOCKET_READ_TIMEOUT 30
#define SOCKET_WRITE_TIMEOUT 10
// Authentication and bridge startup share this absolute work budget, measured
// from request receipt. The webserver allows 30 seconds for the exchange, so
// the remaining 10 seconds are reserved for sending a typed response and
// cleanup. Synchronous PAM modules cannot be interrupted safely; after they
// return, an expired budget fails before sudo or bridge launch.
#define AUTH_REQUEST_TIMEOUT_SEC 20
// Per-phase bridge READY limit. The environment override is also capped at the
// absolute request budget and then clipped to the time actually remaining.
#define BRIDGE_READY_TIMEOUT_SEC 10
// Grace period between a termination signal and SIGKILL escalation while
// reaping the bridge: a child that ignores SIGTERM must not pin the worker in
// an unbounded wait. Test seam: the hermetic suites shorten it.
#ifndef CHILD_TERM_GRACE_SEC
#define CHILD_TERM_GRACE_SEC 5
#endif
#define JOURNAL_FIELD_BUFFER_SIZE 512
#define LINUXIO_WEB_TTY "web console"
// Hermetic-test seams: the PAM integration suite overrides these at compile
// time to point at a private bridge directory, a non-root bridge owner, and
// tmpfile accounting databases. Production builds keep the defaults.
#ifndef BRIDGE_DIR
#define BRIDGE_DIR "/usr/local/bin"
#endif
#ifndef BRIDGE_NAME
#define BRIDGE_NAME "linuxio-bridge"
#endif
#define BRIDGE_PATH BRIDGE_DIR "/" BRIDGE_NAME
#ifndef LINUXIO_BRIDGE_OWNER
#define LINUXIO_BRIDGE_OWNER ((uid_t)0)
#endif

#ifndef AT_EMPTY_PATH
#define AT_EMPTY_PATH 0x1000
#endif
#ifndef _PATH_BTMP
#define _PATH_BTMP "/var/log/btmp"
#endif
#ifndef _PATH_LASTLOG
#define _PATH_LASTLOG "/var/log/lastlog"
#endif
#ifndef _PATH_UTMP
#define _PATH_UTMP "/var/run/utmp"
#endif
#ifndef _PATH_WTMP
#define _PATH_WTMP "/var/log/wtmp"
#endif
#ifndef LINUXIO_PATH_BTMP
#define LINUXIO_PATH_BTMP _PATH_BTMP
#endif
#ifndef LINUXIO_PATH_LASTLOG
#define LINUXIO_PATH_LASTLOG _PATH_LASTLOG
#endif
#ifndef LINUXIO_PATH_UTMP
#define LINUXIO_PATH_UTMP _PATH_UTMP
#endif
#ifndef LINUXIO_PATH_WTMP
#define LINUXIO_PATH_WTMP _PATH_WTMP
#endif
extern char **environ;

// -------- safe formatting helpers --------
static int safe_vsnprintf(char *dst, size_t dstsz, const char *fmt, va_list ap)
{
  if (!dst || dstsz == 0)
    return -1;
  int n = vsnprintf(dst, dstsz, fmt, ap);
  if (n < 0)
  {
    dst[0] = '\0';
    return -1;
  }
  return n;
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
struct journal_field {
  const char *name;
  const char *value;
};

static void journal_send_formatted(int priority, const struct journal_field *fields,
                                   size_t field_count, const char *fmt, va_list ap)
{
  char buf[512];
  char priority_buf[32];
  char message_buf[sizeof(buf) + 16];
  char field_bufs[8][JOURNAL_FIELD_BUFFER_SIZE];
  struct iovec iov[3 + 8];
  size_t iov_count = 0;

  (void)safe_vsnprintf(buf, sizeof(buf), fmt, ap);
  (void)safe_snprintf(priority_buf, sizeof(priority_buf), "PRIORITY=%i", priority);
  (void)safe_snprintf(message_buf, sizeof(message_buf), "MESSAGE=%s", buf);

  iov[iov_count++] = (struct iovec){.iov_base = message_buf, .iov_len = strlen(message_buf)};
  iov[iov_count++] = (struct iovec){.iov_base = priority_buf, .iov_len = strlen(priority_buf)};
  iov[iov_count++] = (struct iovec){.iov_base = "SYSLOG_IDENTIFIER=linuxio-auth",
                                    .iov_len = strlen("SYSLOG_IDENTIFIER=linuxio-auth")};

  if (field_count > 8)
    field_count = 8;

  for (size_t i = 0; i < field_count; i++)
  {
    if (!fields[i].name || !fields[i].value || fields[i].name[0] == '\0')
      continue;
    if (strcmp(fields[i].name, LINUXIO_JOURNAL_FIELD_SESSION_ID) == 0)
      continue;
    (void)safe_snprintf(field_bufs[i], sizeof(field_bufs[i]), "%s=%s",
                        fields[i].name, fields[i].value);
    iov[iov_count++] = (struct iovec){.iov_base = field_bufs[i], .iov_len = strlen(field_bufs[i])};
  }

  (void)sd_journal_sendv(iov, (int)iov_count);
}

_Static_assert(JOURNAL_FIELD_BUFFER_SIZE >=
                   sizeof("LINUXIO_BRIDGE_ERROR=") + PROTO_MAX_ERROR - 1,
               "journal field buffer must preserve a full bridge error");

static void journal_errorf(const char *fmt, ...)
{
  va_list ap;
  va_start(ap, fmt);
  journal_send_formatted(LOG_ERR, NULL, 0, fmt, ap);
  va_end(ap);
}

static void journal_info_fieldsf(const struct journal_field *fields, size_t field_count,
                                 const char *fmt, ...)
{
  va_list ap;
  va_start(ap, fmt);
  journal_send_formatted(LOG_INFO, fields, field_count, fmt, ap);
  va_end(ap);
}

static void journal_error_fieldsf(const struct journal_field *fields, size_t field_count,
                                  const char *fmt, ...)
{
  va_list ap;
  va_start(ap, fmt);
  journal_send_formatted(LOG_ERR, fields, field_count, fmt, ap);
  va_end(ap);
}

// -------- exact I/O helpers --------
static int deadline_remaining_ns(int64_t deadline_ns, int64_t *remaining_ns);

// Read exactly len bytes without allowing an individual read to extend past
// the request's absolute deadline. The caller supplies a positive monotonic
// deadline; ETIMEDOUT is returned when it expires.
static int read_all_until(int fd, void *buf, size_t len, int64_t deadline_ns)
{
  const int64_t ns_per_second = INT64_C(1000000000);
  unsigned char *p = (unsigned char *)buf;

  while (len > 0)
  {
    int64_t remaining_ns;
    int deadline_state = deadline_remaining_ns(deadline_ns, &remaining_ns);
    if (deadline_state <= 0)
    {
      errno = deadline_state == 0 ? ETIMEDOUT : EIO;
      return -1;
    }

    struct pollfd pfd = {.fd = fd, .events = POLLIN, .revents = 0};
    struct timespec poll_timeout = {
        .tv_sec = (time_t)(remaining_ns / ns_per_second),
        .tv_nsec = (long)(remaining_ns % ns_per_second)};
    int ready = ppoll(&pfd, 1, &poll_timeout, NULL);
    if (ready < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    if (ready == 0)
    {
      errno = ETIMEDOUT;
      return -1;
    }
    if (pfd.revents & POLLNVAL)
    {
      errno = EBADF;
      return -1;
    }

    ssize_t n = read(fd, p, len);
    if (n > 0)
    {
      p += (size_t)n;
      len -= (size_t)n;
      continue;
    }
    if (n == 0)
      return -1; // EOF
    if (errno == EINTR || errno == EAGAIN || errno == EWOULDBLOCK)
      continue;
    return -1;
  }

  return 0;
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
static void secure_bzero(void *p, size_t n)
{
#if defined(__GLIBC__)
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

// -------- Binary protocol read helpers --------
static uint16_t read_u16_be(const uint8_t *buf)
{
  return ((uint16_t)buf[0] << 8) | ((uint16_t)buf[1]);
}

// Read a length-prefixed string from fd into buf (max bufsz-1 chars + null).
// Returns 0 on success, -1 on error (oversized fields and embedded NULs are rejected).
static int read_lenstr_until(int fd, char *buf, size_t bufsz, int64_t deadline_ns)
{
  if (!buf || bufsz == 0)
    return -1;
  buf[0] = '\0';

  uint8_t lenbuf[2];
  if (read_all_until(fd, lenbuf, sizeof(lenbuf), deadline_ns) != 0)
    return -1;

  uint16_t len = read_u16_be(lenbuf);
  if (len == 0)
    return 0;

  if (len >= bufsz)
    return -1;

  if (read_all_until(fd, buf, len, deadline_ns) != 0 || memchr(buf, '\0', len) != NULL)
  {
    secure_bzero(buf, bufsz);
    return -1;
  }

  buf[len] = '\0';
  return 0;
}

// -------- PAM conversation ----
struct pam_appdata {
  const char *username;
  const char *password;
};

struct auth_user {
  uid_t uid;
  gid_t gid;
  char name[PROTO_MAX_USERNAME];
  char dir[PATH_MAX];
};

static void free_pam_responses(struct pam_response *r, int n)
{
  if (!r)
    return;

  for (int i = 0; i < n; i++)
  {
    if (r[i].resp)
    {
      secure_bzero(r[i].resp, strlen(r[i].resp));
      free(r[i].resp);
    }
  }

  free(r);
}

static int copy_auth_user(const struct passwd *pw, struct auth_user *auth_user)
{
  size_t name_len, dir_len;

  if (!pw || !auth_user || !pw->pw_name || !pw->pw_dir)
    return -1;

  name_len = strlen(pw->pw_name);
  dir_len = strlen(pw->pw_dir);
  if (name_len == 0 || name_len >= sizeof(auth_user->name) || dir_len >= sizeof(auth_user->dir))
    return -1;

  memset(auth_user, 0, sizeof(*auth_user));
  auth_user->uid = pw->pw_uid;
  auth_user->gid = pw->pw_gid;
  memcpy(auth_user->name, pw->pw_name, name_len + 1);
  memcpy(auth_user->dir, pw->pw_dir, dir_len + 1);

  return 0;
}

static void copy_fixed_field(char *dst, size_t dstsz, const char *src)
{
  size_t len;

  if (!dst || dstsz == 0)
    return;

  memset(dst, 0, dstsz);
  if (!src)
    return;

  len = strlen(src);
  if (len > dstsz)
    len = dstsz;
  if (len > 0)
    memcpy(dst, src, len);
}

static void encode_ut_id(char id[4], pid_t pid)
{
  static const char alphabet[] = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
  uint32_t value = (uint32_t)pid;

  for (int i = 3; i >= 0; i--)
  {
    id[i] = alphabet[value % 62];
    value /= 62;
  }
}

static int decode_utf8_codepoint(const unsigned char *s, size_t len,
                                 uint32_t *codepoint, size_t *width)
{
  uint32_t value;

  if (!s || len == 0 || !codepoint || !width)
    return 0;

  if (s[0] <= 0x7f)
  {
    *codepoint = s[0];
    *width = 1;
    return 1;
  }

  if (s[0] >= 0xc2 && s[0] <= 0xdf)
  {
    if (len < 2 || (s[1] & 0xc0) != 0x80)
      return 0;
    value = ((uint32_t)(s[0] & 0x1f) << 6) |
            (uint32_t)(s[1] & 0x3f);
    *width = 2;
  }
  else if (s[0] >= 0xe0 && s[0] <= 0xef)
  {
    if (len < 3 || (s[1] & 0xc0) != 0x80 || (s[2] & 0xc0) != 0x80)
      return 0;
    if ((s[0] == 0xe0 && s[1] < 0xa0) ||
        (s[0] == 0xed && s[1] >= 0xa0))
      return 0;
    value = ((uint32_t)(s[0] & 0x0f) << 12) |
            ((uint32_t)(s[1] & 0x3f) << 6) |
            (uint32_t)(s[2] & 0x3f);
    *width = 3;
  }
  else if (s[0] >= 0xf0 && s[0] <= 0xf4)
  {
    if (len < 4 || (s[1] & 0xc0) != 0x80 ||
        (s[2] & 0xc0) != 0x80 || (s[3] & 0xc0) != 0x80)
      return 0;
    if ((s[0] == 0xf0 && s[1] < 0x90) ||
        (s[0] == 0xf4 && s[1] >= 0x90))
      return 0;
    value = ((uint32_t)(s[0] & 0x07) << 18) |
            ((uint32_t)(s[1] & 0x3f) << 12) |
            ((uint32_t)(s[2] & 0x3f) << 6) |
            (uint32_t)(s[3] & 0x3f);
    *width = 4;
  }
  else
  {
    return 0;
  }

  *codepoint = value;
  return 1;
}

static int valid_utf8_identity_field(const char *value, size_t max_len)
{
  const unsigned char *bytes = (const unsigned char *)value;
  size_t len;
  size_t offset = 0;

  if (!value || !value[0])
    return 0;

  len = strlen(value);
  if (len >= max_len)
    return 0;

  while (offset < len)
  {
    uint32_t codepoint;
    size_t width;

    if (!decode_utf8_codepoint(bytes + offset, len - offset, &codepoint, &width))
      return 0;
    if (codepoint <= 0x20 || (codepoint >= 0x7f && codepoint <= 0x9f))
      return 0;
    offset += width;
  }

  return 1;
}

static int valid_username(const char *user)
{
  return valid_utf8_identity_field(user, PROTO_MAX_USERNAME);
}

static int valid_remote_host(const char *remote_host)
{
  return valid_utf8_identity_field(remote_host, PROTO_MAX_REMOTE_HOST);
}

static uint32_t clamp_time_to_u32(time_t value)
{
  if (value <= (time_t)0)
    return 0;
  if ((uintmax_t)value > UINT32_MAX)
    return UINT32_MAX;
  return (uint32_t)value;
}

static int32_t clamp_suseconds_to_i32(suseconds_t value)
{
  if (value <= (suseconds_t)0)
    return 0;
  if (value > (suseconds_t)INT32_MAX)
    return INT32_MAX;
  return (int32_t)value;
}

static int update_lastlog(uid_t uid, const struct timeval *tv, const char *remote_host)
{
  struct lastlog entry;
  off_t offset;
  ssize_t nwritten;
  int fd;
  int locked = 0;
  int ret = -1;

  fd = open(LINUXIO_PATH_LASTLOG, O_RDWR | O_CLOEXEC);
  if (fd < 0)
  {
    if (errno != ENOENT)
      journal_errorf("failed to open %s: %m", LINUXIO_PATH_LASTLOG);
    return -1;
  }

  if (flock(fd, LOCK_EX | LOCK_NB) != 0)
  {
    int lock_errno = errno;

    if (lock_errno == EWOULDBLOCK)
      goto out;

    errno = lock_errno;
    journal_errorf("failed to lock %s for uid=%u: %m", LINUXIO_PATH_LASTLOG, (unsigned)uid);
    goto out;
  }
  locked = 1;

  offset = (off_t)uid * (off_t)sizeof(entry);
  memset(&entry, 0, sizeof(entry));
  entry.ll_time = clamp_time_to_u32(tv->tv_sec);
  copy_fixed_field(entry.ll_host, sizeof(entry.ll_host), remote_host);
  copy_fixed_field(entry.ll_line, sizeof(entry.ll_line), LINUXIO_WEB_TTY);

  nwritten = pwrite(fd, &entry, sizeof(entry), offset);
  if (nwritten != (ssize_t)sizeof(entry))
  {
    if (nwritten < 0)
      journal_errorf("failed to write %s for uid=%u: %m", LINUXIO_PATH_LASTLOG, (unsigned)uid);
    else
      journal_errorf("partial write to %s for uid=%u", LINUXIO_PATH_LASTLOG, (unsigned)uid);
    goto out;
  }

  ret = 0;

out:
  if (locked)
    (void)flock(fd, LOCK_UN);
  close(fd);
  return ret;
}

static int utmp_file_exists(void)
{
  struct stat st;
  return stat(LINUXIO_PATH_UTMP, &st) == 0;
}

static void btmp_log(const char *username, const char *remote_host)
{
  struct timeval tv;
  struct utmp entry;
  int fd;
  ssize_t nwritten;

  gettimeofday(&tv, NULL);
  memset(&entry, 0, sizeof(entry));
  copy_fixed_field(entry.ut_line, sizeof(entry.ut_line), LINUXIO_WEB_TTY);
  copy_fixed_field(entry.ut_host, sizeof(entry.ut_host), remote_host);
  copy_fixed_field(entry.ut_user, sizeof(entry.ut_user), username);
  entry.ut_pid = getpid();
  entry.ut_tv.tv_sec = clamp_time_to_u32(tv.tv_sec);
  entry.ut_tv.tv_usec = clamp_suseconds_to_i32(tv.tv_usec);
  entry.ut_type = LOGIN_PROCESS;

  fd = open(LINUXIO_PATH_BTMP, O_WRONLY | O_APPEND | O_CREAT | O_CLOEXEC, 0660);
  if (fd < 0)
  {
    if (errno != ENOENT)
      journal_errorf("failed to open %s: %m", LINUXIO_PATH_BTMP);
    return;
  }

  nwritten = write(fd, &entry, sizeof(entry));
  if (nwritten != (ssize_t)sizeof(entry))
  {
    if (nwritten < 0)
      journal_errorf("failed to write %s: %m", LINUXIO_PATH_BTMP);
    else
      journal_errorf("partial write to %s", LINUXIO_PATH_BTMP);
  }

  close(fd);
}

static void record_login_start(const struct auth_user *auth_user, const char *remote_host)
{
  struct timeval tv;
  struct utmp ut;

  gettimeofday(&tv, NULL);

  utmpname(LINUXIO_PATH_UTMP);

  memset(&ut, 0, sizeof(ut));
  encode_ut_id(ut.ut_id, getpid());
  copy_fixed_field(ut.ut_line, sizeof(ut.ut_line), LINUXIO_WEB_TTY);
  copy_fixed_field(ut.ut_user, sizeof(ut.ut_user), auth_user->name);
  copy_fixed_field(ut.ut_host, sizeof(ut.ut_host), remote_host);
  ut.ut_pid = getpid();
  ut.ut_tv.tv_sec = clamp_time_to_u32(tv.tv_sec);
  ut.ut_tv.tv_usec = clamp_suseconds_to_i32(tv.tv_usec);
  ut.ut_type = USER_PROCESS;

  if (utmp_file_exists())
  {
    setutent();
    if (!pututline(&ut))
      journal_errorf("failed to write %s: %m", LINUXIO_PATH_UTMP);
    endutent();
  }
  updwtmp(LINUXIO_PATH_WTMP, &ut);

  (void)update_lastlog(auth_user->uid, &tv, remote_host);
}

static void record_login_end(void)
{
  struct timeval tv;
  struct utmp ut;

  gettimeofday(&tv, NULL);

  utmpname(LINUXIO_PATH_UTMP);

  memset(&ut, 0, sizeof(ut));
  encode_ut_id(ut.ut_id, getpid());
  copy_fixed_field(ut.ut_line, sizeof(ut.ut_line), LINUXIO_WEB_TTY);
  ut.ut_pid = getpid();
  ut.ut_tv.tv_sec = clamp_time_to_u32(tv.tv_sec);
  ut.ut_tv.tv_usec = clamp_suseconds_to_i32(tv.tv_usec);
  ut.ut_type = DEAD_PROCESS;

  if (utmp_file_exists())
  {
    setutent();
    if (!pututline(&ut))
      journal_errorf("failed to update %s: %m", LINUXIO_PATH_UTMP);
    endutent();
  }
  updwtmp(LINUXIO_PATH_WTMP, &ut);
}

static int pam_conv_func(int n, const struct pam_message **msg, struct pam_response **resp, void *appdata_ptr)
{
  const struct pam_appdata *appdata = (const struct pam_appdata *)appdata_ptr;
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
          free_pam_responses(r, i);
          return PAM_CONV_ERR;
        }
      }
      break;

    case PAM_PROMPT_ECHO_ON:
      if (appdata && appdata->username)
      {
        r[i].resp = strdup(appdata->username);
        if (!r[i].resp)
        {
          free_pam_responses(r, i);
          return PAM_CONV_ERR;
        }
      }
      break;

    case PAM_TEXT_INFO:
    case PAM_ERROR_MSG:
      // Ignore informational messages
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
static int validate_bridge_policy(const struct stat *st, uid_t required_owner)
{
  if (!st || !S_ISREG(st->st_mode))
    return -1;
  if ((st->st_mode & (S_IWGRP | S_IWOTH)) != 0)
    return -1;
  if (st->st_uid != required_owner)
    return -1;
  if ((st->st_mode & 0111) == 0)
    return -1;
  if (st->st_mode & (S_ISUID | S_ISGID))
    return -1;
  return 0;
}

static int validate_parent_dir_policy(const struct stat *ds, uid_t required_owner)
{
  if (!ds || !S_ISDIR(ds->st_mode))
    return -1;
  if (ds->st_uid != required_owner)
    return -1;
  if (ds->st_mode & (S_IWGRP | S_IWOTH))
    return -1;
  return 0;
}

static int open_and_validate_bridge(uid_t required_owner, int *out_fd)
{
  int result = -1;
  int dfd = -1;
  int fd = -1;
  struct stat ds;
  struct stat st;

  if (!out_fd)
    return -1;
  *out_fd = -1;

  dfd = open(BRIDGE_DIR, O_PATH | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
  if (dfd < 0)
  {
    perror("open bridge directory");
    goto out;
  }
  if (fstat(dfd, &ds) != 0)
  {
    perror("fstat bridge directory");
    goto out;
  }
  if (validate_parent_dir_policy(&ds, required_owner) != 0)
    goto out;

  fd = openat(dfd, BRIDGE_NAME, O_PATH | O_NOFOLLOW | O_CLOEXEC);
  if (fd < 0)
  {
    perror("open bridge");
    goto out;
  }
  if (fstat(fd, &st) != 0)
  {
    perror("fstat bridge");
    goto out;
  }
  if (validate_bridge_policy(&st, required_owner) != 0)
    goto out;

  *out_fd = fd;
  fd = -1;
  result = 0;

out:
  if (fd >= 0)
    close(fd);
  if (dfd >= 0)
    close(dfd);
  return result;
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

  // Flags (1 byte). READY_ACK is always set: it tells the bridge the
  // startup-status fd stays open across exec and a ready/error byte is
  // required before the launcher reports login success.
  uint8_t flags = PROTO_FLAG_READY_ACK;
  if (verbose)
    flags |= PROTO_FLAG_VERBOSE;
  if (privileged)
    flags |= PROTO_FLAG_PRIVILEGED;
  header[pos] = flags;

  // Write fixed header
  if (write_all(fd, header, PROTO_HEADER_SIZE) != 0)
    return -1;

  // Write variable-length fields (length-prefixed)
  if (write_lenstr(fd, session_id) != 0)
    return -1;
  if (write_lenstr(fd, username) != 0)
    return -1;

  return 0;
}

static pid_t waitpid_nointr(pid_t pid, int *status, int options)
{
  pid_t result;

  do
  {
    result = waitpid(pid, status, options);
  } while (result < 0 && errno == EINTR);

  return result;
}

// -------- service-stop handling --------
// A service stop must run the launcher epilogue (end accounting, PAM session
// close, credential deletion, pam_end) instead of dying inside the
// session-long child wait. SIGTERM stays blocked for the whole request, so
// PAM calls and the epilogue itself cannot be interrupted; it is delivered
// only inside the session wait's ppoll with this mask, which re-checks the
// flag race-free (a signal either sets the flag before the check or
// interrupts the ppoll - it cannot be lost between them).
static volatile sig_atomic_t g_shutdown_requested = 0;
static sigset_t g_session_wait_sigmask;

static void handle_shutdown_signal(int sig)
{
  (void)sig;
  g_shutdown_requested = 1;
}

static int install_shutdown_handling(void)
{
  struct sigaction term_action;
  sigset_t term_block;

  memset(&term_action, 0, sizeof(term_action));
  term_action.sa_handler = handle_shutdown_signal;
  term_action.sa_flags = 0; // no SA_RESTART: the session wait needs EINTR
  if (sigemptyset(&term_action.sa_mask) != 0 ||
      sigemptyset(&term_block) != 0 ||
      sigaddset(&term_block, SIGTERM) != 0 ||
      sigaction(SIGTERM, &term_action, NULL) != 0 ||
      sigprocmask(SIG_BLOCK, &term_block, &g_session_wait_sigmask) != 0 ||
      sigdelset(&g_session_wait_sigmask, SIGTERM) != 0)
    return -1;
  return 0;
}

static int wait_for_child_with_timeout(pid_t pid, int timeout_sec,
                                       int64_t request_deadline_ns);

// Terminate and reap the owned child without an unbounded block: a child that
// ignores the termination signal is escalated to SIGKILL once the grace
// period expires (wait_for_child_with_timeout kills and reaps on timeout or
// wait failure). SIGKILL cannot be ignored, so it waits directly.
static void terminate_and_reap_child(pid_t pid, int signal_number)
{
  if (pid <= 0)
    return;

  if (signal_number > 0)
    (void)kill(pid, signal_number);

  if (signal_number == SIGKILL)
  {
    (void)waitpid_nointr(pid, NULL, 0);
    return;
  }

  (void)wait_for_child_with_timeout(pid, CHILD_TERM_GRACE_SEC, 0);
}

static int child_status_code(int status)
{
  if (WIFEXITED(status))
    return WEXITSTATUS(status);
  if (WIFSIGNALED(status))
    return 128 + WTERMSIG(status);
  return -1;
}

static int monotonic_now_ns(int64_t *now_ns)
{
  const int64_t ns_per_second = INT64_C(1000000000);
  struct timespec now;

  if (!now_ns || clock_gettime(CLOCK_MONOTONIC, &now) != 0)
    return -1;
  if (now.tv_sec < 0 || now.tv_nsec < 0 || now.tv_nsec >= ns_per_second)
    return -1;
  if ((uintmax_t)now.tv_sec > (uintmax_t)(INT64_MAX / ns_per_second))
    return -1;

  *now_ns = (int64_t)now.tv_sec * ns_per_second + (int64_t)now.tv_nsec;
  return 0;
}

static int deadline_remaining_ns(int64_t deadline_ns, int64_t *remaining_ns)
{
  int64_t now_ns;

  if (!remaining_ns || monotonic_now_ns(&now_ns) != 0)
    return -1;
  if (now_ns >= deadline_ns)
  {
    *remaining_ns = 0;
    return 0;
  }

  *remaining_ns = deadline_ns - now_ns;
  return 1;
}

// Builds a relative phase deadline, clipped to an optional absolute request
// deadline. A non-positive request deadline means no outer clipping.
static int bounded_deadline_ms(int timeout_ms, int64_t request_deadline_ns,
                               int64_t *deadline_ns)
{
  int64_t now_ns;
  int64_t timeout_ns;

  if (!deadline_ns || timeout_ms <= 0 || monotonic_now_ns(&now_ns) != 0)
  {
    errno = EIO;
    return -1;
  }

  timeout_ns = (int64_t)timeout_ms * INT64_C(1000000);
  if (now_ns > INT64_MAX - timeout_ns)
  {
    errno = EOVERFLOW;
    return -1;
  }

  *deadline_ns = now_ns + timeout_ns;
  if (request_deadline_ns > 0 && request_deadline_ns < *deadline_ns)
    *deadline_ns = request_deadline_ns;
  return 0;
}

// Outcomes of waiting for the bridge's startup-status byte.
enum bridge_startup_result
{
  BRIDGE_STARTUP_READY = 0,
  BRIDGE_STARTUP_EXEC_FAILED,    // PROTO_STARTUP_EXEC_FAILED from the pre-exec child
  BRIDGE_STARTUP_REPORTED_ERROR, // PROTO_STARTUP_ERROR (+ optional message)
  BRIDGE_STARTUP_EOF,            // fd closed before any status byte
  BRIDGE_STARTUP_TIMEOUT,
  BRIDGE_STARTUP_WAIT_ERROR,    // poll/read failure, errno preserved
  BRIDGE_STARTUP_PROTOCOL_ERROR // unknown status byte
};

// Reads one byte from the status fd within the deadline. Returns 1 with the
// byte, 0 on EOF, -1 on timeout (errno ETIMEDOUT) or failure (errno set).
static int read_status_byte_until(int fd, int64_t deadline_ns, uint8_t *out)
{
  for (;;)
  {
    int64_t remaining_ns = 0;
    int deadline_state = deadline_remaining_ns(deadline_ns, &remaining_ns);
    if (deadline_state < 0)
    {
      errno = EIO;
      return -1;
    }
    if (deadline_state == 0)
    {
      errno = ETIMEDOUT;
      return -1;
    }

    struct pollfd pfd = {.fd = fd, .events = POLLIN, .revents = 0};
    struct timespec poll_timeout = {
        .tv_sec = (time_t)(remaining_ns / INT64_C(1000000000)),
        .tv_nsec = (long)(remaining_ns % INT64_C(1000000000))};
    int ready = ppoll(&pfd, 1, &poll_timeout, NULL);
    if (ready < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    if (ready == 0)
    {
      errno = ETIMEDOUT;
      return -1;
    }

    ssize_t n = read(fd, out, 1);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    return n > 0 ? 1 : 0;
  }
}

// Waits for the bridge's startup-status byte (see linuxio_protocol.h). On
// BRIDGE_STARTUP_REPORTED_ERROR, msg receives the bridge's message (possibly
// empty) with non-printable bytes blanked so nothing hostile reaches the
// journal or the client error string. On BRIDGE_STARTUP_PROTOCOL_ERROR,
// *bad_byte holds the unknown byte. errno describes BRIDGE_STARTUP_WAIT_ERROR.
static enum bridge_startup_result wait_for_bridge_startup(
    int status_fd, int timeout_ms, int64_t request_deadline_ns,
    char *msg, size_t msg_sz, uint8_t *bad_byte)
{
  if (msg && msg_sz > 0)
    msg[0] = '\0';

  int64_t deadline_ns = 0;
  if (status_fd < 0 ||
      bounded_deadline_ms(timeout_ms, request_deadline_ns, &deadline_ns) != 0)
  {
    return BRIDGE_STARTUP_WAIT_ERROR;
  }

  uint8_t status = 0;
  int rc = read_status_byte_until(status_fd, deadline_ns, &status);
  if (rc < 0)
    return errno == ETIMEDOUT ? BRIDGE_STARTUP_TIMEOUT : BRIDGE_STARTUP_WAIT_ERROR;
  if (rc == 0)
    return BRIDGE_STARTUP_EOF;

  switch (status)
  {
  case PROTO_STARTUP_READY:
    return BRIDGE_STARTUP_READY;
  case PROTO_STARTUP_EXEC_FAILED:
    return BRIDGE_STARTUP_EXEC_FAILED;
  case PROTO_STARTUP_ERROR:
    break;
  default:
    if (bad_byte)
      *bad_byte = status;
    return BRIDGE_STARTUP_PROTOCOL_ERROR;
  }

  // Collect the optional error message until EOF, buffer full, or deadline.
  // The outcome is already "failed"; message trouble must not change it.
  size_t used = 0;
  while (msg && used + 1 < msg_sz)
  {
    uint8_t ch = 0;
    rc = read_status_byte_until(status_fd, deadline_ns, &ch);
    if (rc <= 0)
      break;
    msg[used++] = (ch >= 0x20 && ch < 0x7f) ? (char)ch : ' ';
  }
  if (msg && msg_sz > 0)
    msg[used] = '\0';
  return BRIDGE_STARTUP_REPORTED_ERROR;
}

// Releases a READY bridge only after the authentication response has been
// written in full. Keeping this as a distinct operation makes the transport
// ownership handoff explicit: before GO only the launcher may write the client
// connection; after GO the bridge owns Yamux on that connection.
static int release_bridge_startup(int status_fd)
{
  const uint8_t go = PROTO_STARTUP_GO;
  return write_all(status_fd, &go, sizeof(go));
}

static int socket_write_all_until(int fd, const void *buf, size_t len,
                                  int64_t deadline_ns)
{
  const int64_t ns_per_second = INT64_C(1000000000);
  const unsigned char *p = (const unsigned char *)buf;

  while (len > 0)
  {
    ssize_t n = write(fd, p, len);
    if (n > 0)
    {
      p += (size_t)n;
      len -= (size_t)n;
      continue;
    }
    if (n == 0)
    {
      errno = EPIPE;
      return -1;
    }
    if (errno == EINTR)
      continue;
    if (errno != EAGAIN)
      return -1;

    int64_t remaining_ns;
    int deadline_state = deadline_remaining_ns(deadline_ns, &remaining_ns);
    if (deadline_state <= 0)
    {
      errno = deadline_state == 0 ? ETIMEDOUT : EIO;
      return -1;
    }

    struct pollfd pfd = {
        .fd = fd,
        .events = POLLOUT,
        .revents = 0,
    };
    struct timespec poll_timeout = {
        .tv_sec = (time_t)(remaining_ns / ns_per_second),
        .tv_nsec = (long)(remaining_ns % ns_per_second),
    };
    int ready = ppoll(&pfd, 1, &poll_timeout, NULL);
    if (ready < 0 && errno == EINTR)
      continue;
    if (ready <= 0)
    {
      if (ready == 0)
        errno = ETIMEDOUT;
      return -1;
    }
    if (pfd.revents & POLLNVAL)
    {
      errno = EBADF;
      return -1;
    }
  }

  return 0;
}

static int socket_write_lenstr_until(int fd, const char *s, int64_t deadline_ns)
{
  size_t len = s ? strlen(s) : 0;
  if (len > UINT16_MAX)
    len = UINT16_MAX;

  uint8_t lenbuf[2];
  write_u16_be(lenbuf, (uint16_t)len);
  if (socket_write_all_until(fd, lenbuf, sizeof(lenbuf), deadline_ns) != 0)
    return -1;
  if (len > 0 && socket_write_all_until(fd, s, len, deadline_ns) != 0)
    return -1;
  return 0;
}

struct auth_timing
{
  int64_t request_started_ns;
  int64_t pam_started_ns;
  int64_t pam_completed_ns;
  int64_t sudo_started_ns;
  int64_t sudo_completed_ns;
  int64_t session_setup_started_ns;
  int64_t session_opened_ns;
  int64_t bridge_start_started_ns;
  int64_t bridge_ready_ns;
  int64_t accounting_started_ns;
  int64_t accounting_completed_ns;
  int64_t request_completed_ns;
};

static int64_t elapsed_us(int64_t started_ns, int64_t completed_ns)
{
  if (started_ns <= 0 || completed_ns < started_ns)
    return -1;
  return (completed_ns - started_ns) / INT64_C(1000);
}

static void log_auth_timing(const struct auth_timing *timing, uid_t uid,
                            const char *mode_name)
{
  char uid_buf[32];
  char pam_buf[32], sudo_buf[32], session_buf[32];
  char bridge_buf[32], accounting_buf[32], total_buf[32];
  int64_t pam_us, sudo_us, session_us, bridge_us, accounting_us, total_us;

  if (!timing || !mode_name)
    return;

  pam_us = elapsed_us(timing->pam_started_ns, timing->pam_completed_ns);
  sudo_us = elapsed_us(timing->sudo_started_ns, timing->sudo_completed_ns);
  session_us = elapsed_us(timing->session_setup_started_ns, timing->session_opened_ns);
  bridge_us = elapsed_us(timing->bridge_start_started_ns, timing->bridge_ready_ns);
  accounting_us = elapsed_us(timing->accounting_started_ns, timing->accounting_completed_ns);
  total_us = elapsed_us(timing->request_started_ns, timing->request_completed_ns);
  if (pam_us < 0 || sudo_us < 0 || session_us < 0 ||
      bridge_us < 0 || accounting_us < 0 || total_us < 0)
    return;

  (void)safe_snprintf(uid_buf, sizeof(uid_buf), "%u", (unsigned)uid);
  (void)safe_snprintf(pam_buf, sizeof(pam_buf), "%lld", (long long)pam_us);
  (void)safe_snprintf(sudo_buf, sizeof(sudo_buf), "%lld", (long long)sudo_us);
  (void)safe_snprintf(session_buf, sizeof(session_buf), "%lld", (long long)session_us);
  (void)safe_snprintf(bridge_buf, sizeof(bridge_buf), "%lld", (long long)bridge_us);
  (void)safe_snprintf(accounting_buf, sizeof(accounting_buf), "%lld", (long long)accounting_us);
  (void)safe_snprintf(total_buf, sizeof(total_buf), "%lld", (long long)total_us);

  const struct journal_field fields[] = {
      {"LINUXIO_UID", uid_buf},
      {"LINUXIO_MODE", mode_name},
      {"LINUXIO_AUTH_PAM_US", pam_buf},
      {"LINUXIO_AUTH_SUDO_US", sudo_buf},
      {"LINUXIO_AUTH_SESSION_SETUP_US", session_buf},
      {"LINUXIO_AUTH_BRIDGE_START_US", bridge_buf},
      {"LINUXIO_AUTH_ACCOUNTING_US", accounting_buf},
      {"LINUXIO_AUTH_TOTAL_US", total_buf},
  };
  journal_info_fieldsf(fields, sizeof(fields) / sizeof(fields[0]), "auth timing");
}

static int wait_for_child_with_timeout(pid_t pid, int timeout_sec,
                                       int64_t request_deadline_ns)
{
  const int64_t ns_per_second = INT64_C(1000000000);
  int64_t deadline_ns;
  int pidfd = -1;
  int status = 0;
  int failure_errno = EIO;

  if (pid <= 0 || timeout_sec <= 0)
  {
    failure_errno = EINVAL;
    goto fail;
  }
  if (bounded_deadline_ms(timeout_sec * 1000, request_deadline_ns, &deadline_ns) != 0)
  {
    failure_errno = errno != 0 ? errno : EIO;
    goto fail;
  }

#ifdef SYS_pidfd_open
  pidfd = (int)syscall(SYS_pidfd_open, pid, 0);
  if (pidfd < 0 && errno != ENOSYS)
  {
    failure_errno = errno;
    goto fail;
  }
#endif

  for (;;)
  {
    int64_t remaining_ns;
    int deadline_state = deadline_remaining_ns(deadline_ns, &remaining_ns);
    if (deadline_state < 0)
    {
      failure_errno = EIO;
      goto fail;
    }
    if (deadline_state == 0)
    {
      failure_errno = ETIMEDOUT;
      break;
    }

    if (pidfd >= 0)
    {
      struct timespec poll_timeout = {
          .tv_sec = (time_t)(remaining_ns / ns_per_second),
          .tv_nsec = (long)(remaining_ns % ns_per_second)};
      struct pollfd pfd = {
          .fd = pidfd,
          .events = POLLIN,
          .revents = 0};
      int poll_result = ppoll(&pfd, 1, &poll_timeout, NULL);

      if (poll_result < 0)
      {
        if (errno == EINTR)
          continue;
        failure_errno = errno;
        goto fail;
      }
      if (poll_result == 0)
      {
        failure_errno = ETIMEDOUT;
        break;
      }
      if ((pfd.revents & POLLIN) == 0)
      {
        failure_errno = EIO;
        goto fail;
      }

      // Once ppoll reports readiness within its exact remaining timeout,
      // prefer the child's real status even if this parent resumes later.
      if (waitpid_nointr(pid, &status, 0) != pid)
      {
        failure_errno = errno;
        goto fail;
      }
      close(pidfd);
      return child_status_code(status);
    }

    {
      pid_t waited = waitpid_nointr(pid, &status, WNOHANG);
      if (waited == pid)
        return child_status_code(status);
      if (waited < 0)
      {
        failure_errno = errno;
        goto fail;
      }
    }

    {
      const int64_t fallback_sleep_ns = INT64_C(5000000);
      int64_t sleep_ns = remaining_ns < fallback_sleep_ns ? remaining_ns : fallback_sleep_ns;
      struct timespec sleep_time = {
          .tv_sec = (time_t)(sleep_ns / ns_per_second),
          .tv_nsec = (long)(sleep_ns % ns_per_second)};
      if (nanosleep(&sleep_time, NULL) != 0 && errno != EINTR)
      {
        failure_errno = errno;
        goto fail;
      }
    }
  }

  {
    // A final nonblocking reap closes the deadline boundary without reviving
    // the old false-timeout race. Prefer an observed exit over killing it.
    pid_t waited = waitpid_nointr(pid, &status, WNOHANG);
    if (waited == pid)
    {
      if (pidfd >= 0)
        close(pidfd);
      return child_status_code(status);
    }
    if (waited < 0)
    {
      failure_errno = errno;
      goto fail;
    }
  }

fail:
  if (pidfd >= 0)
    close(pidfd);
  terminate_and_reap_child(pid, SIGKILL);
  errno = failure_errno;
  return -1;
}

// Session-long wait for the supervised bridge. Returns 1 with *status when
// the child exited, 0 when a service stop was requested (the child is left
// alive for the caller's termination path), or -1 with errno on wait failure.
// SIGTERM is deliverable only inside the ppoll calls, so a stop request
// cannot be lost between the flag check and the blocking wait.
static int wait_for_session_child(pid_t pid, int *status)
{
  int pidfd = -1;

  if (pid <= 0 || !status)
  {
    errno = EINVAL;
    return -1;
  }

#ifdef SYS_pidfd_open
  pidfd = (int)syscall(SYS_pidfd_open, pid, 0);
  if (pidfd < 0 && errno != ENOSYS)
    return -1;
#endif

  for (;;)
  {
    if (g_shutdown_requested)
    {
      if (pidfd >= 0)
        close(pidfd);
      return 0;
    }

    pid_t waited = waitpid(pid, status, WNOHANG);
    if (waited == pid)
    {
      if (pidfd >= 0)
        close(pidfd);
      return 1;
    }
    if (waited < 0 && errno != EINTR)
    {
      int wait_errno = errno;
      if (pidfd >= 0)
        close(pidfd);
      errno = wait_errno;
      return -1;
    }

    if (pidfd >= 0)
    {
      struct pollfd pfd = {.fd = pidfd, .events = POLLIN, .revents = 0};
      if (ppoll(&pfd, 1, NULL, &g_session_wait_sigmask) < 0 && errno != EINTR)
      {
        int poll_errno = errno;
        close(pidfd);
        errno = poll_errno;
        return -1;
      }
    }
    else
    {
      // No pidfd support: bounded sleep with SIGTERM deliverable, then
      // re-check the child. Stop and reap latency are capped by the interval.
      struct timespec poll_interval = {.tv_sec = 0, .tv_nsec = 500000000L};
      if (ppoll(NULL, 0, &poll_interval, &g_session_wait_sigmask) < 0 &&
          errno != EINTR)
        return -1;
    }
  }
}

// sudo policy probing. This runs from the root launcher context: -U asks
// sudoers about the authenticated identity without changing credentials.
static int run_sudo_policy_query(const char *const argv[], int timeout_sec,
                                 int64_t request_deadline_ns)
{
  pid_t pid = fork();
  if (pid < 0)
    return -1;
  if (pid == 0)
  {
    // The launcher keeps SIGTERM blocked for its own shutdown handling and
    // the mask survives exec; sudo must stay terminable.
    sigset_t child_mask;
    if (sigemptyset(&child_mask) != 0 ||
        sigprocmask(SIG_SETMASK, &child_mask, NULL) != 0)
      _exit(127);
    (void)signal(SIGTERM, SIG_DFL);

    int devnull = open("/dev/null", O_RDWR);
    if (devnull < 0 || dup2(devnull, STDIN_FILENO) < 0 ||
        dup2(devnull, STDOUT_FILENO) < 0)
      _exit(127);
    if (devnull != STDIN_FILENO && devnull != STDOUT_FILENO)
      close(devnull);

    if (clearenv() != 0 ||
        setenv("PATH",
               "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", 1) != 0 ||
        setenv("LANG", "C", 1) != 0)
      _exit(127);
    execv("/usr/bin/sudo", ARGV_UNCONST(argv));

    _exit(127);
  }
  return wait_for_child_with_timeout(pid, timeout_sec, request_deadline_ns);
}

static int build_sudo_policy_argv(const char *canonical_username,
                                  const char **argv, size_t argv_cap)
{
  if (!canonical_username || !*canonical_username || !argv || argv_cap < 10)
    return -1;
  argv[0] = "/usr/bin/sudo";
  argv[1] = "-n";
  argv[2] = "-l";
  argv[3] = "-U";
  argv[4] = canonical_username;
  argv[5] = "-u";
  argv[6] = "root";
  argv[7] = "--";
  argv[8] = BRIDGE_PATH;
  argv[9] = NULL;
  return 0;
}

// Privileged-mode policy: the user must be allowed by sudoers to run the
// bridge binary as root. "sudo -n -l -U <user> -u root -- <cmd>" exits 0 only
// if the security policy permits the specified user to run that exact command
// (sudo(8)), so sudoers stays the single source of truth: full admins (ALL)
// qualify, users with unrelated narrow rules do not, and an admin can grant
// web admin access explicitly by whitelisting BRIDGE_PATH in sudoers. This
// probe is authorization only: the already-root launcher executes the bridge
// directly, so sudo runtime tags and environment rules do not wrap it.
static int user_can_run_bridge_as_root(const char *canonical_username,
                                       int64_t request_deadline_ns)
{
  if (!canonical_username || !*canonical_username)
    return 0;
  int timeout_sec = env_get_int("LINUXIO_SUDO_TIMEOUT", 4, 1, 30);
  const char *argv_list[10];
  if (build_sudo_policy_argv(canonical_username, argv_list,
                             sizeof(argv_list) / sizeof(argv_list[0])) != 0)
    return 0;
  int rc = run_sudo_policy_query(argv_list, timeout_sec, request_deadline_ns);
  if (rc < 0)
  {
    journal_errorf("sudo policy probe infrastructure failure: %m");
    return 0;
  }
  if (rc == 127)
  {
    journal_errorf("sudo policy probe failed to execute sudo");
    return 0;
  }
  return rc == 0;
}

// Fatal error in the forked bridge child, pre-exec: emit a diagnostic on
// stderr (wired to the journal) before _exit, so a bridge that dies during
// setup is attributable instead of a silent status-127 exit.
__attribute__((__noreturn__)) static void child_die(int status_fd, const char *what)
{
  int saved_errno = errno;
  uint8_t err_byte = PROTO_STARTUP_EXEC_FAILED;
  if (status_fd >= 0)
    (void)write_all(status_fd, &err_byte, sizeof(err_byte));
  errno = saved_errno;
  char buf[256];
  (void)safe_snprintf(buf, sizeof(buf), "linuxio-auth: bridge setup failed: %s: %m\n", what);
  (void)write_all(STDERR_FILENO, buf, strlen(buf));
  _exit(127);
}

static void set_cloexec_or_die(int fd, int status_fd, const char *what)
{
  int fdflags = fcntl(fd, F_GETFD);
  if (fdflags < 0)
    child_die(status_fd, what);
  if (fcntl(fd, F_SETFD, fdflags | FD_CLOEXEC) != 0)
    child_die(status_fd, what);
}

static void clear_cloexec_or_die(int fd, int status_fd, const char *what)
{
  int fdflags = fcntl(fd, F_GETFD);
  if (fdflags < 0)
    child_die(status_fd, what);
  if (fcntl(fd, F_SETFD, fdflags & ~FD_CLOEXEC) != 0)
    child_die(status_fd, what);
}

static void drop_to_user(const struct auth_user *auth_user, int status_fd)
{
  if (!auth_user)
    child_die(status_fd, "no user to drop to");
  if (auth_user->uid == 0)
    child_die(status_fd, "refusing unprivileged root session");
  if (setgroups(0, NULL) != 0)
    child_die(status_fd, "setgroups");
  if (initgroups(auth_user->name, auth_user->gid) != 0)
    child_die(status_fd, "initgroups");
  if (setgid(auth_user->gid) != 0)
    child_die(status_fd, "setgid");
  if (setuid(auth_user->uid) != 0)
    child_die(status_fd, "setuid");
  if (setuid(0) == 0)
    child_die(status_fd, "privilege drop verification (setuid(0) unexpectedly succeeded)");
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
// This mirrors the systemd socket policy: the uid/gid come from the kernel
// (SO_PEERCRED); group membership is resolved from the user database.
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

// Returns 1 if the peer's actual supplementary groups as recorded by the
// kernel at connect time include target_gid, 0 if not, -1 if the kernel
// lacks SO_PEERGROUPS (pre-4.10) or the query failed. Race-free, unlike
// parsing /proc/<pid>/status (SO_PEERCRED's pid can be recycled).
#ifndef SO_PEERGROUPS
#define SO_PEERGROUPS 59
#endif
static int peer_in_group_kernel(int fd, gid_t target_gid)
{
  gid_t probe; // len 0: kernel only reports the needed size, never writes
  socklen_t len = 0;
  if (getsockopt(fd, SOL_SOCKET, SO_PEERGROUPS, &probe, &len) == 0)
    return 0; // peer has no supplementary groups
  if (errno != ERANGE || len == 0)
    return -1; // ENOPROTOOPT on old kernels, or other failure

  gid_t *groups = malloc(len);
  if (!groups)
    return -1;
  if (getsockopt(fd, SOL_SOCKET, SO_PEERGROUPS, groups, &len) != 0)
  {
    free(groups);
    return -1;
  }

  int found = 0;
  for (size_t i = 0; i < len / sizeof(gid_t); i++)
  {
    if (groups[i] == target_gid)
    {
      found = 1;
      break;
    }
  }
  free(groups);
  return found;
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
  struct group *gr = getgrnam(AUTH_SOCKET_GROUP);
  if (!gr)
  {
    journal_errorf("group '%s' not found", AUTH_SOCKET_GROUP);
    return -1;
  }

  if (cred.gid == gr->gr_gid)
    return 0;

  // Supplementary groups: prefer the kernel's connect-time record
  // (SO_PEERGROUPS, Linux 4.10+); on older kernels fall back to configured
  // membership from the user database. The fallback reflects what the uid
  // is entitled to rather than the process's current group set - acceptable
  // because a member uid can acquire its own group at will anyway.
  int in_group = peer_in_group_kernel(fd, gr->gr_gid);
  if (in_group < 0)
    in_group = user_in_group(cred.uid, gr->gr_gid);
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

static int replace_stderr_with_devnull(void)
{
  close(STDERR_FILENO);
  int devnull = open("/dev/null", O_WRONLY);
  if (devnull < 0)
    return -1;
  if (devnull != STDERR_FILENO)
  {
    if (dup2(devnull, STDERR_FILENO) < 0)
    {
      int saved_errno = errno;
      close(devnull);
      errno = saved_errno;
      return -1;
    }
    close(devnull);
  }
  return 0;
}

// Send one binary response without depending on socket-level blocking mode.
// Success format:
//   [magic:4][status:1][mode:1][result:1][reserved:1][uid:4][gid:4][len:2][username]
// Error format:
//   [magic:4][status:1][mode:1][result:1][reserved:1][len:2][error]
static int send_response(int fd, uint8_t status, uint8_t mode, uint8_t result_code,
                         const char *error, const char *username, uid_t uid, gid_t gid)
{
  const int64_t ns_per_second = INT64_C(1000000000);
  int64_t now_ns;
  int64_t deadline_ns;
  uint8_t header[PROTO_AUTH_RESP_HEADER_SIZE];

  int fd_flags = fcntl(fd, F_GETFL);
  if (fd_flags < 0 ||
      (!(fd_flags & O_NONBLOCK) && fcntl(fd, F_SETFL, fd_flags | O_NONBLOCK) != 0))
    return -1;

  if (monotonic_now_ns(&now_ns) != 0 ||
      now_ns > INT64_MAX - (int64_t)SOCKET_WRITE_TIMEOUT * ns_per_second)
  {
    errno = EIO;
    return -1;
  }
  deadline_ns = now_ns + (int64_t)SOCKET_WRITE_TIMEOUT * ns_per_second;

  // Magic + version
  header[0] = PROTO_MAGIC_0;
  header[1] = PROTO_MAGIC_1;
  header[2] = PROTO_MAGIC_2;
  header[3] = PROTO_VERSION;

  // Status and mode
  header[4] = status;
  header[5] = mode;

  // Structured result code + reserved
  header[6] = result_code;
  header[7] = 0;

  if (socket_write_all_until(fd, header, PROTO_AUTH_RESP_HEADER_SIZE, deadline_ns) != 0)
    return -1;

  if (status == PROTO_STATUS_OK)
  {
    uint8_t ids[8];
    write_u32_be(ids, (uint32_t)uid);
    write_u32_be(ids + 4, (uint32_t)gid);
    if (socket_write_all_until(fd, ids, sizeof(ids), deadline_ns) != 0)
      return -1;
    return socket_write_lenstr_until(fd, username, deadline_ns);
  }

  // Error responses always carry a length prefix; a NULL error is encoded as
  // an empty string so the peer can consume the complete frame.
  if (status == PROTO_STATUS_ERROR)
    return socket_write_lenstr_until(fd, error, deadline_ns);

  return 0;
}

static void send_error_response(int fd, uint8_t result_code, const char *error)
{
  (void)send_response(fd, PROTO_STATUS_ERROR, 0, result_code, error, NULL, 0, 0);
}

static int send_ok_response(int fd, uint8_t mode, const char *username, uid_t uid, gid_t gid)
{
  return send_response(fd, PROTO_STATUS_OK, mode, PROTO_RESULT_OK, NULL, username, uid, gid);
}

static uint8_t classify_pam_result(int rc)
{
  switch (rc)
  {
  case PAM_SUCCESS:
    return PROTO_RESULT_OK;
  case PAM_AUTH_ERR:
  case PAM_USER_UNKNOWN:
  case PAM_MAXTRIES:
  case PAM_CRED_ERR:
  case PAM_AUTHTOK_ERR:
    return PROTO_RESULT_AUTH_FAILED;
  case PAM_NEW_AUTHTOK_REQD:
    return PROTO_RESULT_PASSWORD_EXPIRED;
  case PAM_PERM_DENIED:
  case PAM_ACCT_EXPIRED:
  case PAM_CRED_INSUFFICIENT:
    return PROTO_RESULT_ACCESS_DENIED;
  default:
    return PROTO_RESULT_INTERNAL_ERROR;
  }
}

// Fixed FD layout for child process:
// 0 = stdin (bootstrap pipe)
// 1 = stdout (dup from stderr)
// 2 = stderr
// 3 = client connection (CLIENT_CONN_FD)
// 4 = startup-status fd (survives exec; bridge writes READY/ERROR and reads GO)
// 5 = bridge_fd (for execveat)
// Everything >= 6 is closed
#define CLIENT_CONN_FD 3
#define STARTUP_STATUS_FD 4
#define BRIDGE_FD      5

static pid_t spawn_bridge_process(
    const struct auth_user *auth_user,
    int want_privileged,
    int bridge_fd,
    int bootstrap_pipe_read,  // Pipe read end for bootstrap binary (will be stdin)
    int client_fd,            // Client connection FD (will be dup'd to FD 3 for Yamux)
    int startup_status_fd)    // Child end of startup-status socketpair
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
  int orig_startup_status = startup_status_fd;
  int orig_bridge = bridge_fd;
  int child_status_fd = startup_status_fd;

  // The launcher keeps SIGTERM blocked for its own shutdown handling; the
  // mask survives fork and exec, so restore default delivery here or the
  // bridge would never see a service-stop signal.
  sigset_t child_sigmask;
  if (sigemptyset(&child_sigmask) != 0 ||
      sigprocmask(SIG_SETMASK, &child_sigmask, NULL) != 0)
    child_die(child_status_fd, "reset signal mask");
  (void)signal(SIGTERM, SIG_DFL);

  // Preserve a client socket occupying stdin/stdout/stderr before those fixed
  // descriptors are rewritten. The parked copy only needs to survive the
  // descriptor rearrangement until dup2 installs it at CLIENT_CONN_FD.
  if (orig_client >= 0 && orig_client < CLIENT_CONN_FD)
  {
    int saved_client = fcntl(orig_client, F_DUPFD, BRIDGE_FD + 1);
    if (saved_client < 0)
      child_die(child_status_fd, "park client fd");

    if (orig_client == STDERR_FILENO)
    {
      // This descriptor becomes stderr and must remain open across exec.
      if (replace_stderr_with_devnull() != 0)
        child_die(child_status_fd, "replace client stderr");
    }

    orig_client = saved_client;
  }

  // First, move startup_status_fd and bridge_fd to high positions to avoid conflicts
  // (in case any of them is already at 0-5)
  int tmp_startup_status = -1, tmp_bridge = -1;

  if (orig_startup_status >= 0 && orig_startup_status <= BRIDGE_FD)
  {
    tmp_startup_status = fcntl(orig_startup_status, F_DUPFD_CLOEXEC, BRIDGE_FD + 1);
    if (tmp_startup_status < 0) child_die(child_status_fd, "dup startup-status fd");
    child_status_fd = tmp_startup_status;
    // Close original to avoid leaking an extra copy of the status channel
    close(orig_startup_status);
  }
  else
  {
    tmp_startup_status = orig_startup_status;
  }

  if (orig_bridge >= 0 && orig_bridge <= BRIDGE_FD)
  {
    tmp_bridge = fcntl(orig_bridge, F_DUPFD_CLOEXEC, BRIDGE_FD + 1);
    if (tmp_bridge < 0) child_die(child_status_fd, "dup bridge fd");
    // Close original to avoid leaking extra FD
    close(orig_bridge);
  }
  else
  {
    tmp_bridge = orig_bridge;
  }

  // Step 2: Set up stdin (FD 0) from bootstrap pipe. dup2 clears FD_CLOEXEC
  // on the copy, but when the descriptor already occupies its fixed slot the
  // original (created O_CLOEXEC) survives untouched, so clear it explicitly.
  if (orig_bootstrap >= 0)
  {
    if (dup2(orig_bootstrap, STDIN_FILENO) < 0) child_die(child_status_fd, "dup2 bootstrap to stdin");
    if (orig_bootstrap != STDIN_FILENO) close(orig_bootstrap);
    clear_cloexec_or_die(STDIN_FILENO, child_status_fd, "clear bootstrap CLOEXEC");
  }

  // Step 3: Set up stdout (FD 1) as dup of stderr
  if (dup2(STDERR_FILENO, STDOUT_FILENO) < 0) child_die(child_status_fd, "dup2 stderr to stdout");

  // Step 4: Set up client connection at FD 3. As with stdin, a client socket
  // already sitting at FD 3 keeps its original CLOEXEC flag because no dup2
  // runs; the bridge must inherit this descriptor, so clear it explicitly.
  if (orig_client >= 0 && orig_client != CLIENT_CONN_FD)
  {
    if (dup2(orig_client, CLIENT_CONN_FD) < 0) child_die(child_status_fd, "dup2 client connection");
    close(orig_client);
  }
  if (orig_client >= 0)
    clear_cloexec_or_die(CLIENT_CONN_FD, child_status_fd, "clear client CLOEXEC");

  // Step 5: Set up the startup-status fd at FD 4. It must survive exec so the
  // Go bridge can report READY/ERROR and wait for GO before starting Yamux.
  if (tmp_startup_status >= 0 && tmp_startup_status != STARTUP_STATUS_FD)
  {
    if (dup2(tmp_startup_status, STARTUP_STATUS_FD) < 0)
      child_die(child_status_fd, "dup2 startup-status fd");
    child_status_fd = STARTUP_STATUS_FD;
    close(tmp_startup_status);
  }
  clear_cloexec_or_die(STARTUP_STATUS_FD, child_status_fd,
                       "clear startup-status CLOEXEC");

  // Step 6: Set up bridge_fd at FD 5
  if (tmp_bridge >= 0 && tmp_bridge != BRIDGE_FD)
  {
    if (dup2(tmp_bridge, BRIDGE_FD) < 0) child_die(child_status_fd, "dup2 bridge fd");
    close(tmp_bridge);
  }

  // Now we have:
  // 0 = stdin (bootstrap)
  // 1 = stdout (-> stderr)
  // 2 = stderr
  // 3 = client connection
  // 4 = startup-status fd (no CLOEXEC - inherited by the bridge)
  // 5 = bridge_fd

  umask(077);

  // Preserve and validate environment variables before clearenv()
  const char *preserve_lang = getenv("LANG");
  const char *preserve_term = getenv("TERM");
  const char *preserve_journal_stream = getenv("JOURNAL_STREAM");

  // Save validated copies
  char safe_lang[128] = "C.UTF-8";  // Default to UTF-8 instead of plain C
  char safe_term[128] = "xterm-256color";
  char safe_journal_stream[128] = "";

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

  if (preserve_journal_stream && *preserve_journal_stream)
  {
    int valid = 1;
    for (const char *p = preserve_journal_stream; *p && valid; p++)
    {
      char c = *p;
      if (!((c >= '0' && c <= '9') || c == ':'))
        valid = 0;
    }
    if (valid && strlen(preserve_journal_stream) < sizeof(safe_journal_stream))
      safe_snprintf(safe_journal_stream, sizeof(safe_journal_stream), "%s", preserve_journal_stream);
  }

  clearenv();
  setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin", 1);
  setenv("LANG", safe_lang, 1);
  setenv("LC_ALL", safe_lang, 1);
  setenv("TERM", safe_term, 1);

  // Restore JOURNAL_STREAM if present - child processes may still emit stderr output
  if (safe_journal_stream[0] != '\0')
    setenv("JOURNAL_STREAM", safe_journal_stream, 1);

  if (want_privileged)
  {
    setenv("HOME", "/root", 1);
    setenv("USER", "root", 1);
    setenv("LOGNAME", "root", 1);
    if (setgroups(0, NULL) != 0)
      child_die(child_status_fd, "setgroups (privileged)");
    if (setresgid(0, 0, 0) != 0)
      child_die(child_status_fd, "setresgid (privileged)");
    if (setresuid(0, 0, 0) != 0)
      child_die(child_status_fd, "setresuid (privileged)");
  }
  else
  {
    drop_to_user(auth_user, child_status_fd);
    if (auth_user)
    {
      setenv("HOME", auth_user->dir, 1);
      setenv("USER", auth_user->name, 1);
      setenv("LOGNAME", auth_user->name, 1);
      char xdg[64];
      safe_snprintf(xdg, sizeof(xdg), "/run/user/%u", (unsigned)auth_user->uid);
      // pam_systemd creates this directory during pam_open_session; if it did
      // not run (or logind is absent), don't advertise a path that doesn't exist.
      struct stat xdg_st;
      if (stat(xdg, &xdg_st) == 0 && S_ISDIR(xdg_st.st_mode))
        setenv("XDG_RUNTIME_DIR", xdg, 1);
      if (chdir(auth_user->dir) != 0)
        child_die(child_status_fd, "chdir to home directory");
    }
  }

  // Application config is passed via binary bootstrap on stdin.

  // Close all file descriptors >= 6 (keeping 0-5 as set up above). Any
  // close_range failure falls back to an explicit close loop; proceeding with
  // unknown inherited descriptors is not safe for the privileged child.
#ifdef SYS_close_range
  if (syscall(SYS_close_range, BRIDGE_FD + 1, ~0U, 0) != 0)
#endif
  {
    struct rlimit rl;
    rlim_t max_fd;

    if (getrlimit(RLIMIT_NOFILE, &rl) != 0)
      child_die(child_status_fd, "getrlimit for fd cleanup");
    max_fd = rl.rlim_cur;
    if (max_fd == RLIM_INFINITY)
    {
      long open_max = sysconf(_SC_OPEN_MAX);
      if (open_max < 0)
        child_die(child_status_fd, "sysconf for fd cleanup");
      max_fd = (rlim_t)open_max;
    }
    if (max_fd > (rlim_t)INT_MAX)
      child_die(child_status_fd, "fd cleanup limit exceeds int range");

    for (int fd = BRIDGE_FD + 1; fd < (int)max_fd; fd++)
    {
      if (close(fd) != 0 && errno != EBADF)
        child_die(child_status_fd, "close inherited fd");
    }
  }

  // Application config passed via binary bootstrap on stdin - minimal argv
  const char *argv_child[] = {"linuxio-bridge", NULL};

  // Mark BRIDGE_FD as close-on-exec so it doesn't leak into the bridge process
  // (prevents "text file busy" on binary updates and avoids unnecessary FD leak)
  // CLOEXEC only closes after successful exec, so execveat() still works.
  set_cloexec_or_die(BRIDGE_FD, child_status_fd, "set bridge CLOEXEC");

  // Execute only the validated bridge fd. LinuxIO requires a kernel newer
  // than execveat; an unavailable or blocked syscall must fail closed rather
  // than downgrade to pathname execution.
#ifdef SYS_execveat
  (void)syscall(SYS_execveat, BRIDGE_FD, "", ARGV_UNCONST(argv_child), environ, AT_EMPTY_PATH);
#else
  errno = ENOSYS;
#endif

  // Exec failure writes the pre-exec status byte. On success, the inherited
  // startup-status fd remains open for the Go bridge's READY/ERROR and the
  // launcher's GO response.
  child_die(child_status_fd, "exec bridge");
}

// Handle a single client request
static int handle_client(int input_fd, int output_fd)
{
  struct auth_timing timing = {0};
  int64_t request_deadline_ns = 0;
  const int64_t request_budget_ns =
      (int64_t)AUTH_REQUEST_TIMEOUT_SEC * INT64_C(1000000000);
  if (monotonic_now_ns(&timing.request_started_ns) != 0)
  {
    journal_errorf("failed to establish authentication deadline: %m");
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                        "failed to establish authentication deadline");
    return 1;
  }
  if (timing.request_started_ns > INT64_MAX - request_budget_ns)
  {
    errno = EOVERFLOW;
    journal_errorf("failed to establish authentication deadline: %m");
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                        "failed to establish authentication deadline");
    return 1;
  }
  request_deadline_ns = timing.request_started_ns + request_budget_ns;

  // Read binary request header
  uint8_t header[PROTO_AUTH_REQ_HEADER_SIZE];
  if (read_all_until(input_fd, header, PROTO_AUTH_REQ_HEADER_SIZE, request_deadline_ns) != 0)
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "failed to read request header");
    return 1;
  }

  // Validate magic
  if (header[0] != PROTO_MAGIC_0 || header[1] != PROTO_MAGIC_1 ||
      header[2] != PROTO_MAGIC_2 || header[3] != PROTO_VERSION)
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "invalid request magic");
    return 1;
  }

  // Parse header fields
  uint8_t req_flags = header[4];
  int verbose_flag = (req_flags & PROTO_REQ_FLAG_VERBOSE) != 0;

  // Read variable-length fields
  char user[PROTO_MAX_USERNAME] = "";
  char password[PROTO_MAX_PASSWORD] = "";
  char session_id[PROTO_MAX_SESSION_ID] = "";
  char remote_host[PROTO_MAX_REMOTE_HOST] = "";

  int result = 1;
  int pam_end_status = PAM_SUCCESS;
  int credentials_established = 0;
  int session_open = 0;
  int login_started = 0;
  int child_reaped = 0;
  int child_signal = 0;
  int bridge_fd = -1;
  int bootstrap_pipe[2] = {-1, -1};
  int startup_status_channel[2] = {-1, -1};
  pid_t child = -1;
  pam_handle_t *pamh = NULL;

  if (read_lenstr_until(input_fd, user, sizeof(user), request_deadline_ns) != 0 ||
      read_lenstr_until(input_fd, password, sizeof(password), request_deadline_ns) != 0 ||
      read_lenstr_until(input_fd, session_id, sizeof(session_id), request_deadline_ns) != 0 ||
      read_lenstr_until(input_fd, remote_host, sizeof(remote_host), request_deadline_ns) != 0)
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "failed to read request fields");
    goto out;
  }

  // Validate required fields and the web-login credential policy.
  if (!user[0] || !session_id[0])
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "missing required fields");
    goto out;
  }
  if (!password[0])
  {
    send_error_response(output_fd, PROTO_RESULT_AUTH_FAILED, "authentication failed");
    goto out;
  }
  if (!valid_username(user))
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "invalid username format");
    goto out;
  }

  // Validate session_id (defense against path injection)
  if (!valid_session_id(session_id))
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "invalid session_id format");
    goto out;
  }

  if (!valid_remote_host(remote_host))
  {
    send_error_response(output_fd, PROTO_RESULT_BAD_REQUEST, "invalid remote_host format");
    goto out;
  }

  // PAM authentication
  (void)monotonic_now_ns(&timing.pam_started_ns);
  struct pam_appdata appdata = {
      .username = user,
      .password = password};
  struct pam_conv conv = {
      .conv = pam_conv_func,
      .appdata_ptr = &appdata};
  int rc = pam_start("linuxio", user, &conv, &pamh);
  int auth_rc;
  if (rc != PAM_SUCCESS)
  {
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR, pam_strerror(NULL, rc));
    pamh = NULL;
    goto out;
  }

  rc = pam_set_item(pamh, PAM_RHOST, remote_host);
  if (rc == PAM_SUCCESS)
    rc = pam_set_item(pamh, PAM_TTY, LINUXIO_WEB_TTY);
  if (rc != PAM_SUCCESS)
  {
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR, pam_strerror(pamh, rc));
    pam_end_status = rc;
    goto out;
  }

  const int pam_flags = PAM_DISALLOW_NULL_AUTHTOK;
  auth_rc = pam_authenticate(pamh, pam_flags);
  rc = auth_rc;
  if (rc == PAM_SUCCESS)
    rc = pam_acct_mgmt(pamh, pam_flags);

  // Handle password expiration
  if (rc == PAM_NEW_AUTHTOK_REQD)
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", user},
    };
    journal_info_fieldsf(fields, 1, "password expired");
    send_error_response(output_fd, PROTO_RESULT_PASSWORD_EXPIRED,
                        "Password has expired. Please change it via SSH or console.");
    pam_end_status = rc;
    goto out;
  }

  if (rc != PAM_SUCCESS)
  {
    const char *err = pam_strerror(pamh, rc);
    if (auth_rc != PAM_SUCCESS)
      btmp_log(user, remote_host);
    send_error_response(output_fd, classify_pam_result(rc), err);
    pam_end_status = rc;
    goto out;
  }

  const void *pam_user_item = NULL;
  rc = pam_get_item(pamh, PAM_USER, &pam_user_item);
  if (rc != PAM_SUCCESS || !pam_user_item || !((const char *)pam_user_item)[0])
  {
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                        "failed to resolve authenticated user");
    pam_end_status = rc == PAM_SUCCESS ? PAM_USER_UNKNOWN : rc;
    goto out;
  }

  const char *pam_user = (const char *)pam_user_item;
  if (!valid_username(pam_user))
  {
    send_error_response(output_fd, PROTO_RESULT_ACCESS_DENIED,
                        "invalid authenticated user");
    pam_end_status = PAM_USER_UNKNOWN;
    goto out;
  }
  memmove(user, pam_user, strlen(pam_user) + 1);

  rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);
  if (rc != PAM_SUCCESS)
  {
    send_error_response(output_fd, classify_pam_result(rc), pam_strerror(pamh, rc));
    pam_end_status = rc;
    goto out;
  }
  credentials_established = 1;

  // Resolve the identity selected by PAM, not the raw wire username.
  const struct passwd *pw = getpwnam(user);
  if (!pw)
  {
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR, "user lookup failed");
    goto out;
  }

  // Copy libc-owned passwd data before PAM session hooks can overwrite NSS static storage.
  struct auth_user auth_user;
  if (copy_auth_user(pw, &auth_user) != 0)
  {
    send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR, "invalid passwd entry");
    goto out;
  }
  if (!valid_username(auth_user.name))
  {
    send_error_response(output_fd, PROTO_RESULT_ACCESS_DENIED,
                        "invalid resolved user");
    goto out;
  }

  if (auth_user.uid == 0)
  {
    send_error_response(output_fd, PROTO_RESULT_ACCESS_DENIED,
                        "root login is not allowed");
    goto out;
  }

  (void)monotonic_now_ns(&timing.pam_completed_ns);

  {
    int64_t remaining_ns;
    if (deadline_remaining_ns(request_deadline_ns, &remaining_ns) <= 0)
    {
      journal_errorf("authentication deadline expired during PAM");
      send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                          "authentication request timed out");
      goto out;
    }
  }

  {
    char uid_buf[32];
    (void)safe_snprintf(uid_buf, sizeof(uid_buf), "%u", (unsigned)auth_user.uid);
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
        {"LINUXIO_UID", uid_buf},
    };
    journal_info_fieldsf(fields, 2, "pam auth success");
  }

  // PAM is finished with the plaintext password; the policy query does not
  // receive it, and no later launcher stage needs it.
  secure_bzero(password, sizeof(password));

  // Privileged mode iff sudoers lets this user run the bridge as root
  (void)monotonic_now_ns(&timing.sudo_started_ns);
  int want_privileged =
      user_can_run_bridge_as_root(auth_user.name, request_deadline_ns) ? 1 : 0;
  (void)monotonic_now_ns(&timing.sudo_completed_ns);

  uint8_t mode = want_privileged ? PROTO_MODE_PRIVILEGED : PROTO_MODE_UNPRIVILEGED;
  const char *mode_name = mode == PROTO_MODE_PRIVILEGED ? "privileged" : "unprivileged";

  {
    int64_t remaining_ns;
    if (deadline_remaining_ns(request_deadline_ns, &remaining_ns) <= 0)
    {
      journal_errorf("authentication deadline expired during sudo policy check");
      send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                          "authentication request timed out");
      goto out;
    }
  }

  // Validate bridge binary and keep fd open (prevents TOCTOU)
  (void)monotonic_now_ns(&timing.session_setup_started_ns);
  if (open_and_validate_bridge(LINUXIO_BRIDGE_OWNER, &bridge_fd) != 0)
  {
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge validation failed");
    goto out;
  }
  // Keep bridge_fd open - we'll exec it directly to prevent TOCTOU

  // Create pipe for bootstrap data (secrets never touch filesystem).
  // O_CLOEXEC on both ends: the child's dup2 to stdin clears it on FD 0,
  // while the child's inherited copy of the write end closes at exec -
  // otherwise the bridge would hold its own stdin open and never see EOF.
  if (pipe2(bootstrap_pipe, O_CLOEXEC) != 0)
  {
    journal_errorf("failed to create bootstrap pipe: %m");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "failed to prepare bootstrap");
    goto out;
  }

  rc = pam_open_session(pamh, 0);
  if (rc != PAM_SUCCESS)
  {
    const char *err = pam_strerror(pamh, rc);
    send_error_response(output_fd, classify_pam_result(rc), err);
    pam_end_status = rc;
    goto out;
  }
  session_open = 1;

  // Kerberos/AFS-style modules expect a credential refresh after the session
  // hooks run; sshd and cockpit-session both do this, and both treat failure
  // as fatal to the login.
  rc = pam_setcred(pamh, PAM_REINITIALIZE_CRED);
  if (rc != PAM_SUCCESS)
  {
    send_error_response(output_fd, classify_pam_result(rc), pam_strerror(pamh, rc));
    pam_end_status = rc;
    goto out;
  }
  (void)monotonic_now_ns(&timing.session_opened_ns);
  timing.bridge_start_started_ns = timing.session_opened_ns;

  {
    int64_t remaining_ns;
    if (deadline_remaining_ns(request_deadline_ns, &remaining_ns) <= 0)
    {
      journal_errorf("authentication deadline expired during session setup");
      send_error_response(output_fd, PROTO_RESULT_INTERNAL_ERROR,
                          "authentication request timed out");
      goto out;
    }
  }

  // Create a bidirectional startup-status channel with CLOEXEC initially. The
  // child clears CLOEXEC only on its fixed FD 4 copy so it can report
  // READY/ERROR and wait for the launcher's GO without leaking aliases.
  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0,
                 startup_status_channel) != 0)
  {
    journal_errorf("failed to create startup-status channel: %m");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR,
                        "failed to prepare startup status");
    goto out;
  }

  child = spawn_bridge_process(
      &auth_user,
      want_privileged,
      bridge_fd,
      bootstrap_pipe[0],    // Pass pipe read end to child (will be stdin)
      input_fd,             // Pass client connection FD (will be dup'd to FD 3 for Yamux)
      startup_status_channel[1]); // Child end of startup-status channel

  // Parent: close bootstrap read end and the child's status-channel endpoint.
  close(bootstrap_pipe[0]);
  bootstrap_pipe[0] = -1;
  close(startup_status_channel[1]);
  startup_status_channel[1] = -1;

  if (child < 0)
  {
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "failed to spawn bridge");
    goto out;
  }

  // Parent: write binary bootstrap to pipe, then close to signal EOF
  int rc_bootstrap = write_bootstrap_binary(
      bootstrap_pipe[1],
      session_id,
      auth_user.name,
      auth_user.uid,
      auth_user.gid,
      verbose_flag,
      want_privileged);
  close(bootstrap_pipe[1]);
  bootstrap_pipe[1] = -1;

  // Close bridge_fd - child has it via fork
  close(bridge_fd);
  bridge_fd = -1;

  // Wait for the bridge's startup-status byte. READY means initialization is
  // complete and the bridge is blocked waiting for GO; it cannot write Yamux
  // bytes to the client connection before the OK response below.
  int ready_timeout_ms =
      env_get_int("LINUXIO_BRIDGE_READY_TIMEOUT", BRIDGE_READY_TIMEOUT_SEC,
                  1, AUTH_REQUEST_TIMEOUT_SEC) * 1000;
  char bridge_err[PROTO_MAX_ERROR];
  uint8_t bad_status = 0;
  enum bridge_startup_result startup = wait_for_bridge_startup(
      startup_status_channel[0], ready_timeout_ms, request_deadline_ns,
      bridge_err, sizeof(bridge_err), &bad_status);
  int startup_errno = errno;

  // A pre-exec child failure closes the bootstrap reader and reports its
  // status on the startup channel. Either event can reach the parent first,
  // so consume the status before classifying an EPIPE from the bootstrap
  // write. Preserve the existing bootstrap error for every other outcome.
  if (rc_bootstrap != 0 && startup != BRIDGE_STARTUP_EXEC_FAILED)
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    journal_error_fieldsf(fields, 1, "failed to write bootstrap to pipe");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bootstrap communication failed");
    child_signal = SIGTERM;
    goto out;
  }

  switch (startup)
  {
  case BRIDGE_STARTUP_READY:
    break;

  case BRIDGE_STARTUP_EXEC_FAILED:
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    journal_error_fieldsf(fields, 1, "bridge exec failed");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge exec failed");
    // The pre-exec child exits on its own; the shared epilogue still reaps it.
    goto out;
  }

  case BRIDGE_STARTUP_REPORTED_ERROR:
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
        {"LINUXIO_BRIDGE_ERROR", bridge_err},
    };
    journal_error_fieldsf(fields, 2, "bridge reported startup failure");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR,
                        bridge_err[0] != '\0' ? bridge_err : "bridge startup failed");
    child_signal = SIGTERM;
    goto out;
  }

  case BRIDGE_STARTUP_EOF:
  {
    // The status fd closed with no byte: the bridge died before becoming
    // ready, or closed the fd without acking. Distinguish for the journal;
    // fail closed either way.
    int wstatus = 0;
    pid_t reaped = waitpid_nointr(child, &wstatus, WNOHANG);
    if (reaped == child)
    {
      char status_buf[16];
      child_reaped = 1;
      (void)safe_snprintf(status_buf, sizeof(status_buf), "%d", child_status_code(wstatus));
      const struct journal_field fields[] = {
          {"LINUXIO_USER", auth_user.name},
          {"LINUXIO_STATUS", status_buf},
      };
      journal_error_fieldsf(fields, 2, "bridge died during startup (status %s)", status_buf);
    }
    else
    {
      const struct journal_field fields[] = {
          {"LINUXIO_USER", auth_user.name},
      };
      journal_error_fieldsf(fields, 1, "bridge closed startup status without ready ack");
      if (reaped < 0 && errno == ECHILD)
        child_reaped = 1;
      else
        child_signal = SIGKILL;
    }
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge failed to start");
    goto out;
  }

  case BRIDGE_STARTUP_TIMEOUT:
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    journal_error_fieldsf(fields, 1, "bridge not ready after %d ms", ready_timeout_ms);
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge start timeout");
    child_signal = SIGKILL;
    goto out;
  }

  case BRIDGE_STARTUP_PROTOCOL_ERROR:
  {
    char status_buf[16];
    (void)safe_snprintf(status_buf, sizeof(status_buf), "%u", (unsigned)bad_status);
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
        {"LINUXIO_STATUS", status_buf},
    };
    journal_error_fieldsf(fields, 2, "unknown bridge startup status byte");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge startup protocol error");
    child_signal = SIGKILL;
    goto out;
  }

  case BRIDGE_STARTUP_WAIT_ERROR:
  default:
  {
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    errno = startup_errno;
    journal_error_fieldsf(fields, 1, "bridge startup wait failed: %m");
    send_error_response(output_fd, PROTO_RESULT_BRIDGE_ERROR, "bridge startup wait failed");
    child_signal = SIGKILL;
    goto out;
  }
  }

  (void)monotonic_now_ns(&timing.bridge_ready_ns);
  timing.accounting_started_ns = timing.bridge_ready_ns;

  // The bridge is initialized but cannot start Yamux until this process sends
  // the complete OK response and then releases it over the status channel.
  record_login_start(&auth_user, remote_host);
  (void)monotonic_now_ns(&timing.accounting_completed_ns);
  login_started = 1;
  if (send_ok_response(output_fd, mode, auth_user.name, auth_user.uid, auth_user.gid) != 0)
  {
    int response_errno = errno;
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    errno = response_errno;
    journal_error_fieldsf(fields, 1, "failed to send authentication response: %m");
    child_signal = SIGTERM;
    goto out;
  }

  {
    if (release_bridge_startup(startup_status_channel[0]) != 0)
    {
      int release_errno = errno;
      const struct journal_field fields[] = {
          {"LINUXIO_USER", auth_user.name},
      };
      errno = release_errno;
      journal_error_fieldsf(fields, 1, "failed to release bridge startup: %m");
      child_signal = SIGTERM;
      goto out;
    }
    close(startup_status_channel[0]);
    startup_status_channel[0] = -1;
  }
  (void)monotonic_now_ns(&timing.request_completed_ns);
  log_auth_timing(&timing, auth_user.uid, mode_name);

  // Don't close input_fd/output_fd - the bridge (child) has the connection via FD 3
  // The parent's copy will be closed when we exit, which is fine

  {
    char uid_buf[32];
    char gid_buf[32];
    (void)safe_snprintf(uid_buf, sizeof(uid_buf), "%u", (unsigned)auth_user.uid);
    (void)safe_snprintf(gid_buf, sizeof(gid_buf), "%u", (unsigned)auth_user.gid);
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
        {"LINUXIO_UID", uid_buf},
        {"LINUXIO_GID", gid_buf},
        {"LINUXIO_MODE", mode_name},
        {"LINUXIO_PRIVILEGED", mode == PROTO_MODE_PRIVILEGED ? "true" : "false"},
    };
    journal_info_fieldsf(fields, 5, "bridge spawned");
  }

  int status = 0;
  int session_wait = wait_for_session_child(child, &status);
  if (session_wait == 0)
  {
    // Service stop: terminate the bridge through the shared epilogue so end
    // accounting, PAM session close, credential deletion, and pam_end all
    // still run before the worker exits.
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    journal_info_fieldsf(fields, 1, "service stop requested; shutting down session");
    child_signal = SIGTERM;
    result = 0;
    goto out;
  }
  if (session_wait < 0)
  {
    int wait_errno = errno;
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
    };
    errno = wait_errno;
    journal_error_fieldsf(fields, 1, "bridge final wait failed: %m");
    if (wait_errno == ECHILD)
      child_reaped = 1;
    else
      child_signal = SIGKILL;
    goto out;
  }
  child_reaped = 1;

  int exitcode = 1;
  if (WIFEXITED(status))
  {
    exitcode = WEXITSTATUS(status);
    if (exitcode != 0)
    {
      char exit_buf[32];
      char pid_buf[32];
      (void)safe_snprintf(exit_buf, sizeof(exit_buf), "%d", exitcode);
      (void)safe_snprintf(pid_buf, sizeof(pid_buf), "%ld", (long)child);
      const struct journal_field fields[] = {
          {"LINUXIO_USER", auth_user.name},
          {"LINUXIO_STATUS", exit_buf},
          {"LINUXIO_CHILD_PID", pid_buf},
      };
      journal_error_fieldsf(fields, 3, "bridge pid %ld exited with status %d", (long)child, exitcode);
    }
  }
  else if (WIFSIGNALED(status))
  {
    int sig = WTERMSIG(status);
    exitcode = 128 + WTERMSIG(status);
    char exit_buf[32];
    char signal_buf[32];
    char pid_buf[32];
    (void)safe_snprintf(exit_buf, sizeof(exit_buf), "%d", exitcode);
    (void)safe_snprintf(signal_buf, sizeof(signal_buf), "%d", sig);
    (void)safe_snprintf(pid_buf, sizeof(pid_buf), "%ld", (long)child);
    const struct journal_field fields[] = {
        {"LINUXIO_USER", auth_user.name},
        {"LINUXIO_STATUS", exit_buf},
        {"LINUXIO_SIGNAL", signal_buf},
        {"LINUXIO_CHILD_PID", pid_buf},
    };
    journal_error_fieldsf(fields, 4, "bridge pid %ld killed by signal %d", (long)child, sig);
  }
  result = exitcode;

out:
  if (bootstrap_pipe[0] >= 0)
    close(bootstrap_pipe[0]);
  if (bootstrap_pipe[1] >= 0)
    close(bootstrap_pipe[1]);
  if (startup_status_channel[0] >= 0)
    close(startup_status_channel[0]);
  if (startup_status_channel[1] >= 0)
    close(startup_status_channel[1]);
  if (bridge_fd >= 0)
    close(bridge_fd);

  if (child > 0 && !child_reaped)
    terminate_and_reap_child(child, child_signal);

  if (login_started)
    record_login_end();
  if (session_open)
    (void)pam_close_session(pamh, 0);
  if (credentials_established)
    (void)pam_setcred(pamh, PAM_DELETE_CRED);
  if (pamh)
    (void)pam_end(pamh, pam_end_status);

  secure_bzero(password, sizeof(password));
  return result;
}

// -------- main ----------
int main(int argc, char *argv[])
{
  // Handle --version before any other checks
  if (argc == 2 && (strcmp(argv[1], "--version") == 0 || strcmp(argv[1], "version") == 0))
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

  // Don't die on writes to closed pipes/sockets; write() reports EPIPE and
  // write_all() handles it. systemd's IgnoreSIGPIPE default covers the
  // socket-activated path, but don't depend on it for manual/sudo runs.
  (void)signal(SIGPIPE, SIG_IGN);

  // Service stop must not kill the worker mid-request or inside the
  // session-long bridge wait; see wait_for_session_child for delivery.
  if (install_shutdown_handling() != 0)
  {
    log_stderrf("failed to install service-stop handling: %m");
    return 1;
  }

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
