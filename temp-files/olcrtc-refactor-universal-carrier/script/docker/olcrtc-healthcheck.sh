#!/bin/sh
set -eu

exe="$(readlink /proc/1/exe 2>/dev/null || true)"
case "$exe" in
    */olcrtc) exit 0 ;;
    *) exit 1 ;;
esac
