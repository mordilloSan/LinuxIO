/*
 * linuxio_protocol.h - Binary protocol constants for LinuxIO auth/bridge communication
 *
 * This header defines the binary protocol shared between:
 *   - linuxio-auth (C)
 *   - linuxio server/bridge (Go)
 *
 * Keep in sync with backend/common/protocol/
 */

#ifndef LINUXIO_PROTOCOL_H
#define LINUXIO_PROTOCOL_H

/* ==========================================================================
 * Protocol Magic and Version
 * ========================================================================== */

#define PROTO_MAGIC_0            'L'
#define PROTO_MAGIC_1            'I'
#define PROTO_MAGIC_2            'O'
#define PROTO_VERSION            1

/* ==========================================================================
 * Auth Request Protocol (Server -> Auth via Unix socket)
 *
 * Format:
 *   [magic:4][flags:1][reserved:3]  (8 bytes fixed header)
 *   [len:2][user]
 *   [len:2][password]
 *   [len:2][session_id]
 *
 * All multi-byte integers are big-endian.
 * ========================================================================== */

#define PROTO_AUTH_REQ_HEADER_SIZE   8

/* Request flags byte */
#define PROTO_REQ_FLAG_VERBOSE       0x01

/* ==========================================================================
 * Auth Response Protocol (Auth -> Server via Unix socket)
 *
 * Format:
 *   [magic:4][status:1][mode:1][reserved:2]  (8 bytes fixed header)
 *   [len:2][error]      (only if status == error)
 *   [len:2][motd]       (only if status == ok)
 *
 * All multi-byte integers are big-endian.
 * ========================================================================== */

#define PROTO_AUTH_RESP_HEADER_SIZE  8

/* Status byte values */
#define PROTO_STATUS_OK              0
#define PROTO_STATUS_ERROR           1

/* Mode byte values */
#define PROTO_MODE_UNPRIVILEGED      0
#define PROTO_MODE_PRIVILEGED        1

/* ==========================================================================
 * Bootstrap Protocol (Auth -> Bridge via stdin pipe)
 *
 * Format:
 *   [magic:4][uid:4][gid:4][flags:1]  (13 bytes fixed header)
 *   [len:2][session_id]
 *   [len:2][username]
 *
 * All multi-byte integers are big-endian.
 * ========================================================================== */

#define PROTO_HEADER_SIZE            13

/* Bootstrap flags byte (bit field) */
#define PROTO_FLAG_VERBOSE           0x01
#define PROTO_FLAG_PRIVILEGED        0x02

/* ==========================================================================
 * Max lengths for variable fields
 * ========================================================================== */

#define PROTO_MAX_USERNAME           256
#define PROTO_MAX_PASSWORD           2048
#define PROTO_MAX_SESSION_ID         64
#define PROTO_MAX_MOTD               4096
#define PROTO_MAX_ERROR              256

/* ==========================================================================
 * Legacy string constants (for mode_str in response - used internally)
 * ========================================================================== */

#define MODE_PRIVILEGED              "privileged"
#define MODE_UNPRIVILEGED            "unprivileged"

#endif /* LINUXIO_PROTOCOL_H */
