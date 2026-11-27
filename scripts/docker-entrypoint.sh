#!/bin/sh
set -eu

if [ "$(id -u)" -eq 0 ]; then
  chown -R appuser:appuser /data
  exec su-exec appuser:appuser "$@"
fi

exec "$@"
