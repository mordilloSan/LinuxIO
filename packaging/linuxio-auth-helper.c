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
#include <fcntl.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <syslog.h>
#include <stdarg.h>

#ifdef __has_include
#  if __has_include(<systemd/sd-journal.h>)
#    include <systemd/sd-journal.h>
#    define HAVE_SD_JOURNAL 1
#  endif
#endif

// --- forward decls ---
static int write_all(int fd, const void *buf, size_t len);

// -------- secure zero ----------
#ifndef _WIN32
static void secure_bzero(void *p, size_t n) {
#if defined(__GLIBC__) || defined(__APPLE__)
  if (p && n) explicit_bzero(p, n);
#else
  if (!p) return;
  volatile unsigned char *vp = (volatile unsigned char*)p;
  while (n--) *vp++ = 0;
#endif
}
#endif

// -------- PAM conversation ----
static int pam_conv_func(int n, const struct pam_message **msg,
                         struct pam_response **resp, void *appdata_ptr) {
  const char *password = (const char*)appdata_ptr;
  struct pam_response *r = calloc((size_t)n, sizeof(*r));
  if (!r) return PAM_CONV_ERR;

  for (int i = 0; i < n; i++) {
    switch (msg[i]->msg_style) {
      case PAM_PROMPT_ECHO_OFF:
        if (password) {
          r[i].resp = strdup(password);
          if (!r[i].resp) {
            for (int j = 0; j < i; j++) free(r[j].resp);
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
static void drop_to_user(const struct passwd *pw) {
  if (setgroups(0, NULL) != 0) { perror("setgroups"); _exit(127); }
  if (initgroups(pw->pw_name, pw->pw_gid) != 0) { perror("initgroups"); _exit(127); }
  if (setgid(pw->pw_gid) != 0) { perror("setgid"); _exit(127); }
  if (setuid(pw->pw_uid) != 0) { perror("setuid"); _exit(127); }
}

// -------- env builder ----------
static void minimal_env(const struct passwd *pw, const char *envmode, const char *bridge_bin,
                        int set_xdg,
                        const char *sess_id, const char *sess_user, const char *sess_uid,
                        const char *sess_gid, const char *sess_secret,
                        const char *server_base, const char *server_cert) {
  clearenv();
  if (pw) {
    setenv("HOME", pw->pw_dir, 1);
    setenv("USER", pw->pw_name, 1);
    setenv("LOGNAME", pw->pw_name, 1);
  }
  setenv("PATH", "/usr/sbin:/usr/bin:/sbin:/bin", 1);
  if (envmode)    setenv("LINUXIO_ENV", envmode, 1);
  if (bridge_bin) setenv("LINUXIO_BRIDGE_BIN", bridge_bin, 1);

  // Session / server env needed by linuxio-bridge
  if (sess_id)     setenv("LINUXIO_SESSION_ID",    sess_id,    1);
  if (sess_user)   setenv("LINUXIO_SESSION_USER",  sess_user,  1);
  if (sess_uid)    setenv("LINUXIO_SESSION_UID",   sess_uid,   1);
  if (sess_gid)    setenv("LINUXIO_SESSION_GID",   sess_gid,   1);
  if (sess_secret) setenv("LINUXIO_BRIDGE_SECRET", sess_secret,1);
  if (server_base) setenv("LINUXIO_SERVER_BASE_URL", server_base, 1);
  if (server_cert) setenv("LINUXIO_SERVER_CERT",     server_cert, 1);

  if (set_xdg && pw) {
    char xdg[64];
    snprintf(xdg, sizeof(xdg), "/run/user/%u", (unsigned)pw->pw_uid);
    setenv("XDG_RUNTIME_DIR", xdg, 1);
  }
}

// -------- read line from stdin -
static char *readline_stdin(size_t max) {
  char *buf = malloc(max);
  if (!buf) return NULL;
  size_t i = 0;
  int c;
  while (i + 1 < max && (c = fgetc(stdin)) != EOF && c != '\n') buf[i++] = (char)c;
  buf[i] = '\0';
  return buf;
}

static int ensure_runtime_dirs(const struct passwd *pw, gid_t *out_linuxio_gid) {
  if (!pw) return -1;

  const char *base = "/run/linuxio";
  mode_t old = umask(0);

  // linuxio group (fallback to root:0 if missing)
  gid_t linuxio_gid = 0;
  struct group *gr = getgrnam("linuxio");
  if (gr) linuxio_gid = gr->gr_gid;
  if (out_linuxio_gid) *out_linuxio_gid = linuxio_gid;

  // /run/linuxio -> root:linuxio 02771
  if (mkdir(base, 02771) != 0 && errno != EEXIST) {
    perror("mkdir /run/linuxio"); umask(old); return -1;
  }
  if (chown(base, 0, linuxio_gid) != 0) { perror("chown /run/linuxio"); umask(old); return -1; }
  if (chmod(base, 02771) != 0)         { perror("chmod /run/linuxio"); umask(old); return -1; }

  // /run/linuxio/<uid> -> <user>:linuxio 02770
  char userdir[128];
  snprintf(userdir, sizeof(userdir), "%s/%u", base, (unsigned)pw->pw_uid);
  if (mkdir(userdir, 02770) != 0 && errno != EEXIST) {
    perror("mkdir /run/linuxio/<uid>"); umask(old); return -1;
  }
  if (chown(userdir, pw->pw_uid, linuxio_gid) != 0) { perror("chown /run/linuxio/<uid>"); umask(old); return -1; }
  if (chmod(userdir, 02770) != 0)                   { perror("chmod /run/linuxio/<uid>"); umask(old); return -1; }

  umask(old);
  return 0;
}

// -------- validate bridge owner
static int validate_bridge_path_owned(const char *path, uid_t required_owner) {
  struct stat st;
  if (!path) return -1;
  if (stat(path, &st) != 0) { perror("stat bridge"); return -1; }
  if (!S_ISREG(st.st_mode)) { fprintf(stderr, "bridge not regular file\n"); return -1; }
  if ((st.st_mode & (S_IWGRP|S_IWOTH)) != 0) {
    fprintf(stderr, "bridge is group/world-writable\n"); return -1;
  }
  if (st.st_uid != required_owner) {
    fprintf(stderr, "bridge owner mismatch: expect uid %u\n", (unsigned)required_owner); return -1;
  }
  if ((st.st_mode & 0111) == 0) {
    fprintf(stderr, "bridge is not executable\n"); return -1;
  }
  return 0;
}

// -------- journald helpers --------
static void journal_errorf(const char *fmt, ...) {
  va_list ap;
  va_start(ap, fmt);
  char buf[512];
  vsnprintf(buf, sizeof(buf), fmt, ap);
  va_end(ap);
#ifdef HAVE_SD_JOURNAL
  (void) sd_journal_send("MESSAGE=%s", buf, "PRIORITY=%i", LOG_ERR, "SYSLOG_IDENTIFIER=linuxio-auth-helper", NULL);
#else
  openlog("linuxio-auth-helper", LOG_PID, LOG_AUTHPRIV);
  syslog(LOG_ERR, "%s", buf);
  closelog();
#endif
}

// Redirect the future bridge child's stdout/stderr to journald; fallback to file in /run/linuxio/<uid>/bridge-<sid8>.log
static int redirect_bridge_output(uid_t owner_uid, gid_t linuxio_gid, const char *sess_id) {
  char sid8[9] = {0};
  if (sess_id && *sess_id) {
    strncpy(sid8, sess_id, 8);
    sid8[8] = '\0';
  } else {
    strcpy(sid8, "nosessid");
  }

#ifdef HAVE_SD_JOURNAL
  int jfd = sd_journal_stream_fd("linuxio-bridge", LOG_INFO, 1 /*level_prefix*/);
  if (jfd >= 0) {
    if (dup2(jfd, STDOUT_FILENO) < 0) { journal_errorf("dup2 stdout->journald failed: %m"); close(jfd); return -1; }
    if (dup2(jfd, STDERR_FILENO) < 0) { journal_errorf("dup2 stderr->journald failed: %m"); close(jfd); return -1; }
    close(jfd);
    return 0;
  }
  journal_errorf("sd_journal_stream_fd failed; falling back to file log");
#endif

  char dir[128], path[192];
  snprintf(dir, sizeof(dir), "/run/linuxio/%u", (unsigned)owner_uid);
  snprintf(path, sizeof(path), "%s/bridge-%s.log", dir, sid8);

  int fd = open(path, O_CREAT|O_WRONLY|O_APPEND, 0640);
  if (fd < 0) {
    journal_errorf("open %s failed: %m; falling back to /dev/null", path);
    int devnull = open("/dev/null", O_WRONLY);
    if (devnull >= 0) {
      dup2(devnull, STDOUT_FILENO);
      dup2(devnull, STDERR_FILENO);
      close(devnull);
      return 0;
    }
    return -1;
  }
  if (fchown(fd, owner_uid, linuxio_gid) != 0) {
    journal_errorf("fchown(%s) failed: %m", path);
    // non-fatal
  }

  if (dup2(fd, STDOUT_FILENO) < 0) { journal_errorf("dup2 stdout->file failed: %m"); close(fd); return -1; }
  if (dup2(fd, STDERR_FILENO) < 0) { journal_errorf("dup2 stderr->file failed: %m"); close(fd); return -1; }
  close(fd);
  return 0;
}

// ---- utilities to probe sudo capability (auto privilege decision) ----
static int run_cmd_as_user_with_input(const struct passwd *pw,
                                      char *const argv[],
                                      const char *stdin_data,
                                      int timeout_sec)
{
  int inpipe[2] = {-1,-1};
  if (pipe(inpipe) != 0) return -1;

  pid_t pid = fork();
  if (pid < 0) { close(inpipe[0]); close(inpipe[1]); return -1; }

  if (pid == 0) {
    // Child: drop to user, hook stdin from pipe
    if (setgroups(0, NULL) != 0) _exit(127);
    if (initgroups(pw->pw_name, pw->pw_gid) != 0) _exit(127);
    if (setgid(pw->pw_gid) != 0) _exit(127);
    if (setuid(pw->pw_uid) != 0) _exit(127);

    // stdin from read-end
    if (dup2(inpipe[0], STDIN_FILENO) < 0) _exit(127);
    close(inpipe[0]); close(inpipe[1]);

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
  if (stdin_data && *stdin_data) {
    (void)write_all(inpipe[1], stdin_data, strlen(stdin_data));
  }
  close(inpipe[1]);

  // Wait with timeout
  int status = 0;
  int elapsed_ms = 0;
  while (elapsed_ms < timeout_sec * 1000) {
    pid_t r = waitpid(pid, &status, WNOHANG);
    if (r == pid) break;
    if (r < 0 && errno != EINTR) break;
    usleep(100 * 1000);
    elapsed_ms += 100;
  }
  if (elapsed_ms >= timeout_sec * 1000) {
    kill(pid, SIGKILL);
    waitpid(pid, &status, 0);
    return -1;
  }

  if (WIFEXITED(status)) return WEXITSTATUS(status);
  if (WIFSIGNALED(status)) return 128 + WTERMSIG(status);
  return -1;
}

static int user_has_sudo(const struct passwd *pw, const char *password, int *out_nopasswd)
{
  if (out_nopasswd) *out_nopasswd = 0;

  // 1) NOPASSWD probe (no password)
  char *argv_nopw[] = { "/usr/bin/sudo", "-n", "-v", NULL };
  int rc = run_cmd_as_user_with_input(pw, argv_nopw, NULL, 3);
  if (rc == 0) { if (out_nopasswd) *out_nopasswd = 1; return 1; }

  // 2) Password-based probe: sudo -S -p '' -v
  if (password && *password) {
    char *argv_pw[] = { "/usr/bin/sudo", "-S", "-p", "", "-v", NULL };
    char buf[1024];
    (void)snprintf(buf, sizeof(buf), "%s\n", password);
    rc = run_cmd_as_user_with_input(pw, argv_pw, buf, 4);
    if (rc == 0) return 1;
  }

  return 0;
}

// -------- main ----------
int main(void) {
  if (geteuid() != 0) {
    fprintf(stderr, "must be setuid root\n");
    return 126;
  }

  const char *user = getenv("LINUXIO_TARGET_USER");
  if (!user || !*user) { fprintf(stderr, "missing LINUXIO_TARGET_USER\n"); return 2; }

  // password from stdin (or env for testing)
  char *password = NULL;
  const char *env_pw = getenv("LINUXIO_PASSWORD");
  if (env_pw && *env_pw) {
    password = strdup(env_pw);
    unsetenv("LINUXIO_PASSWORD"); // reduce exposure
  } else {
    password = readline_stdin(1024);
  }
  if (!password || !*password) { fprintf(stderr, "missing password on stdin/env\n"); free(password); return 2; }

  // PAM auth + account + creds
  struct pam_conv conv = { pam_conv_func, (void*)password };
  pam_handle_t *pamh = NULL;
  int rc = pam_start("linuxio", user, &conv, &pamh);
  if (rc != PAM_SUCCESS) { fprintf(stderr, "pam_start: %s\n", pam_strerror(pamh, rc)); secure_bzero(password, strlen(password)); free(password); return 5; }

  (void)pam_set_item(pamh, PAM_RHOST, "web");
  rc = pam_authenticate(pamh, 0);
  if (rc == PAM_SUCCESS) rc = pam_acct_mgmt(pamh, 0);
  if (rc == PAM_SUCCESS) rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);
  if (rc != PAM_SUCCESS) {
    fprintf(stderr, "%s\n", pam_strerror(pamh, rc));
    pam_end(pamh, rc);
    if (password) { secure_bzero(password, strlen(password)); free(password); }
    return 1;
  }

  rc = pam_open_session(pamh, 0);
  if (rc != PAM_SUCCESS) {
    fprintf(stderr, "open_session: %s\n", pam_strerror(pamh, rc));
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password) { secure_bzero(password, strlen(password)); free(password); }
    return 5;
  }

  // target user info
  struct passwd *pw = getpwnam(user);
  if (!pw) {
    perror("getpwnam");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password) { secure_bzero(password, strlen(password)); free(password); }
    return 5;
  }

  gid_t linuxio_gid = 0;
  if (ensure_runtime_dirs(pw, &linuxio_gid) != 0) {
    fprintf(stderr, "prepare runtime dir failed\n");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    if (password) { secure_bzero(password, strlen(password)); free(password); }
    return 5;
  }

  // Decide privileged mode
  int nopasswd = 0;
  int want_privileged = user_has_sudo(pw, password, &nopasswd) ? 1 : 0;

  // Read remaining inputs/env before clearenv in child
  const char *envmode_in = getenv("LINUXIO_ENV");
  const char *bridge_in  = getenv("LINUXIO_BRIDGE_BIN");
  const char *envmode    = (envmode_in && *envmode_in) ? envmode_in : "production";
  const char *bridge_path= (bridge_in  && *bridge_in)  ? bridge_in  : "/usr/local/bin/linuxio-bridge";

  const char *sess_id     = getenv("LINUXIO_SESSION_ID");
  const char *sess_user   = getenv("LINUXIO_SESSION_USER");
  const char *sess_uid    = getenv("LINUXIO_SESSION_UID");
  const char *sess_gid    = getenv("LINUXIO_SESSION_GID");
  const char *sess_secret = getenv("LINUXIO_BRIDGE_SECRET");
  const char *server_base = getenv("LINUXIO_SERVER_BASE_URL");
  const char *server_cert = getenv("LINUXIO_SERVER_CERT");

  // Now wipe the password; we no longer need it
  if (password) { secure_bzero(password, strlen(password)); free(password); password = NULL; }

  // bridge binary validation according to decision
  if (want_privileged) {
    if (validate_bridge_path_owned(bridge_path, 0 /*root*/) != 0) {
      pam_close_session(pamh, 0); pam_setcred(pamh, PAM_DELETE_CRED); pam_end(pamh, 0);
      return 5;
    }
  } else {
    struct stat st;
    if (stat(bridge_path, &st) != 0 || !S_ISREG(st.st_mode) ||
        (st.st_mode & (S_IWGRP|S_IWOTH)) ||
        !(st.st_uid == 0 || st.st_uid == pw->pw_uid)) {
      fprintf(stderr, "bridge must be owned by root or %s and not group/world-writable\n", pw->pw_name);
      pam_close_session(pamh, 0); pam_setcred(pamh, PAM_DELETE_CRED); pam_end(pamh, 0);
      return 5;
    }
  }

  if (want_privileged) {
    (void)write_all(STDOUT_FILENO, "MODE=privileged\n", 16);
  } else {
    (void)write_all(STDOUT_FILENO, "MODE=unprivileged\n", 18);
  }

  // ---------- Correct double-fork: NANNY first, then BRIDGE ----------
  pid_t nanny = fork();
  if (nanny < 0) {
    perror("fork nanny");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 5;
  }

  if (nanny == 0) {
    // Nanny process: create the bridge as *its* child, then wait it and close PAM
    pid_t child = fork();
    if (child < 0) {
      perror("fork bridge");
      pam_close_session(pamh, 0);
      pam_setcred(pamh, PAM_DELETE_CRED);
      pam_end(pamh, 0);
      _exit(5);
    }

    if (child == 0) {
      // Bridge child: set identity/env and exec
      umask(077);
      if (want_privileged) {
        minimal_env(NULL, envmode, bridge_path, /*set_xdg=*/0,
                    sess_id, sess_user, sess_uid, sess_gid, sess_secret,
                    server_base, server_cert);
        setenv("HOME", "/root", 1);
        setenv("USER", "root", 1);
        setenv("LOGNAME", "root", 1);
        // keep root uid/gid
      } else {
        drop_to_user(pw);
        minimal_env(pw, envmode, bridge_path, /*set_xdg=*/1,
                    sess_id, sess_user, sess_uid, sess_gid, sess_secret,
                    server_base, server_cert);
        if (chdir(pw->pw_dir) != 0) { perror("chdir"); _exit(127); }
      }

      // Redirect bridge stdout/stderr to journald (or file fallback)
      uid_t owner_uid = pw->pw_uid;
      if (redirect_bridge_output(owner_uid, linuxio_gid, sess_id) != 0) {
        int devnull = open("/dev/null", O_WRONLY);
        if (devnull >= 0) {
          dup2(devnull, STDOUT_FILENO);
          dup2(devnull, STDERR_FILENO);
          close(devnull);
        }
      }

      char *const argv_child[] = { (char*)bridge_path, "--env", (char*)envmode, NULL };
      execv(bridge_path, argv_child);
      perror("exec linuxio-bridge");
      _exit(127);
    }

    // Nanny waits bridge, then closes PAM and exits with same status
    int status = 0;
    while (waitpid(child, &status, 0) < 0 && errno == EINTR) {}
    (void)pam_close_session(pamh, 0);
    (void)pam_setcred(pamh, PAM_DELETE_CRED);
    (void)pam_end(pamh, 0);
    _exit(WIFEXITED(status) ? WEXITSTATUS(status)
                             : WIFSIGNALED(status) ? 128 + WTERMSIG(status) : 1);
  }

  // Original parent: report success immediately & exit
  (void)write_all(STDOUT_FILENO, "OK\n", 3);
  (void)close(STDIN_FILENO);
  (void)close(STDOUT_FILENO);
  (void)close(STDERR_FILENO);
  _exit(0);
}

// -------- write_all ----------
static int write_all(int fd, const void *buf, size_t len) {
  const unsigned char *p = (const unsigned char *)buf;
  while (len > 0) {
    ssize_t n = write(fd, p, len);
    if (n < 0) {
      if (errno == EINTR) continue;
      return -1; // real error
    }
    p += (size_t)n;
    len -= (size_t)n;
  }
  return 0;
}
