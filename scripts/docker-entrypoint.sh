#!/bin/sh
set -eu

data_writable() {
  su-exec appuser:appuser sh -c '
    probe=$(mktemp /data/.mihomo-deck-write-check.XXXXXX) && rm -- "$probe"
  ' 2>/dev/null
}

if [ "$(id -u)" -eq 0 ]; then
  if ! data_writable; then
    if ! chown -R appuser:appuser /data || ! data_writable; then
      echo 'Error: /data is not writable or traversable by appuser (UID 1000). Check the volume permissions and mount mode.' >&2
      exit 1
    fi
  fi
  exec su-exec appuser:appuser "$@"
fi

exec "$@"
