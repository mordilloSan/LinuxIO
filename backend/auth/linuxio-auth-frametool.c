// linuxio-auth-frametool.c - test-only CLI exercising the REAL wire-protocol
// helpers from linuxio-auth.c (parsing, validation, and encoding), driven
// from Go via backend/common/ipc/auth/crosslang_test.go. Never installed.
#define main linuxio_auth_entrypoint
#include "linuxio-auth.c"
#undef main

// -------- argv helpers --------

// Parses argv text as an unsigned long, rejecting empty strings, leading
// whitespace/sign, trailing garbage, and out-of-range values.
static int parse_ulong_arg(const char *s, unsigned long *out)
{
  if (!s || !*s || !out)
    return -1;
  if (!((*s >= '0' && *s <= '9')))
    return -1;

  char *end = NULL;
  errno = 0;
  unsigned long v = strtoul(s, &end, 10);
  if (errno != 0 || !end || *end != '\0')
    return -1;

  *out = v;
  return 0;
}

static int parse_bool_arg(const char *s, int *out)
{
  unsigned long v;
  if (parse_ulong_arg(s, &v) != 0 || v > 1)
    return -1;
  *out = (int)v;
  return 0;
}

// -------- subcommands --------

static int cmd_parse_request(void)
{
  char user[PROTO_MAX_USERNAME] = "";
  char password[PROTO_MAX_PASSWORD] = "";
  char session_id[PROTO_MAX_SESSION_ID] = "";
  char remote_host[PROTO_MAX_REMOTE_HOST] = "";
  uint8_t header[PROTO_AUTH_REQ_HEADER_SIZE];
  int64_t deadline_ns;

  if (monotonic_now_ns(&deadline_ns) != 0)
  {
    printf("error=clock-failure\n");
    return 1;
  }
  deadline_ns += INT64_C(5000000000);

  if (read_all_until(STDIN_FILENO, header, sizeof(header), deadline_ns) != 0)
  {
    printf("error=header-read\n");
    return 1;
  }

  if (header[0] != PROTO_MAGIC_0 || header[1] != PROTO_MAGIC_1 ||
      header[2] != PROTO_MAGIC_2 || header[3] != PROTO_VERSION)
  {
    printf("error=bad-magic\n");
    return 1;
  }

  int verbose_flag = (header[4] & PROTO_REQ_FLAG_VERBOSE) != 0;

  if (read_lenstr_until(STDIN_FILENO, user, sizeof(user), deadline_ns) != 0 ||
      read_lenstr_until(STDIN_FILENO, password, sizeof(password), deadline_ns) != 0 ||
      read_lenstr_until(STDIN_FILENO, session_id, sizeof(session_id), deadline_ns) != 0 ||
      read_lenstr_until(STDIN_FILENO, remote_host, sizeof(remote_host), deadline_ns) != 0)
  {
    printf("error=fields-read\n");
    return 1;
  }

  // Same pre-PAM validation order as handle_client.
  if (!user[0] || !session_id[0])
  {
    printf("error=missing-required-fields\n");
    return 1;
  }
  if (!password[0])
  {
    printf("error=empty-password\n");
    return 1;
  }
  if (!valid_username(user))
  {
    printf("error=invalid-username\n");
    return 1;
  }
  if (!valid_session_id(session_id))
  {
    printf("error=invalid-session-id\n");
    return 1;
  }
  if (!valid_remote_host(remote_host))
  {
    printf("error=invalid-remote-host\n");
    return 1;
  }

  printf("user=%s\n", user);
  printf("password_len=%zu\n", strlen(password));
  printf("session_id=%s\n", session_id);
  printf("remote_host=%s\n", remote_host);
  printf("verbose=%d\n", verbose_flag);
  return 0;
}

static int cmd_emit_ok_response(int argc, char **argv)
{
  unsigned long uid, gid, mode;

  if (argc != 6)
  {
    log_stderrf("emit-ok-response requires: user uid gid mode");
    return 1;
  }
  if (parse_ulong_arg(argv[3], &uid) != 0 ||
      parse_ulong_arg(argv[4], &gid) != 0 ||
      parse_ulong_arg(argv[5], &mode) != 0 || mode > 1)
  {
    log_stderrf("emit-ok-response: bad numeric argument");
    return 1;
  }

  if (send_ok_response(STDOUT_FILENO, (uint8_t)mode, argv[2],
                       (uid_t)uid, (gid_t)gid) != 0)
  {
    log_stderrf("send_ok_response failed: %s", strerror(errno));
    return 1;
  }
  return 0;
}

static int cmd_emit_error_response(int argc, char **argv, int allow_message)
{
  unsigned long result_code;
  const char *message;

  if ((allow_message && argc != 4) || (!allow_message && argc != 3))
  {
    log_stderrf("emit-error-response%s requires: result_code%s",
               allow_message ? "" : "-null", allow_message ? " message" : "");
    return 1;
  }
  message = allow_message ? argv[3] : NULL;
  if (parse_ulong_arg(argv[2], &result_code) != 0 || result_code > 0xff)
  {
    log_stderrf("emit-error-response: bad result_code");
    return 1;
  }

  send_error_response(STDOUT_FILENO, (uint8_t)result_code, message);
  return 0;
}

static int cmd_emit_bootstrap(int argc, char **argv)
{
  unsigned long uid, gid;
  int verbose, privileged;

  if (argc != 8)
  {
    log_stderrf("emit-bootstrap requires: session_id user uid gid verbose privileged");
    return 1;
  }
  if (parse_ulong_arg(argv[4], &uid) != 0 ||
      parse_ulong_arg(argv[5], &gid) != 0 ||
      parse_bool_arg(argv[6], &verbose) != 0 ||
      parse_bool_arg(argv[7], &privileged) != 0)
  {
    log_stderrf("emit-bootstrap: bad numeric argument");
    return 1;
  }

  if (write_bootstrap_binary(STDOUT_FILENO, argv[2], argv[3],
                             (uid_t)uid, (gid_t)gid, verbose, privileged) != 0)
  {
    log_stderrf("write_bootstrap_binary failed: %s", strerror(errno));
    return 1;
  }
  return 0;
}

int main(int argc, char **argv)
{
  if (argc < 2)
  {
    log_stderrf("usage: %s <command> [args...]", argv[0]);
    return 1;
  }

  if (strcmp(argv[1], "parse-request") == 0)
    return cmd_parse_request();
  if (strcmp(argv[1], "emit-ok-response") == 0)
    return cmd_emit_ok_response(argc, argv);
  if (strcmp(argv[1], "emit-error-response") == 0)
    return cmd_emit_error_response(argc, argv, 1);
  if (strcmp(argv[1], "emit-error-response-null") == 0)
    return cmd_emit_error_response(argc, argv, 0);
  if (strcmp(argv[1], "emit-bootstrap") == 0)
    return cmd_emit_bootstrap(argc, argv);

  log_stderrf("unknown command: %s", argv[1]);
  return 1;
}
