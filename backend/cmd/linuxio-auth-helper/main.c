#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <security/pam_appl.h>
#include <pwd.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <syslog.h>
#include <unistd.h>

#ifndef PAM_SERVICE_NAME
#define PAM_SERVICE_NAME "linuxio"
#endif

struct pam_context {
    char *password;
};

static int pam_conversation(int num_msg, const struct pam_message **msg,
                            struct pam_response **resp, void *appdata_ptr) {
    struct pam_context *ctx = (struct pam_context *)appdata_ptr;
    struct pam_response *replies = NULL;

    if (num_msg <= 0 || num_msg > PAM_MAX_NUM_MSG) {
        return PAM_CONV_ERR;
    }

    replies = calloc((size_t)num_msg, sizeof(struct pam_response));
    if (!replies) {
        return PAM_BUF_ERR;
    }

    for (int i = 0; i < num_msg; ++i) {
        switch (msg[i]->msg_style) {
            case PAM_PROMPT_ECHO_OFF:
                replies[i].resp = strdup(ctx->password);
                if (!replies[i].resp) {
                    goto error;
                }
                break;
            case PAM_PROMPT_ECHO_ON:
                goto error;
            case PAM_ERROR_MSG:
            case PAM_TEXT_INFO:
                replies[i].resp = NULL;
                break;
            default:
                goto error;
        }
    }

    *resp = replies;
    return PAM_SUCCESS;

error:
    if (replies) {
        for (int i = 0; i < num_msg; ++i) {
            if (replies[i].resp) {
                free(replies[i].resp);
            }
        }
        free(replies);
    }
    *resp = NULL;
    return PAM_CONV_ERR;
}

static void scrub(char *buf, size_t len) {
    if (!buf) return;
    memset(buf, 0, len);
}

static int read_password(char **out_pw) {
    char *line = NULL;
    size_t cap = 0;
    ssize_t n = getline(&line, &cap, stdin);
    if (n < 0) {
        free(line);
        return -1;
    }
    if (n > 0 && (line[n - 1] == '\n' || line[n - 1] == '\r')) {
        line[n - 1] = '\0';
        --n;
        if (n > 0 && line[n - 1] == '\r') {
            line[n - 1] = '\0';
        }
    }
    *out_pw = line;
    return 0;
}

int main(int argc, char **argv) {
    if (geteuid() != 0) {
        fprintf(stderr, "linuxio-auth-helper must run as root\n");
        return 2;
    }

    if (argc != 2 || argv[1][0] == '\0') {
        fprintf(stderr, "usage: linuxio-auth-helper <username>\n");
        return 2;
    }

    const char *username = argv[1];
    char *password = NULL;
    if (read_password(&password) != 0) {
        fprintf(stderr, "failed to read password\n");
        return 1;
    }

    struct pam_context ctx = {.password = password};
    struct pam_conv conv = {pam_conversation, &ctx};
    pam_handle_t *pamh = NULL;

    int pam_status = pam_start(PAM_SERVICE_NAME, username, &conv, &pamh);
    if (pam_status != PAM_SUCCESS) {
        fprintf(stderr, "pam start failed: %s\n", pam_strerror(pamh, pam_status));
        scrub(password, strlen(password));
        free(password);
        if (pamh) pam_end(pamh, pam_status);
        return 1;
    }

    char hostname[256];
    if (gethostname(hostname, sizeof(hostname)) == 0) {
        pam_set_item(pamh, PAM_RHOST, hostname);
    }

    pam_status = pam_authenticate(pamh, 0);
    if (pam_status == PAM_SUCCESS) {
        pam_status = pam_acct_mgmt(pamh, 0);
    }

    scrub(password, strlen(password));
    free(password);

    if (pam_status != PAM_SUCCESS) {
        fprintf(stderr, "%s\n", pam_strerror(pamh, pam_status));
        pam_end(pamh, pam_status);
        return 1;
    }

    pam_end(pamh, PAM_SUCCESS);
    return 0;
}
