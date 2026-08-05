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

static int test_bootstrap_encoding(void)
{
  const uint8_t expected[] = {
      PROTO_MAGIC_0, PROTO_MAGIC_1, PROTO_MAGIC_2, PROTO_VERSION,
      0x01, 0x02, 0x03, 0x04,
      0xa0, 0xb0, 0xc0, 0xd0,
      PROTO_FLAG_VERBOSE | PROTO_FLAG_PRIVILEGED,
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
  CHECK(wait_for_child_with_timeout(pid, 2) == 23);

  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
  {
    (void)raise(SIGTERM);
    _exit(127);
  }
  CHECK(wait_for_child_with_timeout(pid, 2) == 128 + SIGTERM);

  pid = fork();
  CHECK(pid >= 0);
  if (pid == 0)
  {
    for (;;)
      pause();
  }

  CHECK(clock_gettime(CLOCK_MONOTONIC, &started) == 0);
  CHECK(wait_for_child_with_timeout(pid, 1) == -1);
  CHECK(clock_gettime(CLOCK_MONOTONIC, &finished) == 0);
  elapsed_ns = (int64_t)(finished.tv_sec - started.tv_sec) * INT64_C(1000000000) +
               (int64_t)(finished.tv_nsec - started.tv_nsec);
  CHECK(elapsed_ns >= INT64_C(750000000));
  CHECK(elapsed_ns < INT64_C(10000000000));

  errno = 0;
  CHECK(waitpid(pid, &status, WNOHANG) == -1);
  CHECK(errno == ECHILD);
  return 0;
}

int main(void)
{
  const struct test_case tests[] = {
      {"identity validation", test_identity_validation},
      {"length-prefixed input", test_lenstr_rejects_ambiguous_input},
      {"PAM conversation", test_pam_conversation},
      {"bridge policy", test_bridge_policy},
      {"bootstrap encoding", test_bootstrap_encoding},
      {"child status reporting", test_child_status_reporting},
      {"child wait and timeout", test_child_wait_and_timeout},
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
