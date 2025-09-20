// /usr/local/bin/linuxio-auth-helper  (install 4755 root:root)
#define _GNU_SOURCE
#include <security/pam_appl.h>
#include <pwd.h>
#include <grp.h>
#include <errno.h>
#include <signal.h>
#include <sys/wait.h>
#include <sys/stat.h>
#include <unistd.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

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

static int ensure_runtime_dirs(const struct passwd *pw) {
  if (!pw) return -1;

  const char *base = "/run/linuxio";
  mode_t old = umask(0);

  // linuxio group (fallback to root:0 if missing)
  gid_t linuxio_gid = 0;
  struct group *gr = getgrnam("linuxio");
  if (gr) linuxio_gid = gr->gr_gid;

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
  if (rc != PAM_SUCCESS) { fprintf(stderr, "pam_start: %s\n", pam_strerror(pamh, rc)); free(password); return 5; }

  (void)pam_set_item(pamh, PAM_RHOST, "web");
  rc = pam_authenticate(pamh, 0);
  if (rc == PAM_SUCCESS) rc = pam_acct_mgmt(pamh, 0);
  if (rc == PAM_SUCCESS) rc = pam_setcred(pamh, PAM_ESTABLISH_CRED);

  // wipe pw
  secure_bzero(password, strlen(password));
  free(password);
  password = NULL;

  if (rc != PAM_SUCCESS) {
    fprintf(stderr, "%s\n", pam_strerror(pamh, rc));
    pam_end(pamh, rc);
    return 1;
  }

  rc = pam_open_session(pamh, 0);
  if (rc != PAM_SUCCESS) {
    fprintf(stderr, "open_session: %s\n", pam_strerror(pamh, rc));
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, rc);
    return 5;
  }

  // target user info
  struct passwd *pw = getpwnam(user);
  if (!pw) {
    perror("getpwnam");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 5;
  }

  if (ensure_runtime_dirs(pw) != 0) {
    fprintf(stderr, "prepare runtime dir failed\n");
    pam_close_session(pamh, 0);
    pam_setcred(pamh, PAM_DELETE_CRED);
    pam_end(pamh, 0);
    return 5;
  }
  // inputs/env before clearenv
  const char *priv_env   = getenv("LINUXIO_PRIV");
  int want_privileged    = (priv_env && priv_env[0] == '1');

  const char *envmode_in = getenv("LINUXIO_ENV");
  const char *bridge_in  = getenv("LINUXIO_BRIDGE_BIN");
  const char *envmode    = (envmode_in && *envmode_in) ? envmode_in : "production";
  const char *bridge_path= (bridge_in  && *bridge_in)  ? bridge_in  : "/usr/local/bin/linuxio-bridge";

  // pass-through session/server vars
  const char *sess_id     = getenv("LINUXIO_SESSION_ID");
  const char *sess_user   = getenv("LINUXIO_SESSION_USER");
  const char *sess_uid    = getenv("LINUXIO_SESSION_UID");
  const char *sess_gid    = getenv("LINUXIO_SESSION_GID");
  const char *sess_secret = getenv("LINUXIO_BRIDGE_SECRET");
  const char *server_base = getenv("LINUXIO_SERVER_BASE_URL");
  const char *server_cert = getenv("LINUXIO_SERVER_CERT");

  // bridge binary validation
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
    if ((st.st_mode & 0111) == 0) {
      fprintf(stderr, "bridge is not executable\n");
      pam_close_session(pamh, 0); pam_setcred(pamh, PAM_DELETE_CRED); pam_end(pamh, 0);
      return 5;
    }
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

      const char *v = getenv("LINUXIO_VERBOSE");

      char *argv_child[5];
      int ai = 0;
      argv_child[ai++] = (char*)bridge_path;
      argv_child[ai++] = "--env";
      argv_child[ai++] = (char*)envmode;
      if (v && (v[0]=='1' || strcasecmp(v, "true")==0 || strcasecmp(v, "yes")==0)) {
        argv_child[ai++] = "--verbose";
      }
      argv_child[ai] = NULL;

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
