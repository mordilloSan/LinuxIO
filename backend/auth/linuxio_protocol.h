/*
 * linuxio_protocol.h - Binary protocol constants for LinuxIO auth/bridge communication
 *
 * This header defines the binary protocol shared between:
 *   - linuxio-auth (C)
 *   - linuxio webserver/bridge (Go)
 *
 * Keep in sync with backend/common/ipc/
 */

#ifndef LINUXIO_PROTOCOL_H
#define LINUXIO_PROTOCOL_H

/* ==========================================================================
 * Protocol Magic and Version
 * ========================================================================== */

#define PROTO_MAGIC_0            'L'
#define PROTO_MAGIC_1            'I'
#define PROTO_MAGIC_2            'O'
#define PROTO_VERSION            3

/* ==========================================================================
 * Auth Request Protocol (Server -> Auth via Unix socket)
 *
 * Format:
 *   [magic:4][flags:1][reserved:3]  (8 bytes fixed header)
 *   [len:2][user]
 *   [len:2][password]
 *   [len:2][session_id]
 *   [len:2][remote_host]
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
 *   [magic:4][status:1][mode:1][result:1][reserved:1]  (8 bytes fixed header)
 *   [uid:4][gid:4][len:2][username]    (only if status == ok)
 *   [len:2][error]                     (only if status == error)
 *
 * All multi-byte integers are big-endian.
 * ========================================================================== */

#define PROTO_AUTH_RESP_HEADER_SIZE  8

/* Status byte values */
#define PROTO_STATUS_OK              0
#define PROTO_STATUS_ERROR           1

/* Structured result codes */
#define PROTO_RESULT_OK              0
#define PROTO_RESULT_AUTH_FAILED     1
#define PROTO_RESULT_PASSWORD_EXPIRED 2
#define PROTO_RESULT_ACCESS_DENIED   3
#define PROTO_RESULT_BAD_REQUEST     4
#define PROTO_RESULT_INTERNAL_ERROR  5
#define PROTO_RESULT_BRIDGE_ERROR    6

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
#define PROTO_FLAG_READY_ACK         0x04

/* ==========================================================================
 * Startup-status Protocol (Bridge <-> Auth via inherited status fd)
 *
 * The launcher leaves one end of a socketpair open across exec at a fixed fd
 * and sets PROTO_FLAG_READY_ACK in the bootstrap. The bridge first writes
 * exactly one status byte on that fd:
 *
 *   PROTO_STARTUP_EXEC_FAILED  written by the pre-exec C child on any setup
 *                              or exec failure (followed by _exit)
 *   PROTO_STARTUP_READY        initialization is complete and the bridge is
 *                              waiting for PROTO_STARTUP_GO before starting
 *                              Yamux on the client fd
 *   PROTO_STARTUP_ERROR        fatal error before serving; optionally
 *                              followed by a short UTF-8 message (at most
 *                              PROTO_MAX_ERROR-1 bytes), then close/exit
 *
 * After READY, the launcher writes the complete authentication response to
 * the client connection and then sends PROTO_STARTUP_GO back to the bridge.
 * The bridge closes the status fd and starts Yamux only after receiving GO,
 * so Yamux cannot corrupt the preceding authentication response.
 *
 * EOF before any byte means the process died (or closed the fd) before
 * becoming ready. EOF or an unknown byte while waiting for GO also fails
 * closed.
 * ========================================================================== */

#define PROTO_STARTUP_EXEC_FAILED    0x01
#define PROTO_STARTUP_READY          0x02
#define PROTO_STARTUP_ERROR          0x03
#define PROTO_STARTUP_GO             0x04

/* ==========================================================================
 * Max lengths for variable fields
 *
 * These are C buffer sizes including the NUL terminator: the maximum
 * accepted string length is N-1 bytes. The Go sender enforces the same
 * limits (see backend/common/ipc/auth).
 * ========================================================================== */

#define PROTO_MAX_USERNAME           256
#define PROTO_MAX_PASSWORD           2048
#define PROTO_MAX_SESSION_ID         64
#define PROTO_MAX_REMOTE_HOST        256
#define PROTO_MAX_ERROR              256

/* ==========================================================================
 * Shared journald field names
 * ========================================================================== */

#define LINUXIO_JOURNAL_FIELD_SESSION_ID "LINUXIO_SESSION_ID"

#endif /* LINUXIO_PROTOCOL_H */
