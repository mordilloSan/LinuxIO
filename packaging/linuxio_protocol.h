/*
 * linuxio_protocol.h - Protocol constants for LinuxIO auth communication
 *
 * This header defines JSON field names and limits shared between:
 *   - linuxio-auth (C)
 *   - linuxio server/bridge (Go)
 *
 * Keep in sync with backend/common/protocol/types.go
 */

#ifndef LINUXIO_PROTOCOL_H
#define LINUXIO_PROTOCOL_H

/* ==========================================================================
 * Max lengths for fields
 * ========================================================================== */

#define PROTO_MAX_USERNAME       256
#define PROTO_MAX_PASSWORD       8192
#define PROTO_MAX_SESSION_ID     64
#define PROTO_MAX_SECRET         128
#define PROTO_MAX_SOCKET_PATH    256
#define PROTO_MAX_BRIDGE_PATH    4096
#define PROTO_MAX_ENV_MODE       32
#define PROTO_MAX_SERVER_URL     512
#define PROTO_MAX_SERVER_CERT    16384
#define PROTO_MAX_MOTD           4096
#define PROTO_MAX_ERROR          256

/* ==========================================================================
 * Auth Request fields (Server -> Auth Daemon)
 * ========================================================================== */

#define FIELD_USER            "user"
#define FIELD_PASSWORD        "password"
#define FIELD_SESSION_ID      "session_id"
#define FIELD_SOCKET_PATH     "socket_path"
#define FIELD_BRIDGE_PATH     "bridge_path"
#define FIELD_ENV             "env"
#define FIELD_VERBOSE         "verbose"
#define FIELD_SECRET          "secret"
#define FIELD_SERVER_BASE_URL "server_base_url"
#define FIELD_SERVER_CERT     "server_cert"

/* ==========================================================================
 * Auth Response fields (Auth Daemon -> Server)
 * ========================================================================== */

#define FIELD_STATUS          "status"
#define FIELD_ERROR           "error"
#define FIELD_MODE            "mode"
#define FIELD_MOTD            "motd"

/* Status values */
#define STATUS_OK             "ok"
#define STATUS_ERROR          "error"

/* Mode values */
#define MODE_PRIVILEGED       "privileged"
#define MODE_UNPRIVILEGED     "unprivileged"

/* ==========================================================================
 * Bootstrap fields (Auth Daemon -> Bridge via stdin)
 * ========================================================================== */

#define FIELD_USERNAME        "username"
#define FIELD_UID             "uid"
#define FIELD_GID             "gid"
#define FIELD_LOG_FD          "log_fd"

/* ==========================================================================
 * Environment variables
 * ========================================================================== */

#define ENV_SESSION_ID        "LINUXIO_SESSION_ID"
#define ENV_SOCKET_PATH       "LINUXIO_SOCKET_PATH"
#define ENV_ENV               "LINUXIO_ENV"
#define ENV_VERBOSE           "LINUXIO_VERBOSE"
#define ENV_BRIDGE            "LINUXIO_BRIDGE"
#define ENV_PRIVILEGED        "LINUXIO_PRIVILEGED"

/* ==========================================================================
 * Environment mode values
 * ========================================================================== */

#define ENV_MODE_PRODUCTION   "production"
#define ENV_MODE_DEVELOPMENT  "development"

#endif /* LINUXIO_PROTOCOL_H */
