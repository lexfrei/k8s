#!/bin/sh
set -e

rm -f /run/dbus/dbus.pid
mkdir -p /run/dbus /run/avahi-daemon

dbus-daemon --system --nofork &
sleep 1

exec avahi-daemon --no-drop-root --no-rlimits
