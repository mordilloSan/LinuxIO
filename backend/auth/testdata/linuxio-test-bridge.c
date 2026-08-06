// linuxio-test-bridge - stub bridge for the Tier-1 launcher integration
// suite (make test-auth-pam). The launcher execveat()s this binary in place
// of the real Go bridge. Its environment is fully synthesized by the
// launcher, so the only control channels available are the fixed fd layout
// and the working directory (the launcher chdir()s to the user's home
// before exec).
//
// Behavior is selected by the first line of ./bridge_control in the cwd:
//   ok       full dump, READY on fd 4, wait for GO, marker on fd 3, exit 0
//   eof      exit 7 immediately without writing a status byte
//   garbage  write an unknown status byte (0x7f) on fd 4, exit 7
//   error    write PROTO_STARTUP_ERROR plus a message on fd 4, exit 3
//   sleep    write nothing and sleep until killed
//   linger   like ok, then write ./linger-started and block until terminated
//            (a live supervised session for service-stop scenarios)
//   stubborn like linger, but SIGTERM is ignored (forces SIGKILL escalation)
//
// In "ok" mode it dumps, into the cwd:
//   dump_fds        one "fd=N target=..." line per open descriptor
//   dump_ids        getresuid/getresgid values
//   dump_env        the complete post-exec environment, one line per entry
//   dump_cwd        the working directory
//   dump_bootstrap  the raw bootstrap bytes read from stdin
#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <signal.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#define STARTUP_READY 0x02
#define STARTUP_ERROR 0x03
#define STARTUP_GO 0x04
#define STATUS_FD 4
#define CLIENT_FD 3

extern char **environ;

struct fd_entry
{
  int fd;
  char target[128];
};

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

static void die(const char *what)
{
  char msg[256];
  int len = snprintf(msg, sizeof(msg), "%c%s: %s", STARTUP_ERROR, what,
                     strerror(errno));

  if (len > 0)
    (void)write_all(STATUS_FD, msg, (size_t)len);
  _exit(11);
}

static void write_text_file(const char *path, const char *data, size_t len)
{
  int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC | O_CLOEXEC, 0644);

  if (fd < 0 || write_all(fd, data, len) != 0)
    die(path);
  close(fd);
}

int main(void)
{
  char control[64] = "";
  struct fd_entry fds[64];
  size_t fd_count = 0;
  char bootstrap[4096];
  size_t bootstrap_len = 0;
  char buf[8192];
  int len;

  // Read the control word first; a missing file means the launcher did not
  // chdir where the test expected, which must fail the scenario loudly.
  {
    int fd = open("bridge_control", O_RDONLY | O_CLOEXEC);
    ssize_t n;

    if (fd < 0)
      die("open bridge_control");
    n = read(fd, control, sizeof(control) - 1);
    close(fd);
    if (n < 0)
      die("read bridge_control");
    control[n] = '\0';
    control[strcspn(control, "\n")] = '\0';
  }

  if (strcmp(control, "eof") == 0)
    _exit(7);
  if (strcmp(control, "garbage") == 0)
  {
    const unsigned char weird = 0x7f;
    (void)write_all(STATUS_FD, &weird, 1);
    _exit(7);
  }
  if (strcmp(control, "error") == 0)
  {
    const char frame[] = {STARTUP_ERROR, 's', 't', 'u', 'b', ' ', 'b', 'r',
                          'i', 'd', 'g', 'e', ' ', 'e', 'x', 'p', 'l', 'o',
                          'd', 'e', 'd'};
    (void)write_all(STATUS_FD, frame, sizeof(frame));
    _exit(3);
  }
  if (strcmp(control, "sleep") == 0)
  {
    for (;;)
      pause();
  }
  int linger = strcmp(control, "linger") == 0 || strcmp(control, "stubborn") == 0;
  if (strcmp(control, "stubborn") == 0 && signal(SIGTERM, SIG_IGN) == SIG_ERR)
    die("ignore SIGTERM");
  if (!linger && strcmp(control, "ok") != 0)
    die("unknown bridge_control word");

  // Enumerate open descriptors before creating any new ones, excluding the
  // directory fd used for the enumeration itself.
  {
    DIR *dir = opendir("/proc/self/fd");
    struct dirent *entry;
    int self_fd;

    if (!dir)
      die("opendir /proc/self/fd");
    self_fd = dirfd(dir);
    while ((entry = readdir(dir)) != NULL &&
           fd_count < sizeof(fds) / sizeof(fds[0]))
    {
      char link_path[64];
      ssize_t n;
      char *end = NULL;
      long fd = strtol(entry->d_name, &end, 10);

      if (!end || *end || fd < 0 || fd == self_fd)
        continue;
      fds[fd_count].fd = (int)fd;
      snprintf(link_path, sizeof(link_path), "/proc/self/fd/%ld", fd);
      n = readlink(link_path, fds[fd_count].target,
                   sizeof(fds[fd_count].target) - 1);
      fds[fd_count].target[n > 0 ? (size_t)n : 0] = '\0';
      fd_count++;
    }
    closedir(dir);
  }

  // Drain the bootstrap from stdin; the launcher closes the write end after
  // sending it, so EOF here also proves the pipe plumbing.
  for (;;)
  {
    ssize_t n = read(STDIN_FILENO, bootstrap + bootstrap_len,
                     sizeof(bootstrap) - bootstrap_len);
    if (n < 0)
    {
      if (errno == EINTR)
        continue;
      die("read bootstrap");
    }
    if (n == 0)
      break;
    bootstrap_len += (size_t)n;
    if (bootstrap_len == sizeof(bootstrap))
      die("bootstrap too large");
  }

  {
    size_t used = 0;

    for (size_t i = 0; i < fd_count; i++)
    {
      len = snprintf(buf + used, sizeof(buf) - used, "fd=%d target=%s\n",
                     fds[i].fd, fds[i].target);
      if (len < 0 || (size_t)len >= sizeof(buf) - used)
        die("format fd dump");
      used += (size_t)len;
    }
    write_text_file("dump_fds", buf, used);
  }

  {
    uid_t ruid, euid, suid;
    gid_t rgid, egid, sgid;

    if (getresuid(&ruid, &euid, &suid) != 0 ||
        getresgid(&rgid, &egid, &sgid) != 0)
      die("getresuid/getresgid");
    len = snprintf(buf, sizeof(buf),
                   "ruid=%u euid=%u suid=%u rgid=%u egid=%u sgid=%u\n",
                   (unsigned)ruid, (unsigned)euid, (unsigned)suid,
                   (unsigned)rgid, (unsigned)egid, (unsigned)sgid);
    if (len < 0 || (size_t)len >= sizeof(buf))
      die("format id dump");
    write_text_file("dump_ids", buf, (size_t)len);
  }

  {
    size_t used = 0;

    for (char **env = environ; *env; env++)
    {
      len = snprintf(buf + used, sizeof(buf) - used, "%s\n", *env);
      if (len < 0 || (size_t)len >= sizeof(buf) - used)
        die("format env dump");
      used += (size_t)len;
    }
    write_text_file("dump_env", buf, used);
  }

  {
    char cwd[PATH_MAX];

    if (!getcwd(cwd, sizeof(cwd)))
      die("getcwd");
    write_text_file("dump_cwd", cwd, strlen(cwd));
  }

  write_text_file("dump_bootstrap", bootstrap, bootstrap_len);

  {
    const unsigned char ready = STARTUP_READY;
    unsigned char go = 0;
    ssize_t n;

    if (write_all(STATUS_FD, &ready, 1) != 0)
      _exit(12);
    do
    {
      n = read(STATUS_FD, &go, 1);
    } while (n < 0 && errno == EINTR);
    if (n != 1 || go != STARTUP_GO)
      _exit(9);
  }

  // The transport now belongs to the bridge: the marker must appear in the
  // client's byte stream strictly after the launcher's complete OK response.
  if (write_all(CLIENT_FD, "YAMUX-OK", 8) != 0)
    _exit(10);

  if (linger)
  {
    // Tell the test the session is live before blocking, so it can deliver
    // the service-stop signal deterministically after the marker bytes.
    write_text_file("linger-started", "1", 1);
    for (;;)
      pause();
  }
  return 0;
}
