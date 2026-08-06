#define main linuxio_auth_entrypoint
#include "linuxio-auth.c"
#undef main

struct test_case
{
  const char *name;
  int (*run)(void);
};

#define CHECK(condition)                                                                    \
  do                                                                                        \
  {                                                                                         \
    if (!(condition))                                                                       \
    {                                                                                       \
      fprintf(stderr, "%s:%d: check failed: %s\n", __func__, __LINE__, #condition);         \
      return 1;                                                                             \
    }                                                                                       \
  } while (0)

static int write_pipe_bytes(const uint8_t *data, size_t len, int *read_fd)
{
  int pipefd[2];

  if (!data || !read_fd || pipe2(pipefd, O_CLOEXEC) != 0)
    return -1;
  if (write_all(pipefd[1], data, len) != 0)
  {
    close(pipefd[0]);
    close(pipefd[1]);
    return -1;
  }
  close(pipefd[1]);
  *read_fd = pipefd[0];
  return 0;
}

static int read_pipe_to_end(int fd, uint8_t *buf, size_t bufsz, size_t *out_len)
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

static int test_identity_validation(void)
{
  char longest_username[PROTO_MAX_USERNAME];
  char oversized_username[PROTO_MAX_USERNAME + 1];
  const char unicode_username[] = "m\xc3\xADguel";
  const char malformed_utf8[] = {(char)0xc0, (char)0xaf, '\0'};
  const char c1_control[] = {'h', 'o', 's', 't', (char)0xc2, (char)0x9b, '\0'};

  memset(longest_username, 'a', sizeof(longest_username) - 1);
  longest_username[sizeof(longest_username) - 1] = '\0';
  memset(oversized_username, 'a', sizeof(oversized_username) - 1);
  oversized_username[sizeof(oversized_username) - 1] = '\0';

  CHECK(valid_username("miguel"));
  CHECK(valid_username(unicode_username));
  CHECK(valid_username(longest_username));
  CHECK(!valid_username(NULL));
  CHECK(!valid_username(""));
  CHECK(!valid_username("two words"));
  CHECK(!valid_username("line\nbreak"));
  CHECK(!valid_username("delete\x7f"));
  CHECK(!valid_username(malformed_utf8));
  CHECK(!valid_username(oversized_username));

  CHECK(valid_remote_host("192.0.2.10"));
  CHECK(valid_remote_host("2001:db8::1"));
  CHECK(!valid_remote_host(c1_control));
  return 0;
}

static int test_lenstr_rejects_ambiguous_input(void)
{
  const uint8_t embedded_nul[] = {0, 3, 'a', 0, 'b'};
  const uint8_t truncated[] = {0, 4, 'a', 'b'};
  char buf[8];
  int fd = -1;

  memset(buf, 'x', sizeof(buf));
  CHECK(write_pipe_bytes(embedded_nul, sizeof(embedded_nul), &fd) == 0);
  CHECK(read_lenstr(fd, buf, sizeof(buf)) == -1);
  CHECK(close(fd) == 0);
  for (size_t i = 0; i < sizeof(buf); i++)
    CHECK(buf[i] == '\0');

  memset(buf, 'x', sizeof(buf));
  CHECK(write_pipe_bytes(truncated, sizeof(truncated), &fd) == 0);
  CHECK(read_lenstr(fd, buf, sizeof(buf)) == -1);
  CHECK(close(fd) == 0);
  for (size_t i = 0; i < sizeof(buf); i++)
    CHECK(buf[i] == '\0');

  return 0;
}

static int test_pam_conversation(void)
{
  const struct pam_message messages[] = {
      {.msg_style = PAM_PROMPT_ECHO_OFF, .msg = "Password:"},
      {.msg_style = PAM_PROMPT_ECHO_ON, .msg = "Login:"},
      {.msg_style = PAM_TEXT_INFO, .msg = "Info"},
      {.msg_style = PAM_ERROR_MSG, .msg = "Error"},
  };
  const struct pam_message *message_ptrs[] = {
      &messages[0], &messages[1], &messages[2], &messages[3]};
  struct pam_appdata appdata = {
      .username = "miguel",
      .password = "secret",
  };
  struct pam_response *responses = NULL;

  CHECK(pam_conv_func(4, message_ptrs, &responses, &appdata) == PAM_SUCCESS);
  CHECK(responses != NULL);
  CHECK(responses[0].resp != NULL && strcmp(responses[0].resp, "secret") == 0);
  CHECK(responses[1].resp != NULL && strcmp(responses[1].resp, "miguel") == 0);
  CHECK(responses[2].resp == NULL);
  CHECK(responses[3].resp == NULL);
  for (int i = 0; i < 4; i++)
    CHECK(responses[i].resp_retcode == 0);
  free_pam_responses(responses, 4);

  responses = NULL;
  CHECK(pam_conv_func(0, message_ptrs, &responses, &appdata) == PAM_CONV_ERR);
  CHECK(responses == NULL);
  CHECK(pam_conv_func(33, message_ptrs, &responses, &appdata) == PAM_CONV_ERR);
  CHECK(responses == NULL);
  return 0;
}

static int test_bridge_policy(void)
{
  const uid_t owner = (uid_t)1234;
  struct stat bridge = {
      .st_mode = S_IFREG | 0755,
      .st_uid = owner,
  };
  struct stat parent = {
      .st_mode = S_IFDIR | 0755,
      .st_uid = owner,
  };

  CHECK(validate_bridge_policy(&bridge, owner) == 0);
  bridge.st_mode = S_IFREG | 0775;
  CHECK(validate_bridge_policy(&bridge, owner) == -1);
  bridge.st_mode = S_IFREG | 0644;
  CHECK(validate_bridge_policy(&bridge, owner) == -1);
  bridge.st_mode = S_IFREG | S_ISUID | 0755;
  CHECK(validate_bridge_policy(&bridge, owner) == -1);
  bridge.st_mode = S_IFDIR | 0755;
  CHECK(validate_bridge_policy(&bridge, owner) == -1);
  bridge.st_mode = S_IFREG | 0755;
  CHECK(validate_bridge_policy(&bridge, owner + 1) == -1);

  CHECK(validate_parent_dir_policy(&parent, owner) == 0);
  parent.st_mode = S_IFDIR | 0777;
  CHECK(validate_parent_dir_policy(&parent, owner) == -1);
  parent.st_mode = S_IFREG | 0755;
  CHECK(validate_parent_dir_policy(&parent, owner) == -1);
  parent.st_mode = S_IFDIR | 0755;
  CHECK(validate_parent_dir_policy(&parent, owner + 1) == -1);
  return 0;
}

static int test_sudo_policy_argv(void)
{
  const char *argv[10] = {0};
  const char *canonical = "alice";

  CHECK(build_sudo_policy_argv(canonical, argv, 10) == 0);
  CHECK(strcmp(argv[0], "/usr/bin/sudo") == 0);
  CHECK(strcmp(argv[1], "-n") == 0);
  CHECK(strcmp(argv[2], "-l") == 0);
  CHECK(strcmp(argv[3], "-U") == 0);
  CHECK(argv[4] == canonical);
  CHECK(strcmp(argv[5], "-u") == 0);
  CHECK(strcmp(argv[6], "root") == 0);
  CHECK(strcmp(argv[7], "--") == 0);
  CHECK(strcmp(argv[8], BRIDGE_PATH) == 0);
  CHECK(argv[9] == NULL);
  CHECK(build_sudo_policy_argv(NULL, argv, 10) == -1);
  CHECK(build_sudo_policy_argv("", argv, 10) == -1);
  CHECK(build_sudo_policy_argv(canonical, NULL, 10) == -1);
  CHECK(build_sudo_policy_argv(canonical, argv, 9) == -1);
  CHECK(user_can_run_bridge_as_root(NULL, 0) == 0);
  CHECK(user_can_run_bridge_as_root("", 0) == 0);
  return 0;
}

static int test_bootstrap_encoding(void)
{
  const uint8_t expected[] = {
      PROTO_MAGIC_0, PROTO_MAGIC_1, PROTO_MAGIC_2, PROTO_VERSION,
      0x01, 0x02, 0x03, 0x04,
      0xa0, 0xb0, 0xc0, 0xd0,
      PROTO_FLAG_VERBOSE | PROTO_FLAG_PRIVILEGED | PROTO_FLAG_READY_ACK,
      0, 3, 's', 'i', 'd',
      0, 4, 'u', 's', 'e', 'r',
  };
  uint8_t actual[sizeof(expected) + 1];
  size_t actual_len = 0;
  int pipefd[2];

  CHECK(pipe2(pipefd, O_CLOEXEC) == 0);
  CHECK(write_bootstrap_binary(pipefd[1], "sid", "user",
                               (uid_t)0x01020304U, (gid_t)0xa0b0c0d0U, 1, 1) == 0);
  CHECK(close(pipefd[1]) == 0);
  CHECK(read_pipe_to_end(pipefd[0], actual, sizeof(actual), &actual_len) == 0);
  CHECK(close(pipefd[0]) == 0);
  CHECK(actual_len == sizeof(expected));
  CHECK(memcmp(actual, expected, sizeof(expected)) == 0);
  return 0;
}

static int test_child_status_reporting(void)
{
  uint8_t reported = 0;
  uint8_t extra = 0;
  int pipefd[2];
  int status = 0;
  pid_t pid;

  CHECK(pipe2(pipefd, O_CLOEXEC) == 0);
  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
  {
    int devnull;

    close(pipefd[0]);
    devnull = open("/dev/null", O_WRONLY | O_CLOEXEC);
    if (devnull >= 0)
    {
      (void)dup2(devnull, STDERR_FILENO);
      if (devnull != STDERR_FILENO)
        close(devnull);
    }
    errno = ENOENT;
    child_die(pipefd[1], "test failure");
  }

  CHECK(close(pipefd[1]) == 0);
  CHECK(read(pipefd[0], &reported, sizeof(reported)) == (ssize_t)sizeof(reported));
  CHECK(read(pipefd[0], &extra, sizeof(extra)) == 0);
  CHECK(close(pipefd[0]) == 0);
  CHECK(waitpid_nointr(pid, &status, 0) == pid);
  CHECK(WIFEXITED(status));
  CHECK(WEXITSTATUS(status) == 127);
  CHECK(reported == 1);
  return 0;
}

static int test_elapsed_microseconds(void)
{
  CHECK(elapsed_us(INT64_C(1000000000), INT64_C(1000000999)) == 0);
  CHECK(elapsed_us(INT64_C(1000000000), INT64_C(1000001000)) == 1);
  CHECK(elapsed_us(INT64_C(1000000000), INT64_C(1001234567)) == 1234);
  CHECK(elapsed_us(0, INT64_C(1000000000)) == -1);
  CHECK(elapsed_us(INT64_C(2000000000), INT64_C(1000000000)) == -1);
  return 0;
}

static int test_socket_response_writes(void)
{
  const uint8_t expected[] = {
      PROTO_MAGIC_0, PROTO_MAGIC_1, PROTO_MAGIC_2, PROTO_VERSION,
      PROTO_STATUS_OK, PROTO_MODE_PRIVILEGED, PROTO_RESULT_OK, 0,
      0x01, 0x02, 0x03, 0x04,
      0xa0, 0xb0, 0xc0, 0xd0,
      0, 4, 'u', 's', 'e', 'r',
  };
  uint8_t actual[sizeof(expected) + 1];
  size_t actual_len = 0;
  char fill[4096] = {0};
  int sockets[2];
  int duplicate;
  int flags;
  int64_t now_ns;
  ssize_t fill_result;

  CHECK(socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0, sockets) == 0);
  duplicate = dup(sockets[0]);
  CHECK(duplicate >= 0);
  flags = fcntl(duplicate, F_GETFL);
  CHECK(flags >= 0);
  CHECK(fcntl(duplicate, F_SETFL, flags | O_NONBLOCK) == 0);
  CHECK(close(duplicate) == 0);

  int response_rc = send_ok_response(sockets[0], PROTO_MODE_PRIVILEGED, "user",
                                     (uid_t)0x01020304U, (gid_t)0xa0b0c0d0U);
  if (response_rc != 0)
    fprintf(stderr, "send_ok_response failed: %s\n", strerror(errno));
  CHECK(response_rc == 0);
  CHECK(close(sockets[0]) == 0);
  CHECK(read_pipe_to_end(sockets[1], actual, sizeof(actual), &actual_len) == 0);
  CHECK(close(sockets[1]) == 0);
  CHECK(actual_len == sizeof(expected));
  CHECK(memcmp(actual, expected, sizeof(expected)) == 0);

  CHECK(socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC | SOCK_NONBLOCK, 0, sockets) == 0);
  for (;;)
  {
    fill_result = write(sockets[0], fill, sizeof(fill));
    if (fill_result > 0)
      continue;
    if (fill_result < 0 && errno == EINTR)
      continue;
    break;
  }
  CHECK(fill_result == -1);
  CHECK(errno == EAGAIN);
  CHECK(monotonic_now_ns(&now_ns) == 0);
  errno = 0;
  CHECK(socket_write_all_until(sockets[0], "x", 1,
                               now_ns + INT64_C(20000000)) == -1);
  CHECK(errno == ETIMEDOUT);
  CHECK(close(sockets[0]) == 0);
  CHECK(close(sockets[1]) == 0);
  return 0;
}

static int test_child_wait_and_timeout(void)
{
  struct timespec started;
  struct timespec finished;
  int64_t elapsed_ns;
  int status = 0;
  pid_t pid;

  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
    _exit(23);
  CHECK(wait_for_child_with_timeout(pid, 2, 0) == 23);

  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
  {
    (void)raise(SIGTERM);
    _exit(127);
  }
  CHECK(wait_for_child_with_timeout(pid, 2, 0) == 128 + SIGTERM);

  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
  {
    for (;;)
      pause();
  }

  int64_t outer_deadline_ns;
  CHECK(monotonic_now_ns(&outer_deadline_ns) == 0);
  outer_deadline_ns += INT64_C(50000000);
  CHECK(clock_gettime(CLOCK_MONOTONIC, &started) == 0);
  CHECK(wait_for_child_with_timeout(pid, 1, outer_deadline_ns) == -1);
  CHECK(clock_gettime(CLOCK_MONOTONIC, &finished) == 0);
  elapsed_ns = (int64_t)(finished.tv_sec - started.tv_sec) * INT64_C(1000000000) +
               (int64_t)(finished.tv_nsec - started.tv_nsec);
  CHECK(elapsed_ns >= INT64_C(40000000));
  CHECK(elapsed_ns < INT64_C(10000000000));

  errno = 0;
  CHECK(waitpid(pid, &status, WNOHANG) == -1);
  CHECK(errno == ECHILD);
  return 0;
}

static int test_bridge_startup_wait(void)
{
  char msg[PROTO_MAX_ERROR];
  uint8_t bad = 0;
  int rfd;

  // READY byte -> success
  const uint8_t ready_byte = PROTO_STARTUP_READY;
  CHECK(write_pipe_bytes(&ready_byte, 1, &rfd) == 0);
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_READY);
  CHECK(close(rfd) == 0);

  // Pre-exec failure byte
  const uint8_t exec_failed = PROTO_STARTUP_EXEC_FAILED;
  CHECK(write_pipe_bytes(&exec_failed, 1, &rfd) == 0);
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_EXEC_FAILED);
  CHECK(close(rfd) == 0);

  // ERROR byte with message; control bytes must be blanked
  const uint8_t err_frame[] = {PROTO_STARTUP_ERROR, 'b', 'o', 'o', 'm',
                               0x1b, '!', 0x07};
  CHECK(write_pipe_bytes(err_frame, sizeof(err_frame), &rfd) == 0);
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_REPORTED_ERROR);
  CHECK(strcmp(msg, "boom ! ") == 0);
  CHECK(close(rfd) == 0);

  // ERROR byte without message -> empty string, still an error outcome
  const uint8_t err_only = PROTO_STARTUP_ERROR;
  CHECK(write_pipe_bytes(&err_only, 1, &rfd) == 0);
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_REPORTED_ERROR);
  CHECK(msg[0] == '\0');
  CHECK(close(rfd) == 0);

  // ERROR message longer than the buffer is truncated, not an error
  {
    uint8_t big[1 + PROTO_MAX_ERROR + 64];
    big[0] = PROTO_STARTUP_ERROR;
    memset(big + 1, 'A', sizeof(big) - 1);
    CHECK(write_pipe_bytes(big, sizeof(big), &rfd) == 0);
    CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
          BRIDGE_STARTUP_REPORTED_ERROR);
    CHECK(strlen(msg) == PROTO_MAX_ERROR - 1);
    CHECK(msg[0] == 'A' && msg[PROTO_MAX_ERROR - 2] == 'A');
    CHECK(close(rfd) == 0);
  }

  // EOF before any byte -> died/closed without ack
  const uint8_t none[1] = {0};
  CHECK(write_pipe_bytes(none, 0, &rfd) == 0);
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_EOF);
  CHECK(close(rfd) == 0);

  // Unknown status byte -> protocol error, byte reported
  const uint8_t weird = 0x7f;
  CHECK(write_pipe_bytes(&weird, 1, &rfd) == 0);
  bad = 0;
  CHECK(wait_for_bridge_startup(rfd, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_PROTOCOL_ERROR);
  CHECK(bad == 0x7f);
  CHECK(close(rfd) == 0);

  // No byte within the deadline -> timeout, and the wait actually blocks
  {
    int pipefd[2];
    struct timespec started, finished;
    int64_t elapsed_ns;

    CHECK(pipe2(pipefd, O_CLOEXEC) == 0);
    int64_t outer_deadline_ns;
    CHECK(monotonic_now_ns(&outer_deadline_ns) == 0);
    outer_deadline_ns += INT64_C(50000000);
    CHECK(clock_gettime(CLOCK_MONOTONIC, &started) == 0);
    CHECK(wait_for_bridge_startup(pipefd[0], 1000, outer_deadline_ns,
                                  msg, sizeof(msg), &bad) ==
          BRIDGE_STARTUP_TIMEOUT);
    CHECK(clock_gettime(CLOCK_MONOTONIC, &finished) == 0);
    elapsed_ns = (int64_t)(finished.tv_sec - started.tv_sec) * INT64_C(1000000000) +
                 (int64_t)(finished.tv_nsec - started.tv_nsec);
    CHECK(elapsed_ns >= INT64_C(40000000));
    CHECK(elapsed_ns < INT64_C(10000000000));
    CHECK(close(pipefd[0]) == 0);
    CHECK(close(pipefd[1]) == 0);
  }

  // Invalid inputs fail closed
  CHECK(wait_for_bridge_startup(-1, 1000, 0, msg, sizeof(msg), &bad) ==
        BRIDGE_STARTUP_WAIT_ERROR);

  // READY keeps the bidirectional channel open until the launcher sends GO.
  // This barrier prevents the bridge from writing Yamux bytes before the
  // launcher has completed the authentication response.
  {
    int status_channel[2];
    uint8_t got = 0;

    CHECK(socketpair(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0,
                     status_channel) == 0);
    CHECK(write_all(status_channel[1], &ready_byte, sizeof(ready_byte)) == 0);
    CHECK(wait_for_bridge_startup(status_channel[0], 1000, 0,
                                  msg, sizeof(msg), &bad) ==
          BRIDGE_STARTUP_READY);
    CHECK(release_bridge_startup(status_channel[0]) == 0);
    CHECK(read(status_channel[1], &got, sizeof(got)) == (ssize_t)sizeof(got));
    CHECK(got == PROTO_STARTUP_GO);
    CHECK(close(status_channel[0]) == 0);
    CHECK(close(status_channel[1]) == 0);
  }

  return 0;
}

int main(void)
{
  const struct test_case tests[] = {
      {"identity validation", test_identity_validation},
      {"length-prefixed input", test_lenstr_rejects_ambiguous_input},
      {"PAM conversation", test_pam_conversation},
      {"bridge policy", test_bridge_policy},
      {"sudo policy argv", test_sudo_policy_argv},
      {"bootstrap encoding", test_bootstrap_encoding},
      {"child status reporting", test_child_status_reporting},
      {"elapsed microseconds", test_elapsed_microseconds},
      {"socket response writes", test_socket_response_writes},
      {"child wait and timeout", test_child_wait_and_timeout},
      {"bridge startup wait", test_bridge_startup_wait},
  };
  int failures = 0;

  for (size_t i = 0; i < sizeof(tests) / sizeof(tests[0]); i++)
  {
    int result = tests[i].run();
    printf("%s %s\n", result == 0 ? "ok" : "not ok", tests[i].name);
    failures += result != 0;
  }

  if (failures != 0)
  {
    fprintf(stderr, "%d auth test group(s) failed\n", failures);
    return 1;
  }

  printf("all auth C tests passed\n");
  return 0;
}
