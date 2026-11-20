#!/bin/sh
# SUDO_ASKPASS helper for the VS Code reinstall command: prints the password the
# extension put in this task's environment.
printf '%s\n' "$LINUXIO_SUDO_PASSWORD"
