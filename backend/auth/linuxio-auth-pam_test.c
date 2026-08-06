// Tier-1 hermetic host-integration suite for the LinuxIO auth launcher
// (make test-auth-pam).
//
// The binary must be started by the Makefile with cwd set to a throwaway
// test directory and with the cwrap preloads configured:
//   LD_PRELOAD=libpam_wrapper.so:libnss_wrapper.so:libuid_wrapper.so
//   PAM_WRAPPER=1  PAM_WRAPPER_SERVICE_DIR=<dir with rendered "linuxio">
//   NSS_WRAPPER_PASSWD/GROUP=<files this test writes at startup>
//   UID_WRAPPER=1  UID_WRAPPER_ROOT=1
//   LINUXIO_TEST_REAL_UID/GID=<the invoking user's real ids>
//
// Under that harness the suite runs the real handle_client() end to end:
// a real PAM stack (pam_matrix + the probe module), real fork/exec of a
// stub bridge through the fixed fd layout, the privilege-drop sequence
// under uid_wrapper's emulated-root semantics, and accounting records
// written to tmpfile paths via the compile-time seams below.
static unsigned int g_real_uid;
static unsigned int g_real_gid;

#define BRIDGE_DIR "bridge-bin"
#define LINUXIO_BRIDGE_OWNER ((uid_t)g_real_uid)
#define LINUXIO_PATH_UTMP "acct-utmp"
#define LINUXIO_PATH_WTMP "acct-wtmp"
#define LINUXIO_PATH_BTMP "acct-btmp"
#define LINUXIO_PATH_LASTLOG "acct-lastlog"
// Shorten the TERM-to-KILL grace so the stubborn-bridge service-stop
// scenario completes in about a second instead of the production five.
#define CHILD_TERM_GRACE_SEC 1

#define main linuxio_auth_entrypoint
#include "linuxio-auth.c"
#undef main

#define CANON_USER "liotest"
#define WIRE_ALIAS "liowire"
#define ROOT_USER "lioroot"
#define UNKNOWN_USER "liounknown"
#define TEST_PASSWORD "hermetic-secret"
#define TEST_SESSION_ID "sess-tier1"
#define TEST_REMOTE_HOST "203.0.113.7"
#define STUB_MARKER "YAMUX-OK"

static char g_workdir[PATH_MAX];
static char g_home[PATH_MAX];

struct test_case
{
  const char *name;
  int (*run)(void);
};

#define CHECK(condition)                                                            \
  do                                                                                \
  {                                                                                 \
    if (!(condition))                                                               \
    {                                                                               \
      fprintf(stderr, "%s:%d: check failed: %s\n", __func__, __LINE__, #condition); \
      return 1;                                                                     \
    }                                                                               \
  } while (0)

// -------- small file helpers --------

static int t_write_file(const char *path, const void *data, size_t len, mode_t mode)
{
  int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, mode);

  if (fd < 0)
    return -1;
  if (write_all(fd, data, len) != 0)
  {
    close(fd);
    return -1;
  }
  if (fchmod(fd, mode) != 0)
  {
    close(fd);
    return -1;
  }
  close(fd);
  return 0;
}

static int t_read_file(const char *path, char *buf, size_t bufsz, size_t *out_len)
{
  int fd = open(path, O_RDONLY | O_CLOEXEC);
  size_t total = 0;

  if (fd < 0)
    return -1;
  while (total < bufsz - 1)
  {
    ssize_t n = read(fd, buf + total, bufsz - 1 - total);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      close(fd);
      return -1;
    }
    if (n == 0)
      break;
    total += (size_t)n;
  }
  close(fd);
  buf[total] = '\0';
  if (out_len)
    *out_len = total;
  return 0;
}

static int t_copy_file(const char *src, const char *dst, mode_t mode)
{
  static char buf[1 << 20];
  size_t len = 0;

  if (t_read_file(src, buf, sizeof(buf), &len) != 0)
    return -1;
  return t_write_file(dst, buf, len, mode);
}

static void t_remove(const char *path)
{
  (void)unlink(path);
}

static int home_path(char *buf, size_t bufsz, const char *name)
{
  return safe_snprintf(buf, bufsz, "%s/%s", g_home, name) > 0 ? 0 : -1;
}

// Every scenario starts from the same workspace state: empty accounting
// databases, an empty PAM trace, the requested probe control file, a fresh
// executable stub bridge, and the requested stub behavior word.
static int reset_case(const char *pam_control, const char *bridge_control)
{
  char path[PATH_MAX];

  if (t_write_file("acct-utmp", "", 0, 0644) != 0 ||
      t_write_file("acct-wtmp", "", 0, 0644) != 0 ||
      t_write_file("acct-lastlog", "", 0, 0644) != 0)
    return -1;
  t_remove("acct-btmp");
  t_remove("pam-trace");
  if (t_write_file("pam-control", pam_control, strlen(pam_control), 0644) != 0)
    return -1;

  static const char *const dumps[] = {
      "dump_fds", "dump_ids", "dump_env", "dump_cwd", "dump_bootstrap",
      "linger-started"};
  for (size_t i = 0; i < sizeof(dumps) / sizeof(dumps[0]); i++)
  {
    if (home_path(path, sizeof(path), dumps[i]) != 0)
      return -1;
    t_remove(path);
  }

  if (home_path(path, sizeof(path), "bridge_control") != 0 ||
      t_write_file(path, bridge_control, strlen(bridge_control), 0644) != 0)
    return -1;

  if (t_copy_file("bridge-stub", BRIDGE_PATH, 0755) != 0)
    return -1;

  unsetenv("LINUXIO_BRIDGE_READY_TIMEOUT");
  return 0;
}

// -------- request/response plumbing --------

static size_t append_u16be(uint8_t *buf, size_t pos, uint16_t v)
{
  buf[pos] = (uint8_t)(v >> 8);
  buf[pos + 1] = (uint8_t)v;
  return pos + 2;
}

static size_t append_lenstr(uint8_t *buf, size_t pos, const char *s)
{
  size_t n = strlen(s);

  pos = append_u16be(buf, pos, (uint16_t)n);
  memcpy(buf + pos, s, n);
  return pos + n;
}

static int drain_fd(int fd, uint8_t *buf, size_t bufsz, size_t *out_len)
{
  size_t total = 0;

  while (total < bufsz)
  {
    ssize_t n = read(fd, buf + total, bufsz - total);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      return -1;
    }
    if (n == 0)
    {
      *out_len = total;
      return 0;
    }
    total += (size_t)n;
  }
  return -1;
}

struct login_result
{
  int rc;
  uint8_t out[1024];
  size_t out_len;
};

// Runs one complete request through the real handle_client. When
// close_peer_early is set the client half is closed before handle_client
// runs, so the buffered request stays readable but every response write
// fails - the OK-write failure path.
static int run_login(const char *user, const char *password,
                     int close_peer_early, struct login_result *res)
{
  uint8_t req[4096];
  size_t pos = 0;
  int sv[2];

  memset(res, 0, sizeof(*res));

  req[pos++] = PROTO_MAGIC_0;
  req[pos++] = PROTO_MAGIC_1;
  req[pos++] = PROTO_MAGIC_2;
  req[pos++] = PROTO_VERSION;
  req[pos++] = 0; // flags
  req[pos++] = 0;
  req[pos++] = 0;
  req[pos++] = 0;
  pos = append_lenstr(req, pos, user);
  pos = append_lenstr(req, pos, password);
  pos = append_lenstr(req, pos, TEST_SESSION_ID);
  pos = append_lenstr(req, pos, TEST_REMOTE_HOST);

  if (socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, sv) != 0)
    return -1;
  if (write_all(sv[1], req, pos) != 0)
  {
    close(sv[0]);
    close(sv[1]);
    return -1;
  }
  if (close_peer_early)
  {
    close(sv[1]);
    sv[1] = -1;
  }

  res->rc = handle_client(sv[0], sv[0]);
  close(sv[0]);

  if (sv[1] >= 0)
  {
    int drained = drain_fd(sv[1], res->out, sizeof(res->out), &res->out_len);
    close(sv[1]);
    if (drained != 0)
      return -1;
  }
  return 0;
}

// Exact golden error frame:
// [magic:4][PROTO_STATUS_ERROR][mode=0][want_result][reserved=0][len:2][msg]
// A NULL want_msg skips the message-content comparison but still requires a
// complete, self-consistent frame.
static int check_error_response(const struct login_result *res,
                                uint8_t want_result, const char *want_msg)
{
  CHECK(res->out_len >= PROTO_AUTH_RESP_HEADER_SIZE + 2);
  CHECK(res->out[0] == PROTO_MAGIC_0);
  CHECK(res->out[1] == PROTO_MAGIC_1);
  CHECK(res->out[2] == PROTO_MAGIC_2);
  CHECK(res->out[3] == PROTO_VERSION);
  CHECK(res->out[4] == PROTO_STATUS_ERROR);
  CHECK(res->out[5] == 0);
  CHECK(res->out[6] == want_result);
  CHECK(res->out[7] == 0);

  uint16_t msg_len = read_u16_be(res->out + 8);
  CHECK(res->out_len == (size_t)PROTO_AUTH_RESP_HEADER_SIZE + 2 + msg_len);
  if (want_msg)
  {
    CHECK(msg_len == strlen(want_msg));
    CHECK(memcmp(res->out + 10, want_msg, msg_len) == 0);
  }
  return 0;
}

// Exact golden OK frame for an unprivileged login, optionally followed by
// the stub's post-GO marker bytes proving response-before-Yamux ordering.
static int check_ok_response(const struct login_result *res,
                             const char *want_user, const char *want_marker)
{
  size_t user_len = strlen(want_user);
  size_t frame_len = (size_t)PROTO_AUTH_RESP_HEADER_SIZE + 8 + 2 + user_len;

  CHECK(res->out_len >= frame_len);
  CHECK(res->out[0] == PROTO_MAGIC_0);
  CHECK(res->out[1] == PROTO_MAGIC_1);
  CHECK(res->out[2] == PROTO_MAGIC_2);
  CHECK(res->out[3] == PROTO_VERSION);
  CHECK(res->out[4] == PROTO_STATUS_OK);
  CHECK(res->out[5] == PROTO_MODE_UNPRIVILEGED);
  CHECK(res->out[6] == PROTO_RESULT_OK);
  CHECK(res->out[7] == 0);

  uint32_t uid = ((uint32_t)res->out[8] << 24) | ((uint32_t)res->out[9] << 16) |
                 ((uint32_t)res->out[10] << 8) | (uint32_t)res->out[11];
  uint32_t gid = ((uint32_t)res->out[12] << 24) | ((uint32_t)res->out[13] << 16) |
                 ((uint32_t)res->out[14] << 8) | (uint32_t)res->out[15];
  CHECK(uid == g_real_uid);
  CHECK(gid == g_real_gid);
  CHECK(read_u16_be(res->out + 16) == user_len);
  CHECK(memcmp(res->out + 18, want_user, user_len) == 0);

  if (want_marker)
  {
    CHECK(res->out_len == frame_len + strlen(want_marker));
    CHECK(memcmp(res->out + frame_len, want_marker, strlen(want_marker)) == 0);
  }
  else
  {
    CHECK(res->out_len == frame_len);
  }
  return 0;
}

// -------- PAM trace helpers --------

static int read_trace(char *buf, size_t bufsz)
{
  size_t len = 0;

  buf[0] = '\0';
  if (t_read_file("pam-trace", buf, bufsz, &len) != 0)
    buf[0] = '\0';
  return 0;
}

static size_t count_lines_with_prefix(const char *buf, const char *prefix)
{
  size_t prefix_len = strlen(prefix);
  size_t count = 0;

  for (const char *p = buf; *p;)
  {
    if (strncmp(p, prefix, prefix_len) == 0)
      count++;
    const char *nl = strchr(p, '\n');
    if (!nl)
      break;
    p = nl + 1;
  }
  return count;
}

static int check_trace_equals(const char *expected)
{
  char buf[4096];

  read_trace(buf, sizeof(buf));
  if (strcmp(buf, expected) != 0)
  {
    fprintf(stderr, "pam trace mismatch.\n--- expected ---\n%s--- actual ---\n%s---\n",
            expected, buf);
    return 1;
  }
  return 0;
}

static int check_close_session_count(size_t want)
{
  char buf[4096];

  read_trace(buf, sizeof(buf));
  if (count_lines_with_prefix(buf, "close_session ") != want)
  {
    fprintf(stderr, "close_session count != %zu.\n--- trace ---\n%s---\n", want, buf);
    return 1;
  }
  return 0;
}

// -------- accounting helpers --------

static int read_utmp_records(const char *path, struct utmp *recs, size_t max,
                             size_t *count)
{
  static char buf[64 * sizeof(struct utmp)];
  size_t len = 0;

  *count = 0;
  if (t_read_file(path, buf, sizeof(buf), &len) != 0)
    return 0; // absent file == zero records
  if (len % sizeof(struct utmp) != 0)
    return -1;
  *count = len / sizeof(struct utmp);
  if (*count > max)
    return -1;
  memcpy(recs, buf, len);
  return 0;
}

static int fixed_field_equals(const char *field, size_t field_size, const char *want)
{
  size_t want_len = strlen(want);

  if (want_len > field_size)
    return 0;
  if (strncmp(field, want, want_len) != 0)
    return 0;
  for (size_t i = want_len; i < field_size && i < want_len + 1; i++)
  {
    if (field[i] != '\0')
      return 0;
  }
  return 1;
}

static int check_accounting_empty(void)
{
  struct utmp recs[4];
  size_t count = 0;
  struct stat st;

  CHECK(read_utmp_records("acct-utmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  CHECK(stat("acct-btmp", &st) != 0);
  return 0;
}

// -------- dump helpers --------

static int read_home_dump(const char *name, char *buf, size_t bufsz, size_t *out_len)
{
  char path[PATH_MAX];

  if (home_path(path, sizeof(path), name) != 0)
    return -1;
  return t_read_file(path, buf, bufsz, out_len);
}

static int env_dump_has_line(const char *dump, const char *line)
{
  size_t line_len = strlen(line);

  for (const char *p = dump; *p;)
  {
    const char *nl = strchr(p, '\n');
    size_t seg = nl ? (size_t)(nl - p) : strlen(p);

    if (seg == line_len && strncmp(p, line, line_len) == 0)
      return 1;
    if (!nl)
      break;
    p = nl + 1;
  }
  return 0;
}

// -------- scenarios --------

static int test_happy_path_login(void)
{
  struct login_result res;
  char expected_trace[1024];
  char dump[8192];
  size_t dump_len = 0;

  CHECK(reset_case("", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 0);
  CHECK(check_ok_response(&res, CANON_USER, STUB_MARKER) == 0);

  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "open_session user=%s\n"
                      "setcred reinitialize user=%s\n"
                      "close_session user=%s\n"
                      "setcred delete user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER, CANON_USER, CANON_USER, CANON_USER,
                      CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  // Working directory: the launcher chdir()ed the child to the fake home.
  CHECK(read_home_dump("dump_cwd", dump, sizeof(dump), &dump_len) == 0);
  CHECK(strcmp(dump, g_home) == 0);

  // Post-exec ids. The privilege-drop sequence itself ran under
  // uid_wrapper's emulated-root semantics (any failure, including the
  // setuid(0) fail-closed verification firing, would have aborted the
  // spawn), so the observable post-exec ids equal the invoking user.
  CHECK(read_home_dump("dump_ids", dump, sizeof(dump), &dump_len) == 0);
  {
    unsigned ruid, euid, suid, rgid, egid, sgid;
    CHECK(sscanf(dump, "ruid=%u euid=%u suid=%u rgid=%u egid=%u sgid=%u",
                 &ruid, &euid, &suid, &rgid, &egid, &sgid) == 6);
    CHECK(ruid == g_real_uid && euid == g_real_uid && suid == g_real_uid);
    CHECK(rgid == g_real_gid && egid == g_real_gid && sgid == g_real_gid);
  }

  // Fixed descriptor layout: exactly fds 0-4 survive to the bridge
  // (bridge_fd 5 is CLOEXEC, everything >= 6 is closed pre-exec).
  CHECK(read_home_dump("dump_fds", dump, sizeof(dump), &dump_len) == 0);
  {
    int seen[5] = {0};
    size_t lines = 0;

    for (const char *p = dump; *p;)
    {
      int fd = -1;
      char target[128] = "";

      CHECK(sscanf(p, "fd=%d target=%127s", &fd, target) == 2);
      CHECK(fd >= 0 && fd <= 4);
      CHECK(!seen[fd]);
      seen[fd] = 1;
      lines++;
      if (fd == 0)
        CHECK(strncmp(target, "pipe:", 5) == 0);
      if (fd == 3 || fd == 4)
        CHECK(strncmp(target, "socket:", 7) == 0);

      const char *nl = strchr(p, '\n');
      if (!nl)
        break;
      p = nl + 1;
    }
    CHECK(lines == 5);
    for (int fd = 0; fd <= 4; fd++)
      CHECK(seen[fd]);
  }

  // Synthesized environment: exact allowlist values, and no test-harness
  // wrapper configuration may leak through exec into the bridge.
  CHECK(read_home_dump("dump_env", dump, sizeof(dump), &dump_len) == 0);
  {
    char line[PATH_MAX + 32];

    CHECK(env_dump_has_line(dump, "USER=" CANON_USER));
    CHECK(env_dump_has_line(dump, "LOGNAME=" CANON_USER));
    CHECK(safe_snprintf(line, sizeof(line), "HOME=%s", g_home) > 0);
    CHECK(env_dump_has_line(dump, line));
    CHECK(env_dump_has_line(
        dump, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"));
    CHECK(env_dump_has_line(dump, "LANG=C.UTF-8"));
    CHECK(env_dump_has_line(dump, "LC_ALL=C.UTF-8"));
    CHECK(env_dump_has_line(dump, "TERM=xterm"));
    CHECK(strstr(dump, "LD_PRELOAD") == NULL);
    CHECK(strstr(dump, "PAM_WRAPPER") == NULL);
    CHECK(strstr(dump, "NSS_WRAPPER") == NULL);
    CHECK(strstr(dump, "UID_WRAPPER") == NULL);
    CHECK(strstr(dump, "LINUXIO_PAM_PROBE") == NULL);

    char xdg[64];
    struct stat st;
    CHECK(safe_snprintf(xdg, sizeof(xdg), "/run/user/%u", g_real_uid) > 0);
    if (stat(xdg, &st) == 0 && S_ISDIR(st.st_mode))
    {
      CHECK(safe_snprintf(line, sizeof(line), "XDG_RUNTIME_DIR=%s", xdg) > 0);
      CHECK(env_dump_has_line(dump, line));
    }
    else
    {
      CHECK(strstr(dump, "XDG_RUNTIME_DIR=") == NULL);
    }
  }

  // Bootstrap bytes, exactly as the launcher framed them.
  {
    uint8_t expected[512];
    size_t pos = 0;

    expected[pos++] = PROTO_MAGIC_0;
    expected[pos++] = PROTO_MAGIC_1;
    expected[pos++] = PROTO_MAGIC_2;
    expected[pos++] = PROTO_VERSION;
    write_u32_be(expected + pos, g_real_uid);
    pos += 4;
    write_u32_be(expected + pos, g_real_gid);
    pos += 4;
    expected[pos++] = PROTO_FLAG_READY_ACK;
    pos = append_lenstr(expected, pos, TEST_SESSION_ID);
    pos = append_lenstr(expected, pos, CANON_USER);

    CHECK(read_home_dump("dump_bootstrap", dump, sizeof(dump), &dump_len) == 0);
    CHECK(dump_len == pos);
    CHECK(memcmp(dump, expected, pos) == 0);
  }

  // Accounting records against the tmpfile databases.
  {
    struct utmp recs[4];
    size_t count = 0;
    char ut_id[4];

    encode_ut_id(ut_id, getpid());

    CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
    CHECK(count == 2);
    CHECK(recs[0].ut_type == USER_PROCESS);
    CHECK(recs[0].ut_pid == getpid());
    CHECK(memcmp(recs[0].ut_id, ut_id, 4) == 0);
    CHECK(fixed_field_equals(recs[0].ut_user, sizeof(recs[0].ut_user), CANON_USER));
    CHECK(fixed_field_equals(recs[0].ut_line, sizeof(recs[0].ut_line), LINUXIO_WEB_TTY));
    CHECK(fixed_field_equals(recs[0].ut_host, sizeof(recs[0].ut_host), TEST_REMOTE_HOST));
    CHECK(recs[0].ut_tv.tv_sec > 0);
    CHECK(recs[1].ut_type == DEAD_PROCESS);
    CHECK(recs[1].ut_pid == getpid());
    CHECK(memcmp(recs[1].ut_id, ut_id, 4) == 0);
    CHECK(fixed_field_equals(recs[1].ut_line, sizeof(recs[1].ut_line), LINUXIO_WEB_TTY));
    CHECK(recs[1].ut_user[0] == '\0');

    // The live utmp slot was written at login start and updated in place at
    // login end, so exactly one record remains and it is DEAD_PROCESS.
    CHECK(read_utmp_records("acct-utmp", recs, 4, &count) == 0);
    CHECK(count == 1);
    CHECK(recs[0].ut_type == DEAD_PROCESS);
    CHECK(recs[0].ut_pid == getpid());
    CHECK(memcmp(recs[0].ut_id, ut_id, 4) == 0);

    struct lastlog entry;
    int fd = open("acct-lastlog", O_RDONLY | O_CLOEXEC);
    CHECK(fd >= 0);
    ssize_t n = pread(fd, &entry, sizeof(entry),
                      (off_t)g_real_uid * (off_t)sizeof(entry));
    CHECK(close(fd) == 0);
    CHECK(n == (ssize_t)sizeof(entry));
    CHECK(entry.ll_time > 0);
    CHECK(fixed_field_equals(entry.ll_line, sizeof(entry.ll_line), LINUXIO_WEB_TTY));
    CHECK(fixed_field_equals(entry.ll_host, sizeof(entry.ll_host), TEST_REMOTE_HOST));

    struct stat st;
    CHECK(stat("acct-btmp", &st) != 0);
  }

  return 0;
}

static int test_wrong_password_hits_btmp(void)
{
  struct login_result res;
  struct utmp recs[4];
  size_t count = 0;
  char expected_trace[256];

  CHECK(reset_case("", "ok") == 0);
  CHECK(run_login(CANON_USER, "not-the-password", 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_AUTH_FAILED, NULL) == 0);

  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  CHECK(read_utmp_records("acct-btmp", recs, 4, &count) == 0);
  CHECK(count == 1);
  CHECK(recs[0].ut_type == LOGIN_PROCESS);
  CHECK(fixed_field_equals(recs[0].ut_user, sizeof(recs[0].ut_user), CANON_USER));
  CHECK(fixed_field_equals(recs[0].ut_host, sizeof(recs[0].ut_host), TEST_REMOTE_HOST));
  CHECK(fixed_field_equals(recs[0].ut_line, sizeof(recs[0].ut_line), LINUXIO_WEB_TTY));

  CHECK(read_utmp_records("acct-utmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  return 0;
}

static int test_unknown_user_hits_btmp(void)
{
  struct login_result res;
  struct utmp recs[4];
  size_t count = 0;

  CHECK(reset_case("", "ok") == 0);
  CHECK(run_login(UNKNOWN_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_AUTH_FAILED, NULL) == 0);

  CHECK(read_utmp_records("acct-btmp", recs, 4, &count) == 0);
  CHECK(count == 1);
  CHECK(fixed_field_equals(recs[0].ut_user, sizeof(recs[0].ut_user), UNKNOWN_USER));
  return 0;
}

static int test_pam_user_canonicalization(void)
{
  struct login_result res;
  char expected_trace[1024];
  char dump[8192];
  size_t dump_len = 0;
  struct utmp recs[4];
  size_t count = 0;

  CHECK(reset_case("remap_from=" WIRE_ALIAS "\nremap_to=" CANON_USER "\n",
                   "ok") == 0);
  CHECK(run_login(WIRE_ALIAS, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 0);

  // The response, and everything derived after PAM, must carry the
  // canonical identity - the wire alias may appear nowhere downstream.
  CHECK(check_ok_response(&res, CANON_USER, STUB_MARKER) == 0);

  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "remap from=%s to=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "open_session user=%s\n"
                      "setcred reinitialize user=%s\n"
                      "close_session user=%s\n"
                      "setcred delete user=%s\n",
                      WIRE_ALIAS, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, WIRE_ALIAS,
                      CANON_USER, CANON_USER, CANON_USER, CANON_USER, CANON_USER,
                      CANON_USER, CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  CHECK(read_home_dump("dump_bootstrap", dump, sizeof(dump), &dump_len) == 0);
  CHECK(memmem(dump, dump_len, CANON_USER, strlen(CANON_USER)) != NULL);
  CHECK(memmem(dump, dump_len, WIRE_ALIAS, strlen(WIRE_ALIAS)) == NULL);

  CHECK(read_home_dump("dump_env", dump, sizeof(dump), &dump_len) == 0);
  CHECK(env_dump_has_line(dump, "USER=" CANON_USER));

  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 2);
  CHECK(fixed_field_equals(recs[0].ut_user, sizeof(recs[0].ut_user), CANON_USER));
  return 0;
}

static int test_account_expired_denied(void)
{
  struct login_result res;
  struct utmp recs[4];
  size_t count = 0;
  struct stat st;

  CHECK(reset_case("fail_acct=acct_expired\n", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_ACCESS_DENIED, NULL) == 0);

  // Authentication itself succeeded, so no btmp record may be written.
  CHECK(stat("acct-btmp", &st) != 0);
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  CHECK(check_close_session_count(0) == 0);
  return 0;
}

static int test_expired_password(void)
{
  struct login_result res;

  CHECK(reset_case("fail_acct=new_authtok_reqd\n", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_PASSWORD_EXPIRED,
                             "Password has expired. Please change it via SSH or console.") == 0);
  CHECK(check_close_session_count(0) == 0);
  return 0;
}

static int test_establish_cred_failure(void)
{
  struct login_result res;
  char expected_trace[512];

  CHECK(reset_case("fail_setcred_establish=cred_err\n", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_AUTH_FAILED, NULL) == 0);

  // Credentials were never established: no session, no credential delete.
  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);
  return 0;
}

static int test_open_session_failure(void)
{
  struct login_result res;
  char expected_trace[512];
  struct utmp recs[4];
  size_t count = 0;

  CHECK(reset_case("fail_open_session=session_err\n", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_INTERNAL_ERROR, NULL) == 0);

  // The session never opened, so close_session must not run; established
  // credentials are still deleted through the shared epilogue.
  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "open_session user=%s\n"
                      "setcred delete user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER, CANON_USER, CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  return 0;
}

static int test_reinit_cred_failure_closes_session_once(void)
{
  struct login_result res;
  char expected_trace[512];
  struct utmp recs[4];
  size_t count = 0;

  CHECK(reset_case("fail_setcred_reinit=cred_err\n", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_AUTH_FAILED, NULL) == 0);

  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "open_session user=%s\n"
                      "setcred reinitialize user=%s\n"
                      "close_session user=%s\n"
                      "setcred delete user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER, CANON_USER, CANON_USER, CANON_USER,
                      CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  // Accounting starts only after bridge READY; this failure precedes the
  // bridge entirely.
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 0);
  return 0;
}

static int test_root_login_rejected(void)
{
  struct login_result res;
  char expected_trace[512];
  struct stat st;

  CHECK(reset_case("", "ok") == 0);
  CHECK(run_login(ROOT_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_ACCESS_DENIED,
                             "root login is not allowed") == 0);

  // Rejected after credential establishment, before any session.
  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "setcred delete user=%s\n",
                      ROOT_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, ROOT_USER,
                      ROOT_USER, ROOT_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);
  CHECK(stat("acct-btmp", &st) != 0);
  return 0;
}

// Shared driver for the pre-spawn bridge-validation matrix: PAM fully
// succeeds, validation fails before pam_open_session, so no session may
// open or close.
static int run_bridge_validation_case(void)
{
  struct login_result res;
  char expected_trace[512];

  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, PROTO_RESULT_BRIDGE_ERROR,
                             "bridge validation failed") == 0);

  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "setcred delete user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER, CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);
  return check_accounting_empty();
}

static int test_bridge_missing(void)
{
  CHECK(reset_case("", "ok") == 0);
  t_remove(BRIDGE_PATH);
  return run_bridge_validation_case();
}

static int test_bridge_not_executable(void)
{
  CHECK(reset_case("", "ok") == 0);
  CHECK(chmod(BRIDGE_PATH, 0644) == 0);
  return run_bridge_validation_case();
}

static int test_bridge_setuid_rejected(void)
{
  CHECK(reset_case("", "ok") == 0);
  CHECK(chmod(BRIDGE_PATH, 04755) == 0);
  return run_bridge_validation_case();
}

static int test_bridge_group_writable_rejected(void)
{
  CHECK(reset_case("", "ok") == 0);
  CHECK(chmod(BRIDGE_PATH, 0775) == 0);
  return run_bridge_validation_case();
}

// Shared driver for post-open bridge startup failures: the PAM session was
// opened, so the epilogue must close it exactly once, and accounting (which
// only starts after READY) must remain empty.
static int run_post_open_bridge_case(uint8_t want_result, const char *want_msg)
{
  struct login_result res;

  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(res.rc == 1);
  CHECK(check_error_response(&res, want_result, want_msg) == 0);
  CHECK(check_close_session_count(1) == 0);
  return check_accounting_empty();
}

static int test_bridge_exec_enoexec(void)
{
  CHECK(reset_case("", "ok") == 0);
  // Valid by metadata policy, unrunnable by the kernel: exec fails after
  // validation, so the pre-exec child reports PROTO_STARTUP_EXEC_FAILED.
  CHECK(t_write_file(BRIDGE_PATH, "GARBAGE\n", 8, 0755) == 0);
  return run_post_open_bridge_case(PROTO_RESULT_BRIDGE_ERROR, "bridge exec failed");
}

static int test_bridge_exits_before_ready(void)
{
  CHECK(reset_case("", "eof") == 0);
  return run_post_open_bridge_case(PROTO_RESULT_BRIDGE_ERROR, "bridge failed to start");
}

static int test_bridge_garbage_status_byte(void)
{
  CHECK(reset_case("", "garbage") == 0);
  return run_post_open_bridge_case(PROTO_RESULT_BRIDGE_ERROR,
                                   "bridge startup protocol error");
}

static int test_bridge_reported_error(void)
{
  CHECK(reset_case("", "error") == 0);
  return run_post_open_bridge_case(PROTO_RESULT_BRIDGE_ERROR, "stub bridge exploded");
}

static int test_bridge_ready_timeout(void)
{
  struct timespec started, finished;
  int64_t elapsed_ns;
  int rc;

  CHECK(reset_case("", "sleep") == 0);
  CHECK(setenv("LINUXIO_BRIDGE_READY_TIMEOUT", "1", 1) == 0);
  CHECK(clock_gettime(CLOCK_MONOTONIC, &started) == 0);
  rc = run_post_open_bridge_case(PROTO_RESULT_BRIDGE_ERROR, "bridge start timeout");
  CHECK(clock_gettime(CLOCK_MONOTONIC, &finished) == 0);
  unsetenv("LINUXIO_BRIDGE_READY_TIMEOUT");
  CHECK(rc == 0);

  elapsed_ns = (int64_t)(finished.tv_sec - started.tv_sec) * INT64_C(1000000000) +
               (int64_t)(finished.tv_nsec - started.tv_nsec);
  CHECK(elapsed_ns >= INT64_C(900000000));
  CHECK(elapsed_ns < INT64_C(10000000000));
  return 0;
}

// -------- service-stop scenarios --------

static int restore_shutdown_handling(void)
{
  struct sigaction dfl;
  sigset_t term_set;

  memset(&dfl, 0, sizeof(dfl));
  dfl.sa_handler = SIG_DFL;
  if (sigemptyset(&dfl.sa_mask) != 0 ||
      sigaction(SIGTERM, &dfl, NULL) != 0 ||
      sigemptyset(&term_set) != 0 ||
      sigaddset(&term_set, SIGTERM) != 0 ||
      sigprocmask(SIG_UNBLOCK, &term_set, NULL) != 0)
    return -1;
  g_shutdown_requested = 0;
  return 0;
}

// Forked helper that waits for the stub bridge's linger marker and then
// delivers the service-stop SIGTERM to the test process. Because the stub
// writes the marker file only after its post-GO client bytes, the signal
// always arrives with a fully-established live session.
static pid_t spawn_service_stop_killer(void)
{
  char path[PATH_MAX];
  pid_t pid;

  if (home_path(path, sizeof(path), "linger-started") != 0)
    return -1;
  pid = fork();
  if (pid != 0)
    return pid;

  for (int i = 0; i < 500; i++)
  {
    struct stat st;
    struct timespec delay = {.tv_sec = 0, .tv_nsec = 10000000L};

    if (stat(path, &st) == 0)
      break;
    (void)nanosleep(&delay, NULL);
  }
  (void)kill(getppid(), SIGTERM);
  _exit(0);
}

// Service stop mid-session: SIGTERM arrives while the launcher supervises a
// live bridge. The launcher must terminate the bridge (with bounded SIGKILL
// escalation if it ignores SIGTERM) and still run the full epilogue: end
// accounting, exactly one session close, and credential deletion.
static int run_service_stop_case(const char *bridge_control,
                                 int64_t min_elapsed_ns)
{
  struct login_result res;
  struct utmp recs[4];
  size_t count = 0;
  struct timespec started, finished;
  int64_t elapsed_ns;
  char expected_trace[1024];
  int status = 0;
  pid_t killer;

  CHECK(reset_case("", bridge_control) == 0);
  CHECK(install_shutdown_handling() == 0);
  killer = spawn_service_stop_killer();
  CHECK(killer > 0);

  CHECK(clock_gettime(CLOCK_MONOTONIC, &started) == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 0, &res) == 0);
  CHECK(clock_gettime(CLOCK_MONOTONIC, &finished) == 0);
  CHECK(waitpid_nointr(killer, &status, 0) == killer);
  CHECK(restore_shutdown_handling() == 0);

  // Orderly stop: success exit, and the complete OK response plus the stub's
  // post-GO marker were already on the wire before the stop.
  CHECK(res.rc == 0);
  CHECK(check_ok_response(&res, CANON_USER, STUB_MARKER) == 0);

  elapsed_ns = (int64_t)(finished.tv_sec - started.tv_sec) * INT64_C(1000000000) +
               (int64_t)(finished.tv_nsec - started.tv_nsec);
  CHECK(elapsed_ns >= min_elapsed_ns);
  CHECK(elapsed_ns < INT64_C(10000000000));

  // The full PAM lifecycle ran despite the stop, closing the session once.
  CHECK(safe_snprintf(expected_trace, sizeof(expected_trace),
                      "authenticate user=%s rhost=%s tty=%s\n"
                      "acct_mgmt user=%s\n"
                      "setcred establish user=%s\n"
                      "open_session user=%s\n"
                      "setcred reinitialize user=%s\n"
                      "close_session user=%s\n"
                      "setcred delete user=%s\n",
                      CANON_USER, TEST_REMOTE_HOST, LINUXIO_WEB_TTY, CANON_USER,
                      CANON_USER, CANON_USER, CANON_USER, CANON_USER,
                      CANON_USER) > 0);
  CHECK(check_trace_equals(expected_trace) == 0);

  // Accounting was started at READY and must be completed by the epilogue:
  // start and end records, and the live utmp slot marked dead.
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 2);
  CHECK(recs[0].ut_type == USER_PROCESS);
  CHECK(recs[1].ut_type == DEAD_PROCESS);
  CHECK(read_utmp_records("acct-utmp", recs, 4, &count) == 0);
  CHECK(count == 1);
  CHECK(recs[0].ut_type == DEAD_PROCESS);
  return 0;
}

static int test_service_stop_cleanup(void)
{
  return run_service_stop_case("linger", 0);
}

static int test_service_stop_stubborn_bridge(void)
{
  // The stubborn stub ignores SIGTERM, so cleanup must take at least the
  // (test-shortened) grace period before SIGKILL escalation - and no longer.
  return run_service_stop_case("stubborn", INT64_C(900000000));
}

static int test_ok_write_failure_after_ready(void)
{
  struct login_result res;
  struct utmp recs[4];
  size_t count = 0;

  CHECK(reset_case("", "ok") == 0);
  CHECK(run_login(CANON_USER, TEST_PASSWORD, 1, &res) == 0);
  CHECK(res.rc == 1);

  // Accounting had already started when the OK write failed, so the
  // epilogue must complete it: a start and an end record, and a dead utmp
  // slot - no phantom live session survives a failed response.
  CHECK(read_utmp_records("acct-wtmp", recs, 4, &count) == 0);
  CHECK(count == 2);
  CHECK(recs[0].ut_type == USER_PROCESS);
  CHECK(recs[1].ut_type == DEAD_PROCESS);
  CHECK(read_utmp_records("acct-utmp", recs, 4, &count) == 0);
  CHECK(count == 1);
  CHECK(recs[0].ut_type == DEAD_PROCESS);
  CHECK(check_close_session_count(1) == 0);
  return 0;
}

// -------- harness --------

// The negative-path scenarios legitimately make the launcher, the stub
// bridge, sudo, and pam_wrapper write errors to stderr. That noise is
// expected, so each scenario runs with stderr captured to a file that is
// echoed back only when the scenario fails.
#define STDERR_CAPTURE_FILE "stderr-capture"

static int g_terminal_stderr = -1;

static int begin_stderr_capture(void)
{
  int fd;

  if (g_terminal_stderr < 0)
  {
    g_terminal_stderr = dup(STDERR_FILENO);
    if (g_terminal_stderr < 0)
      return -1;
  }
  fd = open(STDERR_CAPTURE_FILE, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0644);
  if (fd < 0)
    return -1;
  // dup2 clears CLOEXEC on fd 2, so spawned children inherit the capture.
  if (dup2(fd, STDERR_FILENO) < 0)
  {
    close(fd);
    return -1;
  }
  close(fd);
  return 0;
}

static void end_stderr_capture(void)
{
  (void)fflush(stderr);
  if (g_terminal_stderr >= 0)
    (void)dup2(g_terminal_stderr, STDERR_FILENO);
}

static void dump_captured_stderr(void)
{
  static char buf[65536];
  size_t len = 0;

  if (t_read_file(STDERR_CAPTURE_FILE, buf, sizeof(buf), &len) != 0 || len == 0)
    return;
  fprintf(stderr, "--- captured stderr ---\n%s---\n", buf);
}

static int setup_workspace(void)
{
  char content[4096];
  char path[PATH_MAX];
  const char *real_uid = getenv("LINUXIO_TEST_REAL_UID");
  const char *real_gid = getenv("LINUXIO_TEST_REAL_GID");

  if (!real_uid || !real_gid)
  {
    fprintf(stderr, "LINUXIO_TEST_REAL_UID/GID must be set by the test harness\n");
    return -1;
  }
  g_real_uid = (unsigned int)strtoul(real_uid, NULL, 10);
  g_real_gid = (unsigned int)strtoul(real_gid, NULL, 10);
  if (g_real_uid == 0)
  {
    fprintf(stderr, "this suite must run as a non-root user\n");
    return -1;
  }

  if (!getcwd(g_workdir, sizeof(g_workdir)))
    return -1;
  if (safe_snprintf(g_home, sizeof(g_home), "%s/home", g_workdir) <= 0)
    return -1;
  if (mkdir(BRIDGE_DIR, 0755) != 0 && errno != EEXIST)
    return -1;
  if (chmod(BRIDGE_DIR, 0755) != 0)
    return -1;
  if (mkdir(g_home, 0755) != 0 && errno != EEXIST)
    return -1;

  if (safe_snprintf(content, sizeof(content),
                    "%s:x:%u:%u:LinuxIO test user:%s:/bin/sh\n"
                    "%s:x:0:0:LinuxIO root test user:%s:/bin/sh\n",
                    CANON_USER, g_real_uid, g_real_gid, g_home,
                    ROOT_USER, g_home) <= 0 ||
      t_write_file("passwd", content, strlen(content), 0644) != 0)
    return -1;

  if (safe_snprintf(content, sizeof(content),
                    "%s:x:%u:\n%s:x:0:\n",
                    CANON_USER, g_real_gid, ROOT_USER) <= 0 ||
      t_write_file("group", content, strlen(content), 0644) != 0)
    return -1;

  if (safe_snprintf(content, sizeof(content),
                    "%s:%s:linuxio\n%s:%s:linuxio\n",
                    CANON_USER, TEST_PASSWORD, ROOT_USER, TEST_PASSWORD) <= 0 ||
      t_write_file("passdb", content, strlen(content), 0644) != 0)
    return -1;

  if (safe_snprintf(path, sizeof(path), "%s/pam-trace", g_workdir) <= 0 ||
      setenv("LINUXIO_PAM_PROBE_TRACE", path, 1) != 0)
    return -1;
  if (safe_snprintf(path, sizeof(path), "%s/pam-control", g_workdir) <= 0 ||
      setenv("LINUXIO_PAM_PROBE_CONTROL", path, 1) != 0)
    return -1;
  if (setenv("LINUXIO_SUDO_TIMEOUT", "2", 1) != 0)
    return -1;

  return 0;
}

int main(void)
{
  const struct test_case tests[] = {
      {"happy-path login", test_happy_path_login},
      {"wrong password hits btmp", test_wrong_password_hits_btmp},
      {"unknown user hits btmp", test_unknown_user_hits_btmp},
      {"PAM_USER canonicalization", test_pam_user_canonicalization},
      {"account expired denied", test_account_expired_denied},
      {"expired password", test_expired_password},
      {"establish-cred failure", test_establish_cred_failure},
      {"open-session failure", test_open_session_failure},
      {"reinit-cred failure closes session once", test_reinit_cred_failure_closes_session_once},
      {"root login rejected", test_root_login_rejected},
      {"bridge missing", test_bridge_missing},
      {"bridge not executable", test_bridge_not_executable},
      {"bridge setuid rejected", test_bridge_setuid_rejected},
      {"bridge group-writable rejected", test_bridge_group_writable_rejected},
      {"bridge exec ENOEXEC", test_bridge_exec_enoexec},
      {"bridge exits before READY", test_bridge_exits_before_ready},
      {"bridge garbage status byte", test_bridge_garbage_status_byte},
      {"bridge reported startup error", test_bridge_reported_error},
      {"bridge READY timeout", test_bridge_ready_timeout},
      {"OK-write failure after READY", test_ok_write_failure_after_ready},
      {"service-stop cleanup", test_service_stop_cleanup},
      {"service-stop with stubborn bridge", test_service_stop_stubborn_bridge},
  };
  int failures = 0;

  (void)signal(SIGPIPE, SIG_IGN);

  if (setup_workspace() != 0)
  {
    fprintf(stderr, "failed to prepare hermetic PAM workspace\n");
    return 1;
  }

  for (size_t i = 0; i < sizeof(tests) / sizeof(tests[0]); i++)
  {
    int captured = begin_stderr_capture() == 0;
    int result = tests[i].run();
    if (captured)
      end_stderr_capture();
    printf("   %s %s\n", result == 0 ? "✓" : "✗", tests[i].name);
    (void)fflush(stdout);
    if (result != 0 && captured)
      dump_captured_stderr();
    failures += result != 0;
  }

  if (failures != 0)
  {
    fprintf(stderr, "%d PAM integration test group(s) failed\n", failures);
    return 1;
  }

  return 0;
}
